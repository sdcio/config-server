/*
Copyright 2024 Nokia.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package targetmanager

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/google/uuid"
	"github.com/henderiw/apiserver-store/pkg/storebackend"
	"github.com/henderiw/logger/log"
	condv1alpha1 "github.com/sdcio/config-server/apis/condition/v1alpha1"
	"github.com/sdcio/config-server/apis/config"
	configv1alpha1 "github.com/sdcio/config-server/apis/config/v1alpha1"
	configv1alpha1apply "github.com/sdcio/config-server/pkg/generated/applyconfiguration/config/v1alpha1"
	"github.com/sdcio/config-server/pkg/reconcilers/resource"
	sdcpb "github.com/sdcio/sdc-protos/sdcpb"
	"google.golang.org/protobuf/encoding/prototext"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/sets"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

const (
	reconcilerName = "ConfigController"
	finalizer      = "config.config.sdcio.dev/finalizer"
	// finalizerFieldManager owns the Config finalizer (SSA, like the
	// TargetConfigController-finalizer on the Target)
	finalizerFieldManager = "transactor-finalizer"
)

type Transactor struct {
	client client.Client // k8s client
	// finalizer holds the Config while the dataserver has its intent
	finalizer *resource.APIFinalizer
	// status writes every Config status field, see configstatus.go
	status *configStatus
}

func NewTransactor(c client.Client) *Transactor {
	return &Transactor{
		client: c,
		finalizer: resource.NewAPIFinalizer(c, finalizer, finalizerFieldManager,
			func(name, namespace string, finalizers ...string) runtime.ApplyConfiguration {
				ac := configv1alpha1apply.Config(name, namespace)
				if len(finalizers) > 0 {
					ac.WithFinalizers(finalizers...)
				}
				return ac
			},
		),
		status: newConfigStatus(c),
	}
}

// RecoverConfigs replays the applied config of the Configs of the target
// into the datastore and writes the recovery outcome on them. It runs on the
// target runtime, when the datastore needs recovery, so it lists the Configs
// itself: once, for both the replay and the status.
func (r *Transactor) RecoverConfigs(ctx context.Context, target *configv1alpha1.Target, dsctx *DatastoreHandle) (*string, error) {
	log := log.FromContext(ctx)
	log.Debug("RecoverConfigs")
	cfgs, err := ListTargetConfigs(ctx, r.client, target)
	if err != nil {
		return nil, err
	}

	// the replay uses the internal type; a copy, as the conversion shares
	// memory with cfgs
	configList := &config.ConfigList{}
	if err := configv1alpha1.Convert_v1alpha1_ConfigList_To_config_ConfigList(cfgs.DeepCopy(), configList, nil); err != nil {
		return nil, err
	}
	configs := []*config.Config{}
	for i := range configList.Items {
		if configList.Items[i].Status.AppliedConfig != nil {
			configs = append(configs, &configList.Items[i])
		}
	}

	log.Info("recovering target config", "count", len(configs))
	targetKey := storebackend.KeyFromNSN(target.GetNamespacedName())
	msg, err := r.recoverIntents(ctx, dsctx, targetKey, configs)
	if err != nil {
		// This is bad since this means we cannot recover the applied config
		// on a target. We set the target config status to Failed.
		// Most likely a human intervention is needed

		if serr := r.setRecoveredConfigs(ctx, cfgs, func(cfg *configv1alpha1.Config) condv1alpha1.Condition {
			return configv1alpha1.ConfigFailed(cfg.GetGeneration(), msg)
		}); serr != nil {
			return ptr.To("recovery failed, and failed to update config status"), serr
		}

		return &msg, err
	}
	dsctx.MarkRecovered(true)
	log.Debug("recovered configs", "count", len(configs))

	if err := r.setRecoveredConfigs(ctx, cfgs, func(cfg *configv1alpha1.Config) condv1alpha1.Condition {
		return configv1alpha1.ConfigReady(cfg.GetGeneration(), "target recovered")
	}); err != nil {
		// recovery succeeded but we failed to patch status -> surface it
		return ptr.To("recovered, but failed to update config status"), err
	}
	return nil, nil
}

func (r *Transactor) recoverIntents(
	ctx context.Context,
	dsctx *DatastoreHandle,
	key storebackend.Key,
	configs []*config.Config,
) (string, error) {
	log := log.FromContext(ctx).With("target", key.String())

	if len(configs) == 0 {
		return "", nil
	}

	intents := make([]*sdcpb.TransactionIntent, 0, len(configs))
	for _, cfg := range configs {
		update, err := config.GetIntentUpdate(cfg, false)
		if err != nil {
			return "", err
		}
		intents = append(intents, &sdcpb.TransactionIntent{
			Intent:            config.GetGVKNSN(cfg),
			Priority:          int32(cfg.Spec.Priority),
			Update:            update,
			NonRevertive:      !cfg.IsRevertive(),
			PreviouslyApplied: true,
		})
	}

	log.Debug("device intent recovery")

	return r.TransactionSet(ctx, dsctx, &sdcpb.TransactionSetRequest{
		TransactionId: "recovery",
		DatastoreName: key.String(),
		DryRun:        false,
		Timeout:       ptr.To(int32(120)),
		Intents:       intents,
	})
}

func (r *Transactor) TransactionSet(
	ctx context.Context,
	dsctx *DatastoreHandle,
	req *sdcpb.TransactionSetRequest,
) (string, error) {
	rsp, err := dsctx.Client.TransactionSet(ctx, req)
	msg, err := processTransactionResponse(ctx, rsp, err)
	if err != nil {
		return msg, err
	}
	// Assumption: if no error this succeeded, if error this is providing the error code and the info can be
	// retrieved from the individual intents

	// For dryRun we don't have to confirm the transaction as the dataserver does not lock things.
	if req.DryRun {
		return msg, nil
	}

	if _, err := dsctx.Client.TransactionConfirm(ctx, &sdcpb.TransactionConfirmRequest{
		DatastoreName: req.DatastoreName,
		TransactionId: req.TransactionId,
	}); err != nil {
		return msg, err
	}
	return msg, nil
}

// Transact applies the changed configs of the target and writes their
// status: the outcome (ConfigReady, applied config) of the configs it
// transacted, then Ready of every config. The caller only transacts once no
// target stage blocks, so Ready follows the outcome: the one just written,
// or the last one as listed. cfgs are the Configs of the target as the
// caller listed them.
func (r *Transactor) Transact(
	ctx context.Context,
	target *configv1alpha1.Target,
	dsctx *DatastoreHandle,
	cfgs *configv1alpha1.ConfigList,
) (bool, error) {
	results, retry, err := r.transact(ctx, target, dsctx, cfgs)
	outcomes, serr := r.writeStatus(ctx, cfgs, results)
	serr = errors.Join(serr, r.UpdateConfigsReady(ctx, cfgs, readyFromOutcomes(outcomes)))
	if serr != nil {
		return true, errors.Join(err, serr)
	}
	return retry, err
}

// transact returns the outcome per config.
func (r *Transactor) transact(
	ctx context.Context,
	target *configv1alpha1.Target,
	dsctx *DatastoreHandle,
	cfgs *configv1alpha1.ConfigList,
) (map[string]txResult, bool, error) {
	log := log.FromContext(ctx)
	log.Debug("Transact")

	// the classification and the intents use the internal type; a copy, as
	// the conversion shares memory with cfgs
	configList := &config.ConfigList{}
	if err := configv1alpha1.Convert_v1alpha1_ConfigList_To_config_ConfigList(cfgs.DeepCopy(), configList, nil); err != nil {
		return nil, true, err
	}

	targetKey := storebackend.KeyFromNSN(target.GetNamespacedName())

	// Deleting orphan configs leave the device untouched, so they are taken
	// out of the regular transaction: their own state (e.g. unrecoverable)
	// or a failure of other intents must not block their deletion
	// (sdcio/config-server#529).
	orphans, configList := splitDeletingOrphans(configList)
	if len(orphans) > 0 {
		if retry, err := r.deleteOrphans(ctx, dsctx, targetKey, cfgs, orphans); err != nil {
			return nil, retry, err
		}
	}

	// reapply deviations for each config snippet
	// This ensure the deviations are applied
	for _, cfg := range configList.Items {
		if _, err := r.applyDeviation(ctx, &cfg); err != nil {
			return nil, true, err
		}
	}

	// determine change
	configsToUpdate, configsToDelete := getConfigsToTransact(ctx, configList)

	if len(configsToUpdate) == 0 && len(configsToDelete) == 0 {
		log.Info("Transact skip, nothing to update")
		return nil, false, nil
	}

	// before the dataserver has the intent: without the finalizer, a delete
	// would remove the Config without removing its intent
	for i := range cfgs.Items {
		if _, ok := configsToUpdate[config.GetGVKNSN(&cfgs.Items[i])]; !ok {
			continue
		}
		if err := r.finalizer.AddFinalizer(ctx, &cfgs.Items[i]); err != nil {
			return nil, true, err
		}
	}

	uuid := uuid.New()

	rsp, err := r.setIntents(
		ctx,
		dsctx,
		targetKey,
		uuid.String(),
		configsToUpdate,
		configsToDelete,
		false)
	// we first collect the warnings and errors -> to determine error or not
	result := analyzeIntentResponse(err, rsp)

	for _, w := range result.GlobalWarnings {
		log.Warn("transaction warning", "warning", w)
	}
	if result.GlobalError != nil || result.IntentErrors != nil {
		log.Warn("transaction failed",
			"recoverable", result.Recoverable,
			"globalError", result.GlobalError,
			"intentErrors", result.IntentErrors,
		)

		return handleTransactionErrors(
			ctx,
			rsp,
			configsToUpdate,
			configsToDelete,
			result.GlobalError,
			result.Recoverable,
		)
	}
	log.Debug("transaction response", "rsp", prototext.Format(rsp))
	// ok case
	if _, err := dsctx.Client.TransactionConfirm(ctx, &sdcpb.TransactionConfirmRequest{
		DatastoreName: dsctx.DatastoreName,
		TransactionId: uuid.String(),
	}); err != nil {
		return nil, true, err
	}
	results := make(map[string]txResult, len(configsToUpdate))
	for key, cfg := range configsToUpdate {
		applied := &configv1alpha1.ConfigSpec{}
		if err := configv1alpha1.Convert_config_ConfigSpec_To_v1alpha1_ConfigSpec(&cfg.Spec, applied, nil); err != nil {
			return results, false, err
		}
		results[key] = txResult{
			configReady: configv1alpha1.ConfigReady(cfg.GetGeneration(), ""),
			applied:     applied,
			schema:      dsctx.Schema,
		}
	}

	if err := r.releaseConfigs(ctx, cfgs, sets.KeySet(configsToDelete)); err != nil {
		return results, true, err
	}

	return results, false, nil
}

// ReleaseDeletingConfigs lets every deleting Config of the target go,
// without a transaction and whatever its deletion policy. It is used when no
// transaction will run anymore (the target is not found or being deleted),
// so a deletion does not wait on a target that is gone
// (sdcio/config-server#529). What the released Configs left on a device
// that comes back is reported as a target deviation.
func (r *Transactor) ReleaseDeletingConfigs(ctx context.Context, cfgs *configv1alpha1.ConfigList) error {
	deleting := sets.New[string]()
	for i := range cfgs.Items {
		if cfgs.Items[i].GetDeletionTimestamp() != nil {
			deleting.Insert(config.GetGVKNSN(&cfgs.Items[i]))
		}
	}
	if deleting.Len() == 0 {
		return nil
	}
	log.FromContext(ctx).Info("releasing deleting configs without transaction",
		"configs", sets.List(deleting))
	return r.releaseConfigs(ctx, cfgs, deleting)
}

// releaseConfigs lets the listed configs with a key in keys go: it deletes
// their deviation, while the finalizer still holds the Config so a failure
// is retried, then removes the finalizer so the apiserver can remove them.
func (r *Transactor) releaseConfigs(ctx context.Context, cfgs *configv1alpha1.ConfigList, keys sets.Set[string]) error {
	for i := range cfgs.Items {
		cfg := &cfgs.Items[i]
		if !keys.Has(config.GetGVKNSN(cfg)) {
			continue
		}
		if err := r.deleteDeviation(ctx, cfg); err != nil {
			return err
		}
		if err := r.finalizer.RemoveFinalizer(ctx, cfg); resource.IgnoreNotFound(err) != nil {
			return err
		}
	}
	return nil
}

// deleteOrphans removes the intents of deleting orphan configs from the
// datastore in a transaction of their own, then releases the configs.
// The release happens no matter the outcome of the transaction, except on a
// transient (gRPC) error, which is retried: the datastore may still hold the
// intent and should drop it. If the runtime goes away in between, they wait
// for the next transaction, or are released through ReleaseDeletingConfigs
// once the target is gone.
// retry is true when the caller should requeue.
func (r *Transactor) deleteOrphans(
	ctx context.Context,
	dsctx *DatastoreHandle,
	targetKey storebackend.Key,
	cfgs *configv1alpha1.ConfigList,
	orphans map[string]*config.Config,
) (bool, error) {
	log := log.FromContext(ctx)
	transactionID := uuid.New().String()

	rsp, err := r.setIntents(ctx, dsctx, targetKey, transactionID, nil, orphans, false)
	result := analyzeIntentResponse(err, rsp)
	switch {
	case result.GlobalError != nil && result.Recoverable:
		log.Warn("orphan delete transaction failed, retrying", "err", result.GlobalError)
		return true, result.GlobalError
	case result.GlobalError != nil || result.IntentErrors != nil:
		log.Warn("orphan delete transaction failed, releasing configs anyway",
			"configs", mapKeys(orphans),
			"globalError", result.GlobalError,
			"intentErrors", result.IntentErrors,
		)
	default:
		if _, err := dsctx.Client.TransactionConfirm(ctx, &sdcpb.TransactionConfirmRequest{
			DatastoreName: dsctx.DatastoreName,
			TransactionId: transactionID,
		}); err != nil {
			if isRecoverableTransactionError(err) {
				log.Warn("orphan delete confirm failed, retrying", "err", err)
				return true, err
			}
			log.Warn("orphan delete confirm failed, releasing configs anyway",
				"configs", mapKeys(orphans), "err", err)
		}
	}

	if err := r.releaseConfigs(ctx, cfgs, sets.KeySet(orphans)); err != nil {
		return true, err
	}
	return false, nil
}

// splitDeletingOrphans returns the deleting configs with deletionPolicy
// orphan, keyed like getConfigsToTransact, and a list with the other configs.
func splitDeletingOrphans(configList *config.ConfigList) (map[string]*config.Config, *config.ConfigList) {
	orphans := make(map[string]*config.Config)
	rest := &config.ConfigList{
		TypeMeta: configList.TypeMeta,
		ListMeta: configList.ListMeta,
		Items:    make([]config.Config, 0, len(configList.Items)),
	}
	for i := range configList.Items {
		cfg := &configList.Items[i]
		if cfg.GetDeletionTimestamp() != nil && cfg.Orphan() {
			orphans[config.GetGVKNSN(cfg)] = cfg.DeepCopy()
			continue
		}
		rest.Items = append(rest.Items, *cfg)
	}
	return orphans, rest
}

func (r *Transactor) setIntents(
	ctx context.Context,
	dsctx *DatastoreHandle,
	targetKey storebackend.Key,
	transactionID string,
	configsToUpdate, configsToDelete map[string]*config.Config,
	dryRun bool,
) (*sdcpb.TransactionSetResponse, error) {
	log := log.FromContext(ctx).With("target", targetKey.String(), "transactionID", transactionID)

	configsToUpdateSet := sets.New[string]()
	configsToDeleteSet := sets.New[string]()

	intents := make([]*sdcpb.TransactionIntent, 0)

	for key, cfg := range configsToUpdate {
		configsToUpdateSet.Insert(key)
		log.Info("Transaction intent update", "config", key, "revertive", cfg.IsRevertive())
		update, err := config.GetIntentUpdate(cfg, true)
		if err != nil {
			log.Error("Transaction getIntentUpdate config", "error", err)
			return nil, err
		}
		intents = append(intents, &sdcpb.TransactionIntent{
			Intent:       config.GetGVKNSN(cfg),
			Priority:     int32(cfg.Spec.Priority),
			Update:       update,
			NonRevertive: !cfg.IsRevertive(),
		})
	}
	for key, cfg := range configsToDelete {
		configsToDeleteSet.Insert(key)
		log.Info("Transaction intent delete", "config", key, "revertive", cfg.IsRevertive())
		intents = append(intents, &sdcpb.TransactionIntent{
			Intent:              config.GetGVKNSN(cfg),
			Delete:              true,
			DeleteIgnoreNoExist: true,
			Orphan:              cfg.Orphan(),
			// Dont set priority, dont set non revertive
		})
	}

	log.Info("Transaction",
		"configsToUpdate total", len(configsToUpdate),
		"configsToUpdate names", configsToUpdateSet.UnsortedList(),
		"configsToDelete total", len(configsToDelete),
		"configsToDelete names", configsToDeleteSet.UnsortedList(),
	)

	rsp, err := dsctx.Client.TransactionSet(ctx, &sdcpb.TransactionSetRequest{
		TransactionId: transactionID,
		DatastoreName: targetKey.String(),
		DryRun:        dryRun,
		Timeout:       ptr.To(int32(60)),
		Intents:       intents,
	})
	if rsp != nil {
		log.Debug("Transaction rsp", "rsp", prototext.Format(rsp))
	}
	return rsp, err
}

func (r *Transactor) deleteDeviation(ctx context.Context, cfg *configv1alpha1.Config) error {
	deviation := &configv1alpha1.Deviation{}
	deviation.Name = configv1alpha1.DeviationName(configv1alpha1.DeviationType_CONFIG, cfg.Name)
	deviation.Namespace = cfg.Namespace
	if err := r.client.Delete(ctx, deviation); err != nil {
		if resource.IgnoreNotFound(err) != nil {
			return err
		}
	}
	return nil
}

func (r *Transactor) applyDeviation(ctx context.Context, config *config.Config) (configv1alpha1.Deviation, error) {
	key := types.NamespacedName{
		Name:      configv1alpha1.DeviationName(configv1alpha1.DeviationType_CONFIG, config.Name),
		Namespace: config.Namespace,
	}

	deviation := &configv1alpha1.Deviation{}
	if err := r.client.Get(ctx, key, deviation); err != nil {
		if resource.IgnoreNotFound(err) != nil {
			return configv1alpha1.Deviation{}, err
		}
		// Not found: create new deviation
		newDeviation := configv1alpha1.BuildDeviation(metav1.ObjectMeta{
			Name:            configv1alpha1.DeviationName(configv1alpha1.DeviationType_CONFIG, config.Name),
			Namespace:       config.Namespace,
			OwnerReferences: []metav1.OwnerReference{config.GetOwnerReference()},
			Labels:          config.Labels,
		}, &configv1alpha1.DeviationSpec{
			DeviationType: ptr.To(configv1alpha1.DeviationType_CONFIG),
		}, nil)

		if err := r.client.Create(ctx, newDeviation); err != nil {
			return configv1alpha1.Deviation{}, err
		}
		return *newDeviation, nil
	}
	return *deviation, nil
}

func getConfigsToTransact(
	ctx context.Context,
	configList *config.ConfigList,
) (
	map[string]*config.Config,
	map[string]*config.Config,
) {
	log := log.FromContext(ctx)

	configsToUpdate := make(map[string]*config.Config)
	configsToDelete := make(map[string]*config.Config)
	nonRecoverable := make(map[string]*config.Config)
	configsNoChnge := make(map[string]*config.Config)

	// Classify configs: update / delete / non-recoverable / noop
	for i := range configList.Items {
		cfg := configList.Items[i].DeepCopy()
		key := config.GetGVKNSN(cfg)

		switch {
		// The delete intent does not depend on the spec, so the outcome of
		// the last apply (e.g. unrecoverable) must not hold a deletion back.
		case cfg.GetDeletionTimestamp() != nil:
			configsToDelete[key] = cfg

		case !cfg.IsRecoverable(ctx):
			nonRecoverable[key] = cfg

		case !cfg.IsConfigConditionReady():
			configsToUpdate[key] = cfg

		case cfg.Status.AppliedConfig != nil &&
			cfg.Spec.GetShaSum(ctx) == cfg.Status.AppliedConfig.GetShaSum(ctx):
			configsNoChnge[key] = cfg
			continue

		default:
			configsToUpdate[key] = cfg
		}
	}

	log.Debug("getConfigsAndDeviationsToTransact classification start",
		"configsToUpdate", mapKeys(configsToUpdate),
		"configsToDelete", mapKeys(configsToDelete),
		"nonRecoverable", mapKeys(nonRecoverable),
		"configsNoChange", mapKeys(configsNoChnge),
	)

	// --- 5) If we have changes, retry non-recoverables + add configs that did not change

	if len(configsToUpdate) > 0 || len(configsToDelete) > 0 {
		for key, cfg := range nonRecoverable {
			if cfg.GetDeletionTimestamp() != nil {
				configsToDelete[key] = cfg
			} else {
				configsToUpdate[key] = cfg
			}
		}
		for key, cfg := range configsNoChnge {
			if cfg.GetDeletionTimestamp() != nil {
				configsToDelete[key] = cfg
			} else {
				configsToUpdate[key] = cfg
			}
		}
	}

	log.Debug("getConfigsAndDeviationsToTransact classification after change",
		"configsToUpdate", mapKeys(configsToUpdate),
		"configsToDelete", mapKeys(configsToDelete),
		"nonRecoverable", mapKeys(nonRecoverable),
	)

	return configsToUpdate, configsToDelete
}

func mapKeys[T any](m map[string]T) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	return out
}

func handleTransactionErrors(
	ctx context.Context,
	rsp *sdcpb.TransactionSetResponse,
	configsToTransact, deletedConfigsToTransact map[string]*config.Config,
	globalErr error,
	recoverable bool,
) (map[string]txResult, bool, error) {
	log := log.FromContext(ctx)
	log.Warn("handling transaction errors", "recoverable", recoverable)
	results := make(map[string]txResult, len(configsToTransact)+len(deletedConfigsToTransact))

	// If no response at all → apply same error to all configs.
	if rsp == nil {
		for key, cfg := range configsToTransact {
			results[key] = failedResult(ctx, cfg, "", globalErr, recoverable)
		}
		for key, cfg := range deletedConfigsToTransact {
			results[key] = failedResult(ctx, cfg, "", globalErr, false)
		}
		return results, recoverable, globalErr
	}

	// Response present: handle per-intent
	dataServerError := false

	for intentName, intent := range rsp.Intents {
		log.Warn("intent failed", "name", intentName, "errors", intent.Errors)

		var errs = errors.Join(globalErr)
		for _, intentError := range intent.Errors {
			errs = errors.Join(errs, fmt.Errorf("%s", intentError))
		}
		warnings := collectWarnings(intent.Errors)

		msg := ""
		if len(warnings) > 0 {
			msg = strings.Join(warnings, "; ")
		}

		if cfg, ok := configsToTransact[intentName]; ok {
			results[intentName] = failedResult(ctx, cfg, msg, errs, false)
			continue
		}
		if cfg, ok := deletedConfigsToTransact[intentName]; ok {
			results[intentName] = failedResult(ctx, cfg, msg, errs, false)
			continue
		}

		// Dataserver reported an intent we don't know → treat as global error
		dataServerError = true
		recoverable = false
		globalErr = errors.Join(
			errs,
			fmt.Errorf("dataserver reported an error in an intent %s that does not exist", intentName),
		)
		break
	}

	if dataServerError {
		log.Error("transact dataserver error", "err", globalErr)
		for key, cfg := range configsToTransact {
			results[key] = failedResult(ctx, cfg, "", globalErr, recoverable)
		}
		for key, cfg := range deletedConfigsToTransact {
			results[key] = failedResult(ctx, cfg, "", globalErr, false)
		}
	}

	return results, recoverable, globalErr
}

// failedResult stamps the generation that was transacted, not the current
// one: a spec change in between must not be marked unrecoverable.
func failedResult(ctx context.Context, cfg *config.Config, msg string, err error, recoverable bool) txResult {
	log.FromContext(ctx).Warn("config transaction failed",
		"config", cfg.GetName(),
		"recoverable", recoverable,
		"msg", msg,
		"err", err,
	)
	if err != nil {
		msg = fmt.Sprintf("%s err %s", msg, err.Error())
	}
	if recoverable {
		return txResult{configReady: configv1alpha1.ConfigFailed(cfg.GetGeneration(), msg)}
	}
	return txResult{configReady: configv1alpha1.ConfigFailedUnrecoverable(cfg.GetGeneration(), msg)}
}

func collectWarnings(errorsOrMsgs []string) []string {
	warnings := make([]string, 0, len(errorsOrMsgs))
	for _, err := range errorsOrMsgs {
		warnings = append(warnings, fmt.Sprintf("warning: %q", err))
	}
	return warnings
}

type TransactionResult struct {
	GlobalError    error
	IntentErrors   error
	GlobalWarnings []string
	Recoverable    bool
}

func analyzeIntentResponse(err error, rsp *sdcpb.TransactionSetResponse) TransactionResult {
	result := TransactionResult{}

	if err != nil {
		result.GlobalError = fmt.Errorf("transaction error: %w", err)
		result.Recoverable = isRecoverableTransactionError(err)
	}

	if rsp != nil {
		// Collect global warnings
		result.GlobalWarnings = append(result.GlobalWarnings, rsp.Warnings...)
		// Collect intent errors
		for _, intent := range rsp.Intents {
			for _, intentError := range intent.Errors {
				result.IntentErrors = errors.Join(result.IntentErrors, fmt.Errorf("%s", intentError))
				result.Recoverable = false // any intent error is non-recoverable
			}
		}
	}

	return result
}