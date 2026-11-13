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
	condv1alpha1 "github.com/sdcio/config-server/apis/condition/v1alpha1"
	"github.com/sdcio/config-server/apis/config"
	configv1alpha1 "github.com/sdcio/config-server/apis/config/v1alpha1"
	configv1alpha1apply "github.com/sdcio/config-server/pkg/generated/applyconfiguration/config/v1alpha1"
	"github.com/sdcio/config-server/pkg/reconcilers/resource"
	logf "github.com/sdcio/logger"
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
	log := logf.FromContext(ctx)
	log.V(logf.VDebug).Info("RecoverConfigs")
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
	log.V(logf.VDebug).Info("recovered configs", "count", len(configs))

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
	log := logf.FromContext(ctx).WithValues("target", key.String())

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

	log.V(logf.VDebug).Info("device intent recovery")

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
	log := logf.FromContext(ctx)
	log.V(logf.VDebug).Info("Transact")

	// the classification and the intents use the internal type; a copy, as
	// the conversion shares memory with cfgs
	configList := &config.ConfigList{}
	if err := configv1alpha1.Convert_v1alpha1_ConfigList_To_config_ConfigList(cfgs.DeepCopy(), configList, nil); err != nil {
		return nil, true, err
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

	targetKey := storebackend.KeyFromNSN(target.GetNamespacedName())

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
		log.Info("transaction warning", "warning", w)
	}
	if result.GlobalError != nil || result.IntentErrors != nil {
		log.Info("transaction failed",
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
	log.V(logf.VDebug).Info("transaction response", "rsp", prototext.Format(rsp))
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

	for i := range cfgs.Items {
		cfg := &cfgs.Items[i]
		if _, ok := configsToDelete[config.GetGVKNSN(cfg)]; !ok {
			continue
		}
		// while the finalizer still holds the Config, so a failure is retried
		if err := r.deleteDeviation(ctx, cfg); err != nil {
			return results, true, err
		}
		if err := r.finalizer.RemoveFinalizer(ctx, cfg); err != nil {
			return results, true, err
		}
	}

	return results, false, nil
}

func (r *Transactor) setIntents(
	ctx context.Context,
	dsctx *DatastoreHandle,
	targetKey storebackend.Key,
	transactionID string,
	configsToUpdate, configsToDelete map[string]*config.Config,
	dryRun bool,
) (*sdcpb.TransactionSetResponse, error) {
	log := logf.FromContext(ctx).WithValues("target", targetKey.String(), "transactionID", transactionID)

	configsToUpdateSet := sets.New[string]()
	configsToDeleteSet := sets.New[string]()

	intents := make([]*sdcpb.TransactionIntent, 0)

	for key, cfg := range configsToUpdate {
		configsToUpdateSet.Insert(key)
		log.Info("Transaction intent update", "config", key, "revertive", cfg.IsRevertive())
		update, err := config.GetIntentUpdate(cfg, true)
		if err != nil {
			log.Error(err, "Transaction getIntentUpdate config")
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
		log.V(logf.VDebug).Info("Transaction rsp", "rsp", prototext.Format(rsp))
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
	log := logf.FromContext(ctx)

	configsToUpdate := make(map[string]*config.Config)
	configsToDelete := make(map[string]*config.Config)
	nonRecoverable := make(map[string]*config.Config)
	configsNoChnge := make(map[string]*config.Config)

	// Classify configs: update / delete / non-recoverable / noop
	for i := range configList.Items {
		cfg := configList.Items[i].DeepCopy()
		key := config.GetGVKNSN(cfg)

		switch {
		case !cfg.IsRecoverable(ctx):
			nonRecoverable[key] = cfg

		case cfg.GetDeletionTimestamp() != nil:
			configsToDelete[key] = cfg

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

	log.V(logf.VDebug).Info("getConfigsAndDeviationsToTransact classification start",
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

	log.V(logf.VDebug).Info("getConfigsAndDeviationsToTransact classification after change",
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
	log := logf.FromContext(ctx)
	log.Info("handling transaction errors", "recoverable", recoverable)
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
		log.Info("intent failed", "name", intentName, "errors", intent.Errors)

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
		log.Error(globalErr, "transact dataserver error")
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
	logf.FromContext(ctx).Info("config transaction failed",
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
