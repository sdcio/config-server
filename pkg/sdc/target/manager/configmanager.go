/*
Copyright 2026 Nokia.

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
	"strings"

	"github.com/henderiw/logger/log"
	condv1alpha1 "github.com/sdcio/config-server/apis/condition/v1alpha1"
	"github.com/sdcio/config-server/apis/config"
	configv1alpha1 "github.com/sdcio/config-server/apis/config/v1alpha1"
	configv1alpha1apply "github.com/sdcio/config-server/pkg/generated/applyconfiguration/config/v1alpha1"
	"github.com/sdcio/config-server/pkg/reconcilers/resource"
	sdcpb "github.com/sdcio/sdc-protos/sdcpb"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

const (
	finalizer = "config.config.sdcio.dev/finalizer"
)

// ConfigManager is responsible exclusively for Kubernetes Config resource management.
// It has no knowledge of gRPC, datastores, secrets, or encryption.
type ConfigManager struct {
	client       client.Client
	fieldManager string
}

func NewConfigManager(client client.Client, fieldManager string) *ConfigManager {
	return &ConfigManager{client: client, fieldManager: fieldManager}
}

// ── Config listing ─────────────────────────────────────────────────────────────

func (m *ConfigManager) ListConfigsPerTarget(ctx context.Context, target *configv1alpha1.Target) (*config.ConfigList, error) {
	v1alpha1configList := &configv1alpha1.ConfigList{}
	if err := m.client.List(ctx, v1alpha1configList,
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

// ── Transaction success / error ────────────────────────────────────────────────

// ProcessSuccess updates Config conditions after a successful transaction,
// applies finalizers, and cleans up deleted configs.
// Applied config data is now in TargetSnapshot — not written to Config status.
func (m *ConfigManager) ProcessSuccess(
	ctx context.Context,
	toUpdate []IntentInput,
	toDelete []IntentInput,
	targetCond condv1alpha1.Condition,
) error {
	for _, inp := range toUpdate {
		v1cfg, err := toV1Alpha1Config(inp.Config)
		if err != nil {
			return err
		}
		transacted := v1cfg.DeepCopy()
		if err := m.applyFinalizer(ctx, v1cfg); err != nil {
			return err
		}
		if err := m.updateConfigWithSuccess(ctx, transacted, targetCond); err != nil {
			return err
		}
	}
	for _, inp := range toDelete {
		v1cfg, err := toV1Alpha1Config(inp.Config)
		if err != nil {
			return err
		}
		if err := m.deleteFinalizer(ctx, v1cfg); err != nil {
			return err
		}
		if err := m.deleteDeviation(ctx, v1cfg); err != nil {
			return err
		}
	}
	return nil
}

// ProcessErrors updates Config conditions when a transaction fails.
// Returns (retry bool, error).
func (m *ConfigManager) ProcessErrors(
	ctx context.Context,
	rsp *sdcpb.TransactionSetResponse,
	toUpdate []IntentInput,
	toDelete []IntentInput,
	globalErr error,
	recoverable bool,
) (bool, error) {
	log := log.FromContext(ctx)
	log.Warn("handling transaction errors", "recoverable", recoverable)

	updateByKey := make(map[string]IntentInput, len(toUpdate))
	for _, inp := range toUpdate {
		updateByKey[config.GetGVKNSN(inp.Config)] = inp
	}
	deleteByKey := make(map[string]IntentInput, len(toDelete))
	for _, inp := range toDelete {
		deleteByKey[config.GetGVKNSN(inp.Config)] = inp
	}

	if rsp == nil {
		for _, inp := range toUpdate {
			if err := m.processFailedInput(ctx, inp, "", globalErr, recoverable); err != nil {
				return true, err
			}
		}
		for _, inp := range toDelete {
			if err := m.processFailedInput(ctx, inp, "", globalErr, false); err != nil {
				return true, err
			}
		}
		return recoverable, globalErr
	}

	dataServerError := false
	for intentName, intent := range rsp.Intents {
		log.Warn("intent failed", "name", intentName, "errors", intent.Errors)

		var errs error = globalErr
		for _, e := range intent.Errors {
			errs = errors.Join(errs, fmt.Errorf("%s", e))
		}
		msg := strings.Join(collectWarnings(intent.Errors), "; ")

		if inp, ok := updateByKey[intentName]; ok {
			if err := m.processFailedInput(ctx, inp, msg, errs, false); err != nil {
				return true, err
			}
			continue
		}
		if inp, ok := deleteByKey[intentName]; ok {
			if err := m.processFailedInput(ctx, inp, msg, errs, false); err != nil {
				return true, err
			}
			continue
		}

		dataServerError = true
		recoverable = false
		globalErr = errors.Join(errs, fmt.Errorf("dataserver reported unknown intent %s", intentName))
		break
	}

	if dataServerError {
		log.Error("transact dataserver error", "err", globalErr)
		for _, inp := range toUpdate {
			if err := m.processFailedInput(ctx, inp, "", globalErr, recoverable); err != nil {
				return true, err
			}
		}
		for _, inp := range toDelete {
			if err := m.processFailedInput(ctx, inp, "", globalErr, false); err != nil {
				return true, err
			}
		}
	}

	return recoverable, globalErr
}

// ── Target condition broadcast ─────────────────────────────────────────────────

func (m *ConfigManager) SetConfigsTargetConditionForTarget(
	ctx context.Context,
	target *configv1alpha1.Target,
	targetCond condv1alpha1.Condition,
) error {
	cfgList, err := m.ListConfigsPerTarget(ctx, target)
	if err != nil {
		return err
	}
	for i := range cfgList.Items {
		v1cfg, err := toV1Alpha1Config(&cfgList.Items[i])
		if err != nil {
			return err
		}

		oldTargetCond := v1cfg.GetCondition(condv1alpha1.ConditionType(targetCond.Type))
		oldReadyCond := v1cfg.GetCondition(condv1alpha1.ConditionTypeReady)

		v1cfg.SetConditions(targetCond)
		v1cfg.SetOverallStatus()
		newReadyCond := v1cfg.GetCondition(condv1alpha1.ConditionTypeReady)

		if targetCond.Equal(oldTargetCond) && newReadyCond.Equal(oldReadyCond) {
			continue
		}

		configCond := v1cfg.GetCondition(condv1alpha1.ConditionType(configv1alpha1.ConfigReady("").Type))

		statusApply := configv1alpha1apply.ConfigStatus().
    		WithConditions(condv1alpha1.DedupeConditions(targetCond, configCond, newReadyCond)...)

		applyConfig := configv1alpha1apply.Config(v1cfg.Name, v1cfg.Namespace).WithStatus(statusApply)
		if err := m.client.Status().Apply(ctx, applyConfig, &client.SubResourceApplyOptions{
			ApplyOptions: client.ApplyOptions{FieldManager: m.fieldManager},
		}); err != nil {
			return err
		}
	}
	return nil
}

// ── Deviation management ───────────────────────────────────────────────────────

func (m *ConfigManager) ApplyDeviation(ctx context.Context, cfg *config.Config) (configv1alpha1.Deviation, error) {
	key := types.NamespacedName{Name: cfg.Name, Namespace: cfg.Namespace}

	deviation := &configv1alpha1.Deviation{}
	if err := m.client.Get(ctx, key, deviation); err != nil {
		if resource.IgnoreNotFound(err) != nil {
			return configv1alpha1.Deviation{}, err
		}
		newDeviation := configv1alpha1.BuildDeviation(metav1.ObjectMeta{
			Name:            cfg.Name,
			Namespace:       cfg.Namespace,
			OwnerReferences: []metav1.OwnerReference{cfg.GetOwnerReference()},
			Labels:          cfg.Labels,
		}, &configv1alpha1.DeviationSpec{
			DeviationType: ptr.To(configv1alpha1.DeviationType_CONFIG),
		}, nil)
		if err := m.client.Create(ctx, newDeviation); err != nil {
			return configv1alpha1.Deviation{}, err
		}
		return *newDeviation, nil
	}
	return *deviation, nil
}

// ── Private helpers ────────────────────────────────────────────────────────────

func (m *ConfigManager) processFailedInput(
	ctx context.Context,
	inp IntentInput,
	msg string,
	origErr error,
	recoverable bool,
) error {
	v1cfg, err := toV1Alpha1Config(inp.Config)
	if err != nil {
		return err
	}
	if err := m.applyFinalizer(ctx, v1cfg); err != nil {
		return err
	}
	return m.updateConfigWithError(ctx, v1cfg, msg, origErr, recoverable)
}

// updateConfigWithSuccess sets the ConfigReady and overall conditions.
// Applied config data lives in TargetSnapshot — not written here.
func (m *ConfigManager) updateConfigWithSuccess(
	ctx context.Context,
	cfg *configv1alpha1.Config,
	targetCond condv1alpha1.Condition,
) error {
	log := log.FromContext(ctx)
	log.Debug("updateConfigWithSuccess", "config", cfg.GetName())

	current := &configv1alpha1.Config{}
	if err := m.client.Get(ctx, types.NamespacedName{Name: cfg.Name, Namespace: cfg.Namespace}, current); err != nil {
		return err
	}
	if current.GetDeletionTimestamp() != nil {
		log.Debug("skip success update: config is deleting")
		return nil
	}

	newConfigCond := configv1alpha1.ConfigReady("")

	tmp := current.DeepCopy()
	tmp.SetConditions(newConfigCond, targetCond)
	tmp.SetOverallStatus()
	newOverallCond := tmp.GetCondition(condv1alpha1.ConditionTypeReady)

	curConfigCond := current.GetCondition(condv1alpha1.ConditionType(newConfigCond.Type))
	curTargetCond := current.GetCondition(condv1alpha1.ConditionType(targetCond.Type))
	curOverallCond := current.GetCondition(condv1alpha1.ConditionTypeReady)

	// No-op: conditions already reflect what we want.
	if newConfigCond.Equal(curConfigCond) &&
		targetCond.Equal(curTargetCond) &&
		newOverallCond.Equal(curOverallCond) {
		return nil
	}

	statusApply := configv1alpha1apply.ConfigStatus().
    	WithConditions(condv1alpha1.DedupeConditions(newConfigCond, targetCond, newOverallCond)...)

	applyConfig := configv1alpha1apply.Config(cfg.Name, cfg.Namespace).WithStatus(statusApply)
	return m.client.Status().Apply(ctx, applyConfig, &client.SubResourceApplyOptions{
		ApplyOptions: client.ApplyOptions{FieldManager: m.fieldManager},
	})
}

func (m *ConfigManager) updateConfigWithError(
	ctx context.Context,
	cfg *configv1alpha1.Config,
	msg string,
	origErr error,
	recoverable bool,
) error {
	log := log.FromContext(ctx)
	log.Warn("updateConfigWithError", "config", cfg.GetName(), "recoverable", recoverable, "msg", msg)

	if origErr != nil {
		msg = fmt.Sprintf("%s err %s", msg, origErr.Error())
	}

	current := &configv1alpha1.Config{}
	if err := m.client.Get(ctx, types.NamespacedName{Name: cfg.Name, Namespace: cfg.Namespace}, current); err != nil {
		return err
	}
	if current.GetDeletionTimestamp() != nil {
		log.Debug("skip error update: config is deleting")
		return nil
	}

	var newConfigCond condv1alpha1.Condition
	if recoverable {
		newConfigCond = configv1alpha1.ConfigFailed(msg)
	} else {
		newMessage := condv1alpha1.UnrecoverableMessage{
			ResourceVersion: current.GetResourceVersion(),
			Message:         msg,
		}
		newmsg, err := json.Marshal(newMessage)
		if err != nil {
			return err
		}
		newConfigCond = condv1alpha1.FailedUnRecoverable(string(newmsg))
	}

	tmp := current.DeepCopy()
	tmp.SetConditions(newConfigCond)
	tmp.SetOverallStatus()
	newOverallCond := tmp.GetCondition(condv1alpha1.ConditionTypeReady)

	curConfigCond := current.GetCondition(condv1alpha1.ConditionType(newConfigCond.Type))
	curOverallCond := current.GetCondition(condv1alpha1.ConditionTypeReady)

	if newConfigCond.Equal(curConfigCond) && newOverallCond.Equal(curOverallCond) {
		return nil
	}

	statusApply := configv1alpha1apply.ConfigStatus().
    	WithConditions(condv1alpha1.DedupeConditions(newConfigCond, newOverallCond)...)
	applyConfig := configv1alpha1apply.Config(current.Name, current.Namespace).WithStatus(statusApply)
	return m.client.Status().Apply(ctx, applyConfig, &client.SubResourceApplyOptions{
		ApplyOptions: client.ApplyOptions{FieldManager: m.fieldManager},
	})
}

func (m *ConfigManager) applyFinalizer(ctx context.Context, cfg *configv1alpha1.Config) error {
	return m.patchMetadata(ctx, cfg, func() { cfg.SetFinalizers([]string{finalizer}) })
}

func (m *ConfigManager) deleteFinalizer(ctx context.Context, cfg *configv1alpha1.Config) error {
	return m.patchMetadata(ctx, cfg, func() { cfg.SetFinalizers([]string{}) })
}

func (m *ConfigManager) deleteDeviation(ctx context.Context, cfg *configv1alpha1.Config) error {
	deviation := &configv1alpha1.Deviation{
		ObjectMeta: metav1.ObjectMeta{Name: cfg.Name, Namespace: cfg.Namespace},
	}
	if err := m.client.Delete(ctx, deviation); err != nil {
		return resource.IgnoreNotFound(err)
	}
	return nil
}

func (m *ConfigManager) patchMetadata(ctx context.Context, obj client.Object, mutate func()) error {
	orig := obj.DeepCopyObject().(client.Object)
	mutate()
	return m.client.Patch(ctx, obj, client.MergeFrom(orig),
		&client.PatchOptions{FieldManager: m.fieldManager},
	)
}
