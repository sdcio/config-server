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

package transactor

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"strings"

	"github.com/google/uuid"
	"github.com/henderiw/apiserver-store/pkg/storebackend"
	"github.com/henderiw/logger/log"
	condv1alpha1 "github.com/sdcio/config-server/apis/condition/v1alpha1"
	"github.com/sdcio/config-server/apis/config"
	configv1alpha1 "github.com/sdcio/config-server/apis/config/v1alpha1"
	invv1alpha1 "github.com/sdcio/config-server/apis/inv/v1alpha1"
	"github.com/sdcio/config-server/pkg/reconcilers/resource"
	"github.com/sdcio/config-server/pkg/target"
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
	client                client.Client // k8s client
	fieldManager          string
	fieldManagerFinalizer string
}

func New(client client.Client, fieldManager, fieldManagerFinalizer string) *Transactor {
	return &Transactor{
		client:                client,
		fieldManager:          fieldManager,
		fieldManagerFinalizer: fieldManagerFinalizer,
	}
}

func (r *Transactor) RecoverConfigs(ctx context.Context, target *invv1alpha1.Target, tctx *target.Context) (*string, error) {
	log := log.FromContext(ctx)
	log.Info("RecoverConfigs")
	configList, err := r.listConfigsPerTarget(ctx, target)
	if err != nil {
		return nil, err
	}
	// get all CONFIG deviations for a given target, excludes TARGET deviations
	deviationMap, err := r.listDeviationsPerTarget(ctx, target)
	if err != nil {
		return nil, err
	}

	configs := []*config.Config{}
	deviations := []*config.Deviation{}

	for _, config := range configList.Items {
		key := GetGVKNSN(&config)
		if config.Status.AppliedConfig != nil {
			configs = append(configs, &config)
		}
		if !config.IsRevertive() {
			deviation, ok := deviationMap[key]
			if !ok {
				log.Warn("deviation missing for config", "config", key)
				continue
			}
			// dont include deviations if there are none
			if len(deviation.Spec.Deviations) != 0 {
				labels := deviation.GetLabels()
				if labels == nil {
					labels = map[string]string{}
				}
				labels["priority"] = strconv.Itoa(int(config.Spec.Priority))
				deviation.SetLabels(labels)
				deviations = append(deviations, deviation)
			}
		}
	}
	//sort.Slice(configs, func(i, j int) bool {
	//	return configs[i].CreationTimestamp.Before(&configs[j].CreationTimestamp)
	//})
	if len(configs) == 0 && len(deviations) == 0 {
		tctx.SetRecoveredConfigsState(ctx)
		log.Info("recovered configs, nothing to recover", "count", len(configs), "deviations", len(deviations))
		return nil, nil
	}
	log.Info("recovering target config", "count", len(configs), "deviations", len(deviations))
	targetKey := storebackend.KeyFromNSN(target.GetNamespacedName())
	msg, err := tctx.RecoverIntents(ctx, targetKey, configs, deviations)
	if err != nil {
		// This is bad since this means we cannot recover the applied config
		// on a target. We set the target config status to Failed.
		// Most likely a human intervention is needed
		return &msg, err
	}
	tctx.SetRecoveredConfigsState(ctx)
	log.Info("recovered configs", "count", len(configs), "deviations", len(deviations))
	return nil, nil
}

