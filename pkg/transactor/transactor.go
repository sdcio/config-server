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

	"github.com/henderiw/apiserver-store/pkg/storebackend"
	"github.com/henderiw/logger/log"
	condv1alpha1 "github.com/sdcio/config-server/apis/condition/v1alpha1"
	"github.com/sdcio/config-server/apis/config"
	configv1alpha1 "github.com/sdcio/config-server/apis/config/v1alpha1"
	invv1alpha1 "github.com/sdcio/config-server/apis/inv/v1alpha1"
	"github.com/sdcio/config-server/pkg/reconcilers/resource"
	"github.com/sdcio/config-server/pkg/target"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/encoding/prototext"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
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

func New(client client.Client, fiedlManager, fieldManagerFinalizer string) *Transactor {
	return &Transactor{
		client:                client,
		fieldManager:          fiedlManager,
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
		if config.Status.AppliedConfig != nil {
			configs = append(configs, &config)
		}
		if !config.IsRevertive() {
			deviation, ok := deviationMap[GetGVKNSN(&config)]
			if !ok {
				continue
			}
			// dont include deviations if there are none
			if len(deviation.Spec.Deviations) != 0 {
				labels := deviation.GetLabels()
				if labels != nil {
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
		log.Info("config recovery done -> no configs to recover")
		return nil, nil
	}
	log.Info("recovering target config ....")
	targetKey := storebackend.KeyFromNSN(target.GetNamespacedName())
	msg, err := tctx.RecoverIntents(ctx, targetKey, configs, deviations)
	if err != nil {
		// This is bad since this means we cannot recover the applied config
		// on a target. We set the target config status to Failed.
		// Most likely a human intervention is needed
		return &msg, err
	}
	tctx.SetRecoveredConfigsState(ctx)
	log.Info("config recovery done -> configs recovered")
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
	configsToTransact, deletedConfigsToTransact, deviationsToTransact := getConfigsAndDeviationsToTransact(ctx, configList, deviationMap)

	log.Info("Transact", 
		"configsToTransact", len(configsToTransact), 
		"deletedConfigsToTransact", len(deletedConfigsToTransact),
		"deviationsToTransact", len(deviationsToTransact),
	)
	if len(configsToTransact) == 0 && len(deletedConfigsToTransact) == 0 && len(deviationsToTransact) == 0 {
		return false, nil
	}

	targetKey := storebackend.KeyFromNSN(target.GetNamespacedName())

	rsp, err := tctx.SetIntents(ctx, targetKey, "dummyTransactionID", configsToTransact, deletedConfigsToTransact, deviationsToTransact, false)
	// we first collect the warnings and errors -> to determine error or not
	var global_error error
	var intent_errors error
	var global_warnings []string
	var recoverable bool
	if err != nil {
		global_error = errors.Join(global_error, fmt.Errorf("error: %s", err.Error()))
		recoverable = false
		if er, ok := status.FromError(err); ok {
			switch er.Code() {
			// Aborted is the refering to a lock in the dataserver
			case codes.Aborted, codes.ResourceExhausted:
				recoverable = true
			default:
				recoverable = false
			}
		}
		if rsp != nil {
			// determine errors from the response
			for _, intent := range rsp.Intents {
				for _, intentError := range intent.Errors {
					recoverable = false
					intent_errors = errors.Join(intent_errors, fmt.Errorf("%s", intentError))
				}
			}
		}
	}
	if rsp != nil {
		// global wornings
		for _, warning := range rsp.Warnings {
			global_warnings = append(global_warnings, fmt.Sprintf("global warning: %q", warning))
		}
		log.Warn("transaction", "global warnings", global_warnings)
	}
	if global_error != nil || intent_errors != nil {
		log.Info("transaction failed", "recoverable", recoverable, "error", global_error)
		if rsp == nil {
			for _, configOrig := range configsToTransact {
				config := &configv1alpha1.Config{}
				if err := configv1alpha1.Convert_config_Config_To_v1alpha1_Config(configOrig, config, nil); err != nil {
					return false, err
				}
				if err := r.applyFinalizer(ctx, config); err != nil {
					return true, err
				}
				if err := r.updateConfigWithError(ctx, config, "", global_error, recoverable); err != nil {
					return true, err
				}
			}

			for _, configOrig := range deletedConfigsToTransact {
				config := &configv1alpha1.Config{}
				if err := configv1alpha1.Convert_config_Config_To_v1alpha1_Config(configOrig, config, nil); err != nil {
					return false, err
				}
				if err := r.applyFinalizer(ctx, config); err != nil {
					return true, err
				}
				if err := r.updateConfigWithError(ctx, config, "", global_error, false); err != nil {
					return true, err
				}
				
			}
			return recoverable, global_error
		} 
		for intentName, intent := range rsp.Intents {
			var errs error
			errs = errors.Join(global_error)
			for _, intentError := range intent.Errors {
				errs = errors.Join(fmt.Errorf("%s", intentError))
			}
			collectedWarnings := []string{}
			for _, intentWarning := range intent.Errors {
				collectedWarnings = append(collectedWarnings, fmt.Sprintf("warning: %q", intentWarning))
			}
			msg := ""
			if len(collectedWarnings) > 0 {
				msg = strings.Join(collectedWarnings, "; ")
			}

			configOrig, ok := configsToTransact[intentName]
			if ok {
				config := &configv1alpha1.Config{}
				if err := configv1alpha1.Convert_config_Config_To_v1alpha1_Config(configOrig, config, nil); err != nil {
					return false, err
				}
				if err := r.applyFinalizer(ctx, config); err != nil {
					return true, err
				}
				if err := r.updateConfigWithError(ctx, config, msg, errs, false); err != nil {
					return true, err
				}
				continue
			}

			configOrig, ok = deletedConfigsToTransact[intentName]
			if ok {
				config := &configv1alpha1.Config{}
				if err := configv1alpha1.Convert_config_Config_To_v1alpha1_Config(configOrig, config, nil); err != nil {
					return false, err
				}
				if err := r.applyFinalizer(ctx, config); err != nil {
					return true, err
				}
				if err := r.updateConfigWithError(ctx, config, msg, err, false); err != nil {
					return true, err
				}
				continue
			}
		}
		return recoverable, global_error
	}	
	log.Info("transaction response", "rsp", prototext.Format(rsp))
	// ok case
	if err := tctx.TransactionConfirm(ctx, targetKey.String(), "dummyTransactionID"); err != nil {

	}
	for configKey, configOrig := range configsToTransact {
		config := &configv1alpha1.Config{}
		if err := configv1alpha1.Convert_config_Config_To_v1alpha1_Config(configOrig, config, nil); err != nil {
			return false, err
		}
		if err := r.applyFinalizer(ctx, config); err != nil {
			return true, err
		}
		if err := r.updateConfigWithSuccess(ctx, config, (*configv1alpha1.ConfigStatusLastKnownGoodSchema)(tctx.GetSchema()), ""); err != nil {
			return true, err
		}
		deviation, ok := deviationMap[configKey]
		if ok && config.IsRevertive() {
			if err := r.clearDeviation(ctx, deviation); err != nil {
				return true, err
			}
		}
	}

	for configKey, configOrig := range deletedConfigsToTransact {
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
	log.Info("updateConfigWithError", "msg", msg, "error", err)

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
	
	configOrig := config.DeepCopy()
	patch := client.MergeFrom(configOrig)

	config.SetFinalizers([]string{finalizer})
	return r.client.Patch(ctx, config, patch, &client.SubResourcePatchOptions{
		PatchOptions: client.PatchOptions{
			FieldManager: r.fieldManagerFinalizer,
		},
	})
}

func (r *Transactor) deleteFinalizer(ctx context.Context, config *configv1alpha1.Config) error {
	log := log.FromContext(ctx)
	log.Info("deleteFinalizer")
	
	configOrig := config.DeepCopy()
	patch := client.MergeFrom(configOrig)

	config.SetFinalizers([]string{})
	return r.client.Patch(ctx, config, patch, &client.SubResourcePatchOptions{
		PatchOptions: client.PatchOptions{
			FieldManager: r.fieldManagerFinalizer,
		},
	})
}

func (r *Transactor) updateConfigWithSuccess(
	ctx context.Context,
	config *configv1alpha1.Config,
	schema *configv1alpha1.ConfigStatusLastKnownGoodSchema,
	msg string,
) error {
	log := log.FromContext(ctx)
	log.Info("updateConfigWithSuccess")

	configOrig := config.DeepCopy()
	patch := client.MergeFrom(configOrig)

	config.SetConditions(condv1alpha1.ReadyWithMsg(msg))
	config.Status.LastKnownGoodSchema = schema
	config.Status.AppliedConfig = &config.Spec
	if config.IsRevertive() {
		config.Status.DeviationGeneration = nil
	} else {
		config.Status.DeviationGeneration = ptr.To(config.GetGeneration())
	}

	return r.client.Status().Patch(ctx, config, patch, &client.SubResourcePatchOptions{
		PatchOptions: client.PatchOptions{
			FieldManager: r.fieldManager,
		},
	})
}

func getConfigsAndDeviationsToTransact(
	ctx context.Context,
	configList *config.ConfigList,
	deviationMap map[string]*config.Deviation,
) (map[string]*config.Config, map[string]*config.Config, map[string]*config.Deviation) {
	nonrecoverableConfigs := map[string]*config.Config{}
	changedConfigs := map[string]*config.Config{}
	toBeDeletedConfigs := map[string]*config.Config{}
	unchangedConfigs := map[string]*config.Config{}
	log := log.FromContext(ctx)

	// classify configs
	for _, config := range configList.Items {
		// this should cover the last transaction failed
		if !config.IsRecoverable() {
			nonrecoverableConfigs[GetGVKNSN(&config)] = &config
			continue
		}
		// we first check the deletion tiemstamp if it set it should be deleted
		if config.GetDeletionTimestamp() != nil {
			toBeDeletedConfigs[GetGVKNSN(&config)] = &config
			continue
		}

		// determine if the spec changed
		if config.Status.AppliedConfig != nil &&
			config.Spec.GetShaSum(ctx) == config.Status.AppliedConfig.GetShaSum(ctx) {
			
			unchangedConfigs[GetGVKNSN(&config)] = &config
			continue
		}
		changedConfigs[GetGVKNSN(&config)] = &config
	}


	// we determine if there is a config change -> if so we incorporate the previously failed
	// configs to retry
	if len(changedConfigs) != 0 || len(configv1alpha1.DeletionDelete) != 0 {
		for _, config := range nonrecoverableConfigs {
			if config.DeletionTimestamp != nil {
				toBeDeletedConfigs[GetGVKNSN(config)] = config
			} else {
				changedConfigs[GetGVKNSN(config)] = config
			}
		}
	}
	
	// determine which configs to include based on deviation config
	deviationsToTransact := map[string]*config.Deviation{}
	for _, config := range configList.Items {
		if config.IsRevertive() {
			log.Info("check deviation configs", "name", GetGVKNSN(&config), "revertive", true)
			deviation, ok := deviationMap[GetGVKNSN(&config)]
			if !ok {
				// strange, deviations should always be present -> during init this can happen
				// when deviations are not yet created
				continue
			}
			log.Info("check deviation configs", "name", GetGVKNSN(&config), "revertive", true, "deviation spec", deviation.Spec)
			if deviation.HasNotAppliedDeviation() {
				changedConfigs[GetGVKNSN(&config)] = &config
			}
		} else {
			// Non revertive
			// The deviations for the configs that changed will be included
			// Configs in deletion will not include the deviations
			if _, ok := changedConfigs[GetGVKNSN(&config)]; ok {
				// lookup deviation, if exists include it
				deviation, ok := deviationMap[GetGVKNSN(&config)]
				if !ok {
					// strange, deviations should always be present -> during init this can happen
					// when deviations are not yet created
					continue
				}
				if len(deviation.Spec.Deviations) != 0 {
					labels := deviation.GetLabels()
					if labels != nil {
						labels = map[string]string{}
					}
					labels["priority"] = strconv.Itoa(int(config.Spec.Priority))
					deviation.SetLabels(labels)
					deviationsToTransact[GetGVKNSN(deviation)] = deviation
				}
			}
		}
	}

	return changedConfigs, toBeDeletedConfigs, deviationsToTransact
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
	configv1alpha1.Convert_v1alpha1_ConfigList_To_config_ConfigList(v1alpha1configList, configList, nil)

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
	configv1alpha1.Convert_v1alpha1_DeviationList_To_config_DeviationList(v1alpha1deviationList, deviationList, nil)

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
	v1alpha1deviation := &configv1alpha1.Deviation{}
	if err := configv1alpha1.Convert_config_Deviation_To_v1alpha1_Deviation(deviation, v1alpha1deviation, nil); err != nil {
		return err
	}

	log := log.FromContext(ctx)
	v1alpha1deviationOrig := v1alpha1deviation.DeepCopy()
	patch := client.MergeFrom(v1alpha1deviationOrig)

	v1alpha1deviation.Spec.Deviations = []configv1alpha1.ConfigDeviation{}

	if err := r.client.Patch(ctx, v1alpha1deviation, patch, &client.SubResourcePatchOptions{
		PatchOptions: client.PatchOptions{
			FieldManager: r.fieldManager,
		},
	}); err != nil {
		log.Error("cannot clear deviation", "deviation", deviation.GetNamespacedName())
	}
	return nil
}
