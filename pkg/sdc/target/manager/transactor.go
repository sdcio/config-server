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
	"encoding/json"
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
	invv1alpha1 "github.com/sdcio/config-server/apis/inv/v1alpha1"
	"github.com/sdcio/config-server/pkg/reconcilers/resource"
	sdcpb "github.com/sdcio/sdc-protos/sdcpb"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/encoding/prototext"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/sets"
	genericapirequest "k8s.io/apiserver/pkg/endpoints/request"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client"
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

func (r *Transactor) RecoverConfigs(ctx context.Context, target *invv1alpha1.Target, dsctx *DatastoreHandle) (*string, error) {
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

		if err := r.SetConfigsTargetConditionForTarget(
			ctx,
			target,
			configv1alpha1.ConfigFailed(msg),
		); err != nil {
			// recovery succeeded but we failed to patch status -> surface it
			return ptr.To("recovered, but failed to update config status"), err
		}

		return &msg, err
	}
	dsctx.MarkRecovered(true)
	log.Debug("recovered configs", "count", len(configs))

	if err := r.SetConfigsTargetConditionForTarget(
        ctx,
        target,
        configv1alpha1.ConfigReady("target recovered"),
    ); err != nil {
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
	for _, config := range configs {
		update, err := GetIntentUpdate(ctx, key, config, false)
		if err != nil {
			return "", err
		}
		intents = append(intents, &sdcpb.TransactionIntent{
			Intent:   GetGVKNSN(config),
			Priority: int32(config.Spec.Priority),
			Update:   update,
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

func (r *Transactor) Transact(ctx context.Context, target *invv1alpha1.Target, dsctx *DatastoreHandle) (bool, error) {
	log := log.FromContext(ctx)
	log.Debug("Transact")
	// get all configs for the target
	configList, err := r.ListConfigsPerTarget(ctx, target)
	if err != nil {
		return true, err
	}
	// reapply deviations for each config snippet
	for _, config := range configList.Items {
		if _, err := r.applyDeviation(ctx, &config); err != nil {
			return true, err
		}
	}

	// determine change
	configsToUpdate, configsToDelete := getConfigsToTransact(ctx, configList)

	if len(configsToUpdate) == 0 &&
		len(configsToDelete) == 0 {
		log.Info("Transact skip, nothing to update")
		return false, nil
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

		return r.handleTransactionErrors(
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
		return true, err
	}
	for _, configOrig := range configsToUpdate {
		config, err := toV1Alpha1Config(configOrig)
		if err != nil {
			return false, err
		}
		if err := r.applyFinalizer(ctx, config); err != nil {
			return true, err
		}

		if err := r.updateConfigWithSuccess(ctx, config, dsctx.Schema, ""); err != nil {
			return true, err
		}
	}

	for _, configOrig := range configsToDelete {
		config := &configv1alpha1.Config{}
		if err := configv1alpha1.Convert_config_Config_To_v1alpha1_Config(configOrig, config, nil); err != nil {
			return true, err
		}
		if err := r.deleteFinalizer(ctx, config); err != nil {
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

	for key, config := range configsToUpdate {
		configsToUpdateSet.Insert(key)
		update, err := GetIntentUpdate(ctx, targetKey, config, true)
		if err != nil {
			log.Error("Transaction getIntentUpdate config", "error", err)
			return nil, err
		}
		intents = append(intents, &sdcpb.TransactionIntent{
			Intent:   GetGVKNSN(config),
			Priority: int32(config.Spec.Priority),
			Update:   update,
		})
	}
	for key, config := range configsToDelete {
		configsToDeleteSet.Insert(key)
		intents = append(intents, &sdcpb.TransactionIntent{
			Intent: GetGVKNSN(config),
			//Priority: int32(config.Spec.Priority),
			Delete:              true,
			DeleteIgnoreNoExist: true,
			Orphan:              config.Orphan(),
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

func (r *Transactor) updateConfigWithError(ctx context.Context, config *configv1alpha1.Config, msg string, err error, recoverable bool) error {
	log := log.FromContext(ctx)
	log.Warn("updateConfigWithError", "config", config.GetName(), "recoverable", recoverable, "msg", msg, "err", err)

	configOrig := config.DeepCopy()
	patch := client.MergeFrom(configOrig)

	if err != nil {
		msg = fmt.Sprintf("%s err %s", msg, err.Error())
	}

	config.SetFinalizers([]string{finalizer})
	if recoverable {
		// IMPRTANT TO USE THIS TYPE
		config.SetConditions(configv1alpha1.ConfigFailed(msg))
	} else {
		newMessage := condv1alpha1.UnrecoverableMessage{
			ResourceVersion: config.GetResourceVersion(),
			Message:         msg,
		}
		newmsg, err := json.Marshal(newMessage)
		if err != nil {
			return err
		}
		config.Status.DeviationGeneration = nil
		config.SetConditions(condv1alpha1.FailedUnRecoverable(string(newmsg)))
	}

	return r.client.Status().Patch(ctx, config, patch, &client.SubResourcePatchOptions{
		PatchOptions: client.PatchOptions{
			FieldManager: r.fieldManager,
		},
	})
}

func (r *Transactor) applyFinalizer(ctx context.Context, config *configv1alpha1.Config) error {
	log := log.FromContext(ctx)
	log.Debug("applyFinalizer")

	return r.patchMetadata(ctx, config, func() {
		config.SetFinalizers([]string{finalizer})
	})
}

func (r *Transactor) deleteFinalizer(ctx context.Context, config *configv1alpha1.Config) error {
	log := log.FromContext(ctx)
	log.Debug("deleteFinalizer")

	return r.patchMetadata(ctx, config, func() {
		config.SetFinalizers([]string{})
	})
}

func (r *Transactor) updateConfigWithSuccess(
	ctx context.Context,
	cfg *configv1alpha1.Config,
	schema *configv1alpha1.ConfigStatusLastKnownGoodSchema,
	msg string,
) error {
	log := log.FromContext(ctx)
	log.Debug("updateConfigWithSuccess", "config", cfg.GetName())

	// THE TYPE IS IMPORTANT here
	cond := configv1alpha1.ConfigReady(msg)

	return r.patchStatusIfChanged(ctx, cfg, func(c *configv1alpha1.Config) {
		c.SetConditions(cond)
		c.Status.LastKnownGoodSchema = schema
		c.Status.AppliedConfig = &c.Spec
	}, func(old, new *configv1alpha1.Config) bool {
		// only write if the ready condition or these fields changed
		if !new.GetCondition(condv1alpha1.ConditionTypeReady).Equal(old.GetCondition(condv1alpha1.ConditionTypeReady)) {
			return true
		}
		if !reflect.DeepEqual(old.Status.LastKnownGoodSchema, new.Status.LastKnownGoodSchema) {
			return true
		}
		if !reflect.DeepEqual(old.Status.AppliedConfig, new.Status.AppliedConfig) {
			return true
		}
		return false
	})
}

func (r *Transactor) ListConfigsPerTarget(ctx context.Context, target *invv1alpha1.Target) (*config.ConfigList, error) {
	ctx = genericapirequest.WithNamespace(ctx, target.GetNamespace())

	opts := []client.ListOption{
		client.MatchingLabels{
			config.TargetNamespaceKey: target.GetNamespace(),
			config.TargetNameKey:      target.GetName(),
		},
	}
	v1alpha1configList := &configv1alpha1.ConfigList{}
	if err := r.client.List(ctx, v1alpha1configList, opts...); err != nil {
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
		Name:      config.Name,
		Namespace: config.Namespace,
	}

	deviation := &configv1alpha1.Deviation{}
	if err := r.client.Get(ctx, key, deviation); err != nil {
		if resource.IgnoreNotFound(err) != nil {
			return configv1alpha1.Deviation{}, err
		}
		// Not found: create new deviation
		newDeviation := configv1alpha1.BuildDeviation(metav1.ObjectMeta{
			Name:            config.Name,
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

/*
func (r *Transactor) patchStatus(
	ctx context.Context,
	obj client.Object,
	mutate func(),
) error {
	orig := obj.DeepCopyObject().(client.Object)
	mutate()
	return r.client.Status().Patch(ctx, obj, client.MergeFrom(orig),
		&client.SubResourcePatchOptions{
			PatchOptions: client.PatchOptions{FieldManager: r.fieldManager},
		},
	)
}
*/

func (r *Transactor) patchMetadata(
	ctx context.Context,
	obj client.Object,
	mutate func(),
) error {
	orig := obj.DeepCopyObject().(client.Object)
	mutate()
	return r.client.Patch(ctx, obj, client.MergeFrom(orig),
		&client.SubResourcePatchOptions{
			PatchOptions: client.PatchOptions{FieldManager: r.fieldManager},
		},
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

	// Classify configs: update / delete / non-recoverable / noop
	for i := range configList.Items {
		cfg := &configList.Items[i]
		key := GetGVKNSN(cfg)

		switch {
		case !cfg.IsRecoverable(ctx):
			nonRecoverable[key] = cfg

		case cfg.GetDeletionTimestamp() != nil:
			configsToDelete[key] = cfg

		case cfg.Status.AppliedConfig != nil &&
			cfg.Spec.GetShaSum(ctx) == cfg.Status.AppliedConfig.GetShaSum(ctx):
			// no change, skip
			continue

		default:
			configsToUpdate[key] = cfg
		}
	}

	log.Debug("getConfigsAndDeviationsToTransact classification start",
		"configsToUpdate", mapKeys(configsToUpdate),
		"configsToDelete", mapKeys(configsToDelete),
		"nonRecoverable", mapKeys(nonRecoverable),
	)

	// --- 5) If we have changes, retry non-recoverables

	if len(configsToUpdate) > 0 || len(configsToDelete) > 0 {
		for key, cfg := range nonRecoverable {
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

func (r *Transactor) handleTransactionErrors(
	ctx context.Context,
	rsp *sdcpb.TransactionSetResponse,
	configsToTransact, deletedConfigsToTransact map[string]*config.Config,
	globalErr error,
	recoverable bool,
) (bool, error) {
	log := log.FromContext(ctx)
	log.Warn("handling transaction errors", "recoverable", recoverable)

	// If no response at all → apply same error to all configs.
	if rsp == nil {
		for _, cfg := range configsToTransact {
			if err := r.processFailedConfig(ctx, cfg, "", globalErr, recoverable); err != nil {
				return true, err
			}
		}
		for _, cfg := range deletedConfigsToTransact {
			if err := r.processFailedConfig(ctx, cfg, "", globalErr, false); err != nil {
				return true, err
			}
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
			if err := r.processFailedConfig(ctx, cfg, msg, errs, false); err != nil {
				return true, err
			}
			continue
		}
		if cfg, ok := deletedConfigsToTransact[intentName]; ok {
			if err := r.processFailedConfig(ctx, cfg, msg, errs, false); err != nil {
				return true, err
			}
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
		for _, cfg := range configsToTransact {
			if err := r.processFailedConfig(ctx, cfg, "", globalErr, recoverable); err != nil {
				return true, err
			}
		}
		for _, cfg := range deletedConfigsToTransact {
			if err := r.processFailedConfig(ctx, cfg, "", globalErr, false); err != nil {
				return true, err
			}
		}
	}

	return recoverable, globalErr
}

func (r *Transactor) processFailedConfig(
	ctx context.Context,
	configOrig *config.Config,
	msg string,
	origErr error,
	recoverable bool,
) error {
	config, err := toV1Alpha1Config(configOrig)
	if err != nil {
		return err
	}
	if err := r.applyFinalizer(ctx, config); err != nil {
		return err
	}
	return r.updateConfigWithError(ctx, config, msg, origErr, recoverable)
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
		// gRPC status code to determine recoverability
		if statusErr, ok := status.FromError(err); ok {
			switch statusErr.Code() {
			case codes.Aborted, codes.ResourceExhausted:
				result.Recoverable = true
			default:
				result.Recoverable = false
			}
		}
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

func (r *Transactor) patchStatusIfChanged(
	ctx context.Context,
	cfg *configv1alpha1.Config,
	mutate func(c *configv1alpha1.Config),
	changed func(old, new *configv1alpha1.Config) bool,
) error {
	orig := cfg.DeepCopy()

	mutate(cfg)

	if changed != nil && !changed(orig, cfg) {
		return nil
	}

	return r.client.Status().Patch(ctx, cfg, client.MergeFrom(orig),
		&client.SubResourcePatchOptions{
			PatchOptions: client.PatchOptions{FieldManager: r.fieldManager},
		},
	)
}

func (r *Transactor) SetConfigsTargetConditionForTarget(
	ctx context.Context,
	target *invv1alpha1.Target,
	targetCond condv1alpha1.Condition,
) error {
	//log := log.FromContext(ctx)

	// Reuse your existing lister (returns config.ConfigList)
	cfgList, err := r.ListConfigsPerTarget(ctx, target)
	if err != nil {
		return err
	}

	for i := range cfgList.Items {
		// convert to v1alpha1 for patching
		v1cfg, err := toV1Alpha1Config(&cfgList.Items[i])
		if err != nil {
			return err
		}

		err = r.patchStatusIfChanged(ctx, v1cfg, func(c *configv1alpha1.Config) {
			// 1) set TargetReady condition
			c.SetConditions(targetCond)
			// 2) derive overall Ready from ConfigReady + TargetReady
			c.SetOverallStatus()
		}, func(old, new *configv1alpha1.Config) bool {
			// compare the two condition types you may change
			oldT := old.GetCondition(condv1alpha1.ConditionType(targetCond.Type))
			newT := new.GetCondition(condv1alpha1.ConditionType(targetCond.Type))
			if !newT.Equal(oldT) {
				return true
			}

			// 2) did overall Ready change?
			oldR := old.GetCondition(condv1alpha1.ConditionTypeReady)
			newR := new.GetCondition(condv1alpha1.ConditionTypeReady)
			return !newR.Equal(oldR) 
		})
		if err != nil {
			return err
		}
	}

	return nil
}