func (r *Transactor) Transact(ctx context.Context, target *invv1alpha1.Target, tctx *target.Context) (bool, error) {
	log := log.FromContext(ctx)
	log.Info("Transact")
	// get all configs for the target
	configList, err := r.listConfigsPerTarget(ctx, target)
	if err != nil {
		return true, err
	}
	// reapply deviations for each config snippet
	for _, config := range configList.Items {
		if _, err := r.applyDeviation(ctx, &config); err != nil {
			return true, err
		}
	}
	// get all deviations for the target
	deviationMap, err := r.listDeviationsPerTarget(ctx, target)
	if err != nil {
		return true, err
	}

	// determine change
	configsToUpdate, configsToDelete, deviationsToUpdate, deviationsToDelete := getConfigsAndDeviationsToTransact(ctx, configList, deviationMap)

	if len(configsToUpdate) == 0 &&
		len(configsToDelete) == 0 &&
		len(deviationsToUpdate) == 0 &&
		len(deviationsToDelete) == 0 {
		log.Info("Transact skip, nothing to update")
		return false, nil
	}

	targetKey := storebackend.KeyFromNSN(target.GetNamespacedName())

	uuid := uuid.New()

	rsp, err := tctx.SetIntents(
		ctx,
		targetKey,
		uuid.String(),
		configsToUpdate,
		configsToDelete,
		deviationsToUpdate,
		deviationsToDelete,
		false)
	// we first collect the warnings and errors -> to determine error or not
	result := analyzeIntentResponse(err, rsp)

	for _, w := range result.GlobalWarnings {
		log.Warn("transaction warning", "warning", w)
	}
	if result.GlobalError != nil || result.IntentErrors != nil {
		log.Info("transaction failed",
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
	if err := tctx.TransactionConfirm(ctx, targetKey.String(), uuid.String()); err != nil {
		return false, err
	}
	for configKey, configOrig := range configsToUpdate {
		config, err := toV1Alpha1Config(configOrig)
		if err != nil {
			return false, err
		}
		if err := r.applyFinalizer(ctx, config); err != nil {
			return true, err
		}

		var deviationGeneration *int64
		deviation, ok := deviationsToUpdate[configKey]
		if ok && !config.IsRevertive() {
			deviationGeneration = &deviation.Generation
		}

		if err := r.updateConfigWithSuccess(ctx, config, (*configv1alpha1.ConfigStatusLastKnownGoodSchema)(tctx.GetSchema()), deviationGeneration, ""); err != nil {
			return true, err
		}

		if ok && config.IsRevertive() {
			if err := r.clearDeviation(ctx, deviation); err != nil {
				return true, err
			}
		}
	}

	for configKey, configOrig := range configsToDelete {
		config := &configv1alpha1.Config{}
		if err := configv1alpha1.Convert_config_Config_To_v1alpha1_Config(configOrig, config, nil); err != nil {
			return true, err
		}
		if err := r.deleteFinalizer(ctx, config); err != nil {
			return true, err
		}
		deviation, ok := deviationMap[configKey]
		if ok {
			if err := r.clearDeviation(ctx, deviation); err != nil {
				return true, err
			}
		}
	}

	return false, nil
}

func (r *Transactor) updateConfigWithError(ctx context.Context, config *configv1alpha1.Config, msg string, err error, recoverable bool) error {
	log := log.FromContext(ctx)
	log.Info("updateConfigWithError", "config", config.GetName(), "recoverable", recoverable, "msg", msg, "err", err)

	configOrig := config.DeepCopy()
	patch := client.MergeFrom(configOrig)

	if err != nil {
		msg = fmt.Sprintf("%s err %s", msg, err.Error())
	}

	config.SetFinalizers([]string{finalizer})
	if recoverable {
		config.SetConditions(condv1alpha1.Failed(msg))
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
	log.Info("applyFinalizer")

	return r.patchMetadata(ctx, config, func() {
		config.SetFinalizers([]string{finalizer})
	})
}

func (r *Transactor) deleteFinalizer(ctx context.Context, config *configv1alpha1.Config) error {
	log := log.FromContext(ctx)
	log.Info("deleteFinalizer")

	return r.patchMetadata(ctx, config, func() {
		config.SetFinalizers([]string{})
	})
}

func (r *Transactor) updateConfigWithSuccess(
	ctx context.Context,
	config *configv1alpha1.Config,
	schema *configv1alpha1.ConfigStatusLastKnownGoodSchema,
	deviationGeneration *int64,
	msg string,
) error {
	log := log.FromContext(ctx)
	log.Info("updateConfigWithSuccess", "config", config.GetName())

	return r.patchStatus(ctx, config, func() {
		config.SetConditions(condv1alpha1.ReadyWithMsg(msg))
		config.Status.LastKnownGoodSchema = schema
		config.Status.AppliedConfig = &config.Spec
		if config.IsRevertive() {
			config.Status.DeviationGeneration = nil
		} else {
			config.Status.DeviationGeneration = deviationGeneration
		}
	})
}

func (r *Transactor) listConfigsPerTarget(ctx context.Context, target *invv1alpha1.Target) (*config.ConfigList, error) {
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

// listDeviationsPerTarget retrieves all CONFIG deviations for a given target, excludes TARGET deviations
func (r *Transactor) listDeviationsPerTarget(ctx context.Context, target *invv1alpha1.Target) (map[string]*config.Deviation, error) {
	ctx = genericapirequest.WithNamespace(ctx, target.GetNamespace())

	opts := []client.ListOption{
		client.MatchingLabels{
			config.TargetNamespaceKey: target.GetNamespace(),
			config.TargetNameKey:      target.GetName(),
		},
	}
	v1alpha1deviationList := &configv1alpha1.DeviationList{}
	if err := r.client.List(ctx, v1alpha1deviationList, opts...); err != nil {
		return nil, err
	}
	deviationList := &config.DeviationList{}
	if err := configv1alpha1.Convert_v1alpha1_DeviationList_To_config_DeviationList(v1alpha1deviationList, deviationList, nil); err != nil {
		return nil, err
	}

	deviationMap := map[string]*config.Deviation{}
	for i := range deviationList.Items {
		dev := deviationList.Items[i]
		// dont include deviations for the device
		if dev.Spec.DeviationType != nil && *dev.Spec.DeviationType == config.DeviationType_TARGET {
			continue
		}
		deviationMap[GetGVKNSN(&dev)] = &dev
	}

	return deviationMap, nil
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

func (r *Transactor) clearDeviation(ctx context.Context, deviation *config.Deviation) error {
	v1alpha1deviation, err := toV1Alpha1Deviation(deviation)
	if err != nil {
		return err
	}

	return r.patchSpec(ctx, v1alpha1deviation, func() {
		v1alpha1deviation.Spec.Deviations = []configv1alpha1.ConfigDeviation{}
	})
}

func toV1Alpha1Config(cfg *config.Config) (*configv1alpha1.Config, error) {
	out := &configv1alpha1.Config{}
	if err := configv1alpha1.Convert_config_Config_To_v1alpha1_Config(cfg, out, nil); err != nil {
		return nil, err
	}
	return out, nil
}

func toV1Alpha1Deviation(cfg *config.Deviation) (*configv1alpha1.Deviation, error) {
	out := &configv1alpha1.Deviation{}
	if err := configv1alpha1.Convert_config_Deviation_To_v1alpha1_Deviation(cfg, out, nil); err != nil {
		return nil, err
	}
	return out, nil
}

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

func (r *Transactor) patchMetadata(
	ctx context.Context,
	obj client.Object,
	mutate func(),
) error {
	orig := obj.DeepCopyObject().(client.Object)
	mutate()
	return r.client.Patch(ctx, obj, client.MergeFrom(orig),
		&client.SubResourcePatchOptions{
			PatchOptions: client.PatchOptions{FieldManager: r.fieldManagerFinalizer},
		},
	)
}

func (r *Transactor) patchSpec(
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

func safeCopyLabels(src map[string]string) map[string]string {
	if src == nil {
		return map[string]string{}
	}
	dst := make(map[string]string, len(src))
	for k, v := range src {
		dst[k] = v
	}
	return dst
}

func getConfigsAndDeviationsToTransact(
	ctx context.Context,
	configList *config.ConfigList,
	deviationMap map[string]*config.Deviation,
) (map[string]*config.Config, map[string]*config.Config, map[string]*config.Deviation, map[string]*config.Deviation) {
	log := log.FromContext(ctx)

	configsToUpdate := make(map[string]*config.Config)
	configsToDelete := make(map[string]*config.Config)
	nonRecoverable := make(map[string]*config.Config)
	deviationsToUpdate := make(map[string]*config.Deviation)
	deviationsToDelete := make(map[string]*config.Deviation)

	configsToUpdateSet := sets.New[string]()
	configsToDeleteSet := sets.New[string]()
	nonRecoverableSet := sets.New[string]()
	deviationsToUpdateSet := sets.New[string]()
	deviationsToDeleteSet := sets.New[string]()

	// collect configs to apply
	for i := range configList.Items {
		config := &configList.Items[i]
		key := GetGVKNSN(config)

		switch {
		case !config.IsRecoverable():
			nonRecoverable[key] = config
			nonRecoverableSet.Insert(key)
			continue

		case config.GetDeletionTimestamp() != nil:
			configsToDelete[key] = config
			configsToDeleteSet.Insert(key)

		case config.Status.AppliedConfig != nil &&
			config.Spec.GetShaSum(ctx) == config.Status.AppliedConfig.GetShaSum(ctx):
			continue

		default:
			configsToUpdate[key] = config
			configsToUpdateSet.Insert(key)
		}
	}
	// Collect deviations to apply

	// handle deviations
	for i := range configList.Items {
		cfg := &configList.Items[i]
		key := GetGVKNSN(cfg)

		deviation, ok := deviationMap[key]
		if !ok {
			log.Warn("deviation missing for config", "config", key)
			deviation = config.BuildDeviation(metav1.ObjectMeta{Name: cfg.GetName(), Namespace: cfg.GetNamespace()}, nil, nil)
		}

		switch {
		case !cfg.IsRecoverable():
			continue

		case cfg.GetDeletionTimestamp() != nil:
			if !cfg.IsRevertive() {
				labels := safeCopyLabels(deviation.GetLabels())
				labels["orphan"] = strconv.FormatBool(cfg.Orphan())
				deviation.SetLabels(labels)
				deviationsToDelete[key] = deviation
				deviationsToDeleteSet.Insert(key)
			}
			continue
		default:
			if cfg.IsRevertive() {
				if deviation.HasNotAppliedDeviation() {
					log.Info("config included due to non revertive deviations", "key", key, "revertive", true)
					configsToUpdate[key] = cfg
					configsToUpdateSet.Insert(key)
				}
			} else {
				// check for change of deviation
				if cfg.HashDeviationGenerationChanged(*deviation) {
					if deviation.HasNotAppliedDeviation() {
						//change
						// safe copy of labels
						labels := safeCopyLabels(deviation.GetLabels())
						labels["priority"] = strconv.Itoa(int(cfg.Spec.Priority))
						deviation.SetLabels(labels)
						deviationsToUpdate[key] = deviation
						deviationsToUpdateSet.Insert(key)
					} else {
						labels := safeCopyLabels(deviation.GetLabels())
						labels["orphan"] = strconv.FormatBool(cfg.Orphan())
						deviation.SetLabels(labels)
						deviationsToDelete[key] = deviation
						deviationsToDeleteSet.Insert(key)
					}
					
				}		
				if len(deviation.Spec.Deviations) == 0 {
					labels := safeCopyLabels(deviation.GetLabels())
					labels["orphan"] = strconv.FormatBool(cfg.Orphan())
					deviationsToDelete[key] = deviation
					deviationsToDeleteSet.Insert(key)
				}	
			}
		}	
	}

	// for every config we create and delete we will include the deviations for non revertive configs.
	for key, cfg := range configsToUpdate {
		deviation, ok := deviationMap[key]
		if !ok {
			log.Warn("deviation missing for config", "config", key)
			deviation = config.BuildDeviation(metav1.ObjectMeta{Name: cfg.GetName(), Namespace: cfg.GetNamespace()}, nil, nil)
		}

		if !cfg.IsRevertive() {
			if len(deviation.Spec.Deviations) != 0 {
				if deviation.HasNotAppliedDeviation() {
					//change
					// safe copy of labels
					labels := safeCopyLabels(deviation.GetLabels())
					labels["priority"] = strconv.Itoa(int(cfg.Spec.Priority))
					deviation.SetLabels(labels)
					deviationsToUpdate[key] = deviation
					deviationsToUpdateSet.Insert(key)
				} else {
					labels := safeCopyLabels(deviation.GetLabels())
					labels["orphan"] = strconv.FormatBool(cfg.Orphan())
					deviation.SetLabels(labels)
					deviationsToDelete[key] = deviation
					deviationsToDeleteSet.Insert(key)
				}		
			} else {
				labels := safeCopyLabels(deviation.GetLabels())
				labels["orphan"] = strconv.FormatBool(cfg.Orphan())
				deviation.SetLabels(labels)
				deviationsToDelete[key] = deviation
				deviationsToDeleteSet.Insert(key)		
			}
		}
	}

	for key, cfg := range configsToDelete {
		deviation, ok := deviationMap[key]
		if !ok {
			log.Warn("deviation missing for config", "config", key)
			deviation = config.BuildDeviation(metav1.ObjectMeta{Name: cfg.GetName(), Namespace: cfg.GetNamespace()}, nil, nil)
		}

		if !cfg.IsRevertive() {
			labels := safeCopyLabels(deviation.GetLabels())
			labels["orphan"] = strconv.FormatBool(cfg.Orphan())
			deviation.SetLabels(labels)
			deviationsToDelete[key] = deviation
			deviationsToDeleteSet.Insert(key)
		}
	}

	log.Info("getConfigsAndDeviationsToTransact classification start",
	 	"configsToUpdate", configsToUpdateSet.UnsortedList(), 
		"configsToDelete", configsToDeleteSet.UnsortedList(), 
		"nonRecoverable", nonRecoverableSet.UnsortedList(),
		"deviationsToUpdate", deviationsToUpdateSet.UnsortedList(), 
		"deviationsToDelete", deviationsToDeleteSet.UnsortedList(), 
	)

	// If we have changes, retry non-recoverables
	if len(configsToUpdate) > 0 || len(configsToDelete) > 0 {
		for key, cfg := range nonRecoverable {
			if cfg.GetDeletionTimestamp() != nil {
				configsToDelete[key] = cfg
				configsToDeleteSet.Insert(key)
			} else {
				configsToUpdate[key] = cfg
				configsToUpdateSet.Insert(key)
			}
		}
	}

	log.Info("getConfigsAndDeviationsToTransact classification after change",
	 	"configsToUpdate", configsToUpdateSet.UnsortedList(), 
		"configsToDelete", configsToDeleteSet.UnsortedList(), 
		"nonRecoverable", nonRecoverableSet.UnsortedList(),
		"deviationsToUpdate", deviationsToUpdateSet.UnsortedList(), 
		"deviationsToDelete", deviationsToDeleteSet.UnsortedList(), 
	)
	
	return configsToUpdate, configsToDelete, deviationsToUpdate, deviationsToDelete
}

func (r *Transactor) handleTransactionErrors(
	ctx context.Context,
	rsp *sdcpb.TransactionSetResponse,
	configsToTransact, deletedConfigsToTransact map[string]*config.Config,
	globalErr error,
	recoverable bool,
) (bool, error) {
	log := log.FromContext(ctx)
	log.Info("handling transaction errors", "recoverable", recoverable)

	if rsp == nil {
		for _, configOrig := range configsToTransact {
			if err := r.processFailedConfig(ctx, configOrig, "", globalErr, recoverable); err != nil {
				return true, err
			}
		}
		for _, configOrig := range deletedConfigsToTransact {
			if err := r.processFailedConfig(ctx, configOrig, "", globalErr, false); err != nil {
				return true, err
			}
		}
		return recoverable, globalErr
	}

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

		if configOrig, ok := configsToTransact[intentName]; ok {
			if err := r.processFailedConfig(ctx, configOrig, msg, errs, false); err != nil {
				return true, err
			}
			continue
		} else if configOrig, ok := deletedConfigsToTransact[intentName]; ok {
			if err := r.processFailedConfig(ctx, configOrig, msg, errs, false); err != nil {
				return true, err
			}
			continue
		}
		dataServerError =true
		recoverable = false
		globalErr = errors.Join(
			errs, 
			fmt.Errorf("dataserver reported an error in an intent %s that does not exists", intentName),
		)
		
		break
	}
	if dataServerError {
		log.Error("transact dataserver error", "err", globalErr)
		for _, configOrig := range configsToTransact {
			if err := r.processFailedConfig(ctx, configOrig, "", globalErr, recoverable); err != nil {
				return true, err
			}
		}
		for _, configOrig := range deletedConfigsToTransact {
			if err := r.processFailedConfig(ctx, configOrig, "", globalErr, false); err != nil {
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

func collectWarnings(errors []string) []string {
	warnings := make([]string, 0, len(errors))
	for _, err := range errors {
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
