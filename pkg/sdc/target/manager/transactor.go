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
	"reflect"
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
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/sets"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
)

const (
	reconcilerName = "ConfigController"
	finalizer      = "config.config.sdcio.dev/finalizer"
)

type Transactor struct {
	client       client.Client // k8s client
	fieldManager string
}

func NewTransactor(client client.Client, fieldManager string) *Transactor {
	return &Transactor{
		client:       client,
		fieldManager: fieldManager,
	}
}

func (r *Transactor) RecoverConfigs(ctx context.Context, target *configv1alpha1.Target, dsctx *DatastoreHandle) (*string, error) {
	log := log.FromContext(ctx)
	log.Debug("RecoverConfigs")
	configList, err := r.ListConfigsPerTarget(ctx, target)
	if err != nil {
		return nil, err
	}

	configs := []*config.Config{}

	for _, config := range configList.Items {
		if config.Status.AppliedConfig != nil {
			configs = append(configs, &config)
		}
	}

	log.Info("recovering target config", "count", len(configs))
	targetKey := storebackend.KeyFromNSN(target.GetNamespacedName())
	msg, err := r.recoverIntents(ctx, dsctx, targetKey, configs)
	if err != nil {
		// This is bad since this means we cannot recover the applied config
		// on a target. We set the target config status to Failed.
		// Most likely a human intervention is needed

		if serr := r.setRecoveredConfigs(ctx, target, func(cfg *configv1alpha1.Config) condv1alpha1.Condition {
			return configv1alpha1.ConfigFailed(cfg.GetGeneration(), msg)
		}); serr != nil {
			return ptr.To("recovery failed, and failed to update config status"), serr
		}

		return &msg, err
	}
	dsctx.MarkRecovered(true)
	log.Debug("recovered configs", "count", len(configs))

	if err := r.setRecoveredConfigs(ctx, target, func(cfg *configv1alpha1.Config) condv1alpha1.Condition {
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

// Transact applies the changed configs of the target and writes the status
// of every config of the target once.
func (r *Transactor) Transact(
	ctx context.Context,
	target *configv1alpha1.Target,
	dsctx *DatastoreHandle,
) (bool, error) {
	configList, err := r.ListConfigsPerTarget(ctx, target)
	if err != nil {
		return true, err
	}
	cfgs := make([]*configv1alpha1.Config, 0, len(configList.Items))
	for i := range configList.Items {
		cfg, err := toV1Alpha1Config(&configList.Items[i])
		if err != nil {
			return true, err
		}
		cfgs = append(cfgs, cfg)
	}

	results := map[string]txResult{}
	retry, err := r.transact(ctx, target, dsctx, configList, cfgs, results)
	if serr := r.writeStatus(ctx, cfgs, results); serr != nil {
		return true, errors.Join(err, serr)
	}
	return retry, err
}

// transact records the outcome per config in results. cfgs is kept in sync
// with the metadata patches, writeStatus uses it as the current state.
func (r *Transactor) transact(
	ctx context.Context,
	target *configv1alpha1.Target,
	dsctx *DatastoreHandle,
	configList *config.ConfigList,
	cfgs []*configv1alpha1.Config,
	results map[string]txResult,
) (bool, error) {
	log := log.FromContext(ctx)
	log.Debug("Transact")

	// reapply deviations for each config snippet
	// This ensure the deviations are applied
	for _, cfg := range configList.Items {
		if _, err := r.applyDeviation(ctx, &cfg); err != nil {
			return true, err
		}
	}

	// determine change
	configsToUpdate, configsToDelete := getConfigsToTransact(ctx, configList)

	if len(configsToUpdate) == 0 && len(configsToDelete) == 0 {
		log.Info("Transact skip, nothing to update")
		return false, nil
	}

	// before the dataserver has the intent: without the finalizer, a delete
	// would remove the Config without removing its intent
	for _, cfg := range cfgs {
		if _, ok := configsToUpdate[config.GetGVKNSN(cfg)]; !ok {
			continue
		}
		if err := r.addFinalizer(ctx, cfg); err != nil {
			return true, err
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
			results,
		)
	}
	log.Debug("transaction response", "rsp", prototext.Format(rsp))
	// ok case
	if _, err := dsctx.Client.TransactionConfirm(ctx, &sdcpb.TransactionConfirmRequest{
		DatastoreName: dsctx.DatastoreName,
		TransactionId: uuid.String(),
	}); err != nil {
		return true, err
	}
	for key, cfg := range configsToUpdate {
		applied, err := toV1Alpha1Config(cfg)
		if err != nil {
			return false, err
		}
		results[key] = txResult{
			configReady: configv1alpha1.ConfigReady(cfg.GetGeneration(), ""),
			applied:     &applied.Spec,
			schema:      dsctx.Schema,
		}
	}

	for _, cfg := range cfgs {
		if _, ok := configsToDelete[config.GetGVKNSN(cfg)]; !ok {
			continue
		}
		if err := r.removeFinalizer(ctx, cfg); err != nil {
			return true, err
		}
		if err := r.deleteDeviation(ctx, cfg); err != nil {
			return true, err
		}
	}

	return false, nil
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

// addFinalizer and removeFinalizer only touch this finalizer, and only patch
// when it changes. cfg takes the patched object.
func (r *Transactor) addFinalizer(ctx context.Context, cfg *configv1alpha1.Config) error {
	if controllerutil.ContainsFinalizer(cfg, finalizer) {
		return nil
	}
	log.FromContext(ctx).Debug("addFinalizer", "config", cfg.GetName())
	return r.patchMetadata(ctx, cfg, func() {
		controllerutil.AddFinalizer(cfg, finalizer)
	})
}

func (r *Transactor) removeFinalizer(ctx context.Context, cfg *configv1alpha1.Config) error {
	if !controllerutil.ContainsFinalizer(cfg, finalizer) {
		return nil
	}
	log.FromContext(ctx).Debug("removeFinalizer", "config", cfg.GetName())
	return r.patchMetadata(ctx, cfg, func() {
		controllerutil.RemoveFinalizer(cfg, finalizer)
	})
}

func (r *Transactor) deleteDeviation(ctx context.Context, cfg *configv1alpha1.Config) error {
	deviation := &configv1alpha1.Deviation{}
	deviation.Name = cfg.Name
	deviation.Namespace = cfg.Namespace
	if err := r.client.Delete(ctx, deviation); err != nil {
		if resource.IgnoreNotFound(err) != nil {
			return err
		}
	}
	return nil
}

func (r *Transactor) ListConfigsPerTarget(ctx context.Context, target *configv1alpha1.Target) (*config.ConfigList, error) {
	v1alpha1configList := &configv1alpha1.ConfigList{}
	if err := r.client.List(ctx, v1alpha1configList,
		client.InNamespace(target.GetNamespace()),
		client.MatchingLabels{
			config.TargetNamespaceKey: target.GetNamespace(),
			config.TargetNameKey:      target.GetName(),
		},
	); err != nil {
		return nil, err
	}

	configList := &config.ConfigList{}
	if err := configv1alpha1.Convert_v1alpha1_ConfigList_To_config_ConfigList(v1alpha1configList, configList, nil); err != nil {
		return nil, err
	}

	return configList, nil
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

func toV1Alpha1Config(cfg *config.Config) (*configv1alpha1.Config, error) {
	out := &configv1alpha1.Config{}
	if err := configv1alpha1.Convert_config_Config_To_v1alpha1_Config(cfg, out, nil); err != nil {
		return nil, err
	}
	return out, nil
}

func (r *Transactor) patchMetadata(
	ctx context.Context,
	obj client.Object,
	mutate func(),
) error {
	orig := obj.DeepCopyObject().(client.Object)
	mutate()
	// a stale cached object would drop a finalizer added in the meantime
	return r.client.Patch(ctx, obj, client.MergeFromWithOptions(orig, client.MergeFromWithOptimisticLock{}),
		&client.PatchOptions{FieldManager: r.fieldManager},
	)
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
	results map[string]txResult,
) (bool, error) {
	log := log.FromContext(ctx)
	log.Warn("handling transaction errors", "recoverable", recoverable)

	// If no response at all → apply same error to all configs.
	if rsp == nil {
		for key, cfg := range configsToTransact {
			results[key] = failedResult(ctx, cfg, "", globalErr, recoverable)
		}
		for key, cfg := range deletedConfigsToTransact {
			results[key] = failedResult(ctx, cfg, "", globalErr, false)
		}
		return recoverable, globalErr
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

	return recoverable, globalErr
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

// txResult is the outcome of the transaction for one config.
type txResult struct {
	configReady condv1alpha1.Condition
	applied     *configv1alpha1.ConfigSpec // success only
	schema      *configv1alpha1.ConfigStatusLastKnownGoodSchema
}

func (t txResult) applyTo(cfg *configv1alpha1.Config) {
	cfg.SetConditions(t.configReady)
	cfg.Status.DeviationGeneration = nil
	if t.configReady.Reason == string(condv1alpha1.ConditionReasonUnrecoverable) {
		cfg.Status.DeviationGeneration = ptr.To[int64](0)
	}
	if t.applied != nil {
		cfg.Status.AppliedConfig = t.applied
		cfg.Status.LastKnownGoodSchema = t.schema
	}
}

// writeStatus writes every config once: the transaction result where there
// is one, Ready from ConfigReady for all. All configs are attempted, so one
// failure does not lose the results of the others.
func (r *Transactor) writeStatus(ctx context.Context, cfgs []*configv1alpha1.Config, results map[string]txResult) error {
	var errs error
	for _, current := range cfgs {
		if current.GetDeletionTimestamp() != nil {
			continue
		}
		desired := current.DeepCopy()
		res, ok := results[config.GetGVKNSN(current)]
		if ok {
			res.applyTo(desired)
		}
		desired.SetConditions(desired.ReadyFromApplied())
		// a result comes from the dataserver, only a Ready refresh may be stale
		if err := r.applyStatus(ctx, current, desired, !ok); err != nil {
			errs = errors.Join(errs, err)
		}
	}
	return errs
}

// SetConfigsNotReady sets Ready on every Config of the target to the target
// stage that blocks it (configv1alpha1.ConfigNotReady). ConfigReady, the
// transaction state, is kept.
func (r *Transactor) SetConfigsNotReady(ctx context.Context, target *configv1alpha1.Target, ready condv1alpha1.Condition) error {
	return r.updateConfigsForTarget(ctx, target, true, func(cfg *configv1alpha1.Config) bool {
		cfg.SetConditions(ready)
		return true
	})
}

// setRecoveredConfigs sets ConfigReady on the Configs that were part of the
// recovery (the ones with an applied config). Ready is left to the target
// config controller: the target is not ready yet while recovering.
func (r *Transactor) setRecoveredConfigs(ctx context.Context, target *configv1alpha1.Target, cond func(cfg *configv1alpha1.Config) condv1alpha1.Condition) error {
	return r.updateConfigsForTarget(ctx, target, false, func(cfg *configv1alpha1.Config) bool {
		if cfg.Status.AppliedConfig == nil {
			return false
		}
		cfg.SetConditions(cond(cfg))
		cfg.Status.DeviationGeneration = nil // a transaction outcome, see applyStatus
		return true
	})
}

// updateConfigsForTarget applies mutate to every Config of the target and
// writes the result. mutate returns false to leave a Config alone.
//
// deviationGeneration is carried over unchanged: callers that write a
// transaction outcome clear it themselves (setRecoveredConfigs), the
// Ready-only updates must keep the marker the unrecoverable path set.
//
// conditional is set by the Ready-only updates, see applyStatus.
func (r *Transactor) updateConfigsForTarget(ctx context.Context, target *configv1alpha1.Target, conditional bool, mutate func(cfg *configv1alpha1.Config) bool) error {
	cfgList, err := r.ListConfigsPerTarget(ctx, target)
	if err != nil {
		return err
	}
	for i := range cfgList.Items {
		current, err := toV1Alpha1Config(&cfgList.Items[i])
		if err != nil {
			return err
		}
		desired := current.DeepCopy()
		if !mutate(desired) {
			continue
		}
		if err := r.applyStatus(ctx, current, desired, conditional); err != nil {
			return err
		}
	}
	return nil
}

// applyStatus is the single writer of the Config status owned by the
// transactor field manager: Ready, ConfigReady, lastKnownGoodSchema,
// appliedConfig and deviationGeneration. With SSA, whatever this manager
// applied before and leaves out now is removed, so every apply carries the
// full owned set from desired. The same mechanism drops the legacy
// conditions this manager wrote (LegacyConfigConditionTypes).
//
// deviationGeneration is the exception: the unrecoverable error path sets it
// (to 0), a transaction outcome (success, recoverable failure, recovery)
// clears it, and the Ready-only updates (updateConfigsForTarget) carry it
// over unchanged.
//
// lastTransitionTime only moves when a condition's status flips. Messages
// are already bounded by the condition constructors.
//
// conditional sends current's resourceVersion: current comes from the cache,
// a stale copy is rejected (409) instead of overwriting a newer status (#533).
// Transaction outcomes stay unconditional, a rejection would re-transact.
func (r *Transactor) applyStatus(ctx context.Context, current, desired *configv1alpha1.Config, conditional bool) error {
	changed := !reflect.DeepEqual(desired.Status.LastKnownGoodSchema, current.Status.LastKnownGoodSchema) ||
		!reflect.DeepEqual(desired.Status.AppliedConfig, current.Status.AppliedConfig) ||
		!reflect.DeepEqual(desired.Status.DeviationGeneration, current.Status.DeviationGeneration)
	for _, t := range configv1alpha1.LegacyConfigConditionTypes {
		if current.Status.HasCondition(t) {
			changed = true
		}
	}

	conds := make([]condv1alpha1.Condition, 0, 2)
	for _, t := range []condv1alpha1.ConditionType{condv1alpha1.ConditionTypeReady, configv1alpha1.ConditionTypeConfigReady} {
		if !desired.Status.HasCondition(t) {
			continue
		}
		newCond, oldCond := desired.GetCondition(t), current.GetCondition(t)
		if !current.Status.HasCondition(t) || !newCond.Equal(oldCond) {
			changed = true
		}
		conds = append(conds, keepTransitionTime(newCond, oldCond))
	}
	if !changed {
		return nil
	}

	statusApply := configv1alpha1apply.ConfigStatus().WithConditions(conds...)
	if s := desired.Status.LastKnownGoodSchema; s != nil {
		statusApply = statusApply.WithLastKnownGoodSchema(schemaToApply(s))
	}
	if a := desired.Status.AppliedConfig; a != nil {
		statusApply = statusApply.WithAppliedConfig(configSpecToApply(a))
	}
	if g := desired.Status.DeviationGeneration; g != nil {
		statusApply = statusApply.WithDeviationGeneration(*g)
	}

	log.FromContext(ctx).Debug("apply config status",
		"config", desired.GetNamespacedName().String(),
		"ready", desired.GetCondition(condv1alpha1.ConditionTypeReady).IsTrue(),
		"reason", desired.GetCondition(condv1alpha1.ConditionTypeReady).Reason)

	configApply := configv1alpha1apply.Config(desired.Name, desired.Namespace).WithStatus(statusApply)
	if conditional {
		configApply = configApply.WithResourceVersion(current.ResourceVersion)
	}

	return r.client.Status().Apply(ctx, configApply,
		&client.SubResourceApplyOptions{
			ApplyOptions: client.ApplyOptions{FieldManager: r.fieldManager},
		})
}

func schemaToApply(s *configv1alpha1.ConfigStatusLastKnownGoodSchema) *configv1alpha1apply.ConfigStatusLastKnownGoodSchemaApplyConfiguration {
	if s == nil {
		return nil
	}
	a := configv1alpha1apply.ConfigStatusLastKnownGoodSchema()
	if s.Type != "" {
		a.WithType(s.Type)
	}
	if s.Vendor != "" {
		a.WithVendor(s.Vendor)
	}
	if s.Version != "" {
		a.WithVersion(s.Version)
	}
	return a
}

func configSpecToApply(s *configv1alpha1.ConfigSpec) *configv1alpha1apply.ConfigSpecApplyConfiguration {
	if s == nil {
		return nil
	}

	a := configv1alpha1apply.ConfigSpec().
		WithPriority(s.Priority)

	if s.Revertive != nil {
		a.WithRevertive(*s.Revertive)
	}

	if s.Lifecycle != nil {
		a.WithLifecycle(lifecycleToApply(s.Lifecycle))
	}

	for i := range s.Config {
		a.WithConfig(configBlobToApply(&s.Config[i]))
	}
	return a
}

func lifecycleToApply(l *configv1alpha1.Lifecycle) *configv1alpha1apply.LifecycleApplyConfiguration {
	if l == nil {
		return nil
	}
	a := configv1alpha1apply.Lifecycle()
	a.WithDeletionPolicy(l.DeletionPolicy)

	return a
}

func configBlobToApply(b *configv1alpha1.ConfigBlob) *configv1alpha1apply.ConfigBlobApplyConfiguration {
	if b == nil {
		return nil
	}
	a := configv1alpha1apply.ConfigBlob()
	if b.Path != "" {
		a.WithPath(b.Path)
	}
	a.WithValue(b.Value)
	return a
}

// keepTransitionTime carries the old lastTransitionTime over when the status
// did not change, so a message or reason update is not reported as a
// transition.
func keepTransitionTime(newCond, oldCond condv1alpha1.Condition) condv1alpha1.Condition {
	if oldCond.Status == newCond.Status && !oldCond.LastTransitionTime.IsZero() {
		newCond.LastTransitionTime = oldCond.LastTransitionTime
	}
	return newCond
}
