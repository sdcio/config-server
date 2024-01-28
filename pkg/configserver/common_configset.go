// Copyright 2023 The xxx Authors.
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//    http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package configserver

import (
	"context"
	"fmt"
	"sort"
	"strings"

	"github.com/henderiw/logger/log"
	configv1alpha1 "github.com/iptecharch/config-server/apis/config/v1alpha1"
	invv1alpha1 "github.com/iptecharch/config-server/apis/inv/v1alpha1"
	"github.com/iptecharch/config-server/pkg/store"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/uuid"
	"k8s.io/apimachinery/pkg/watch"
	"k8s.io/apiserver/pkg/registry/rest"
	"k8s.io/utils/pointer"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

func (r *configCommon) createConfigSet(ctx context.Context,
	runtimeObject runtime.Object,
	createValidation rest.ValidateObjectFunc,
	options *metav1.CreateOptions) (runtime.Object, error) {

	// logger
	log := log.FromContext(ctx)
	// setting a uid for the element
	accessor, err := meta.Accessor(runtimeObject)
	if err != nil {
		return nil, apierrors.NewBadRequest(err.Error())
	}
	accessor.SetUID(uuid.NewUUID())
	accessor.SetCreationTimestamp(metav1.Now())
	accessor.SetResourceVersion(generateRandomString(6))

	key, err := r.getKey(ctx, accessor.GetName())
	if err != nil {
		return nil, apierrors.NewBadRequest(err.Error())
	}
	log.Info("create configset", "key", key.String())

	// get the data of the runtime object
	newConfigSet, ok := runtimeObject.(*configv1alpha1.ConfigSet)
	if !ok {
		return nil, apierrors.NewBadRequest(fmt.Sprintf("expected Config object, got %T", runtimeObject))
	}

	newConfigSet, err = r.upsertConfigSet(ctx, newConfigSet)
	if err != nil {
		return newConfigSet, err
	}
	// update the store
	if err := r.storeCreateConfigSet(ctx, key, newConfigSet); err != nil {
		return nil, apierrors.NewInternalError(err)
	}

	return newConfigSet, nil
}

func (r *configCommon) updateConfigSet(
	ctx context.Context,
	name string,
	objInfo rest.UpdatedObjectInfo,
	createValidation rest.ValidateObjectFunc,
	updateValidation rest.ValidateObjectUpdateFunc,
	forceAllowCreate bool,
	options *metav1.UpdateOptions,
) (runtime.Object, bool, error) {
	// logger
	log := log.FromContext(ctx)

	// Get Key
	key, err := r.getKey(ctx, name)
	if err != nil {
		return nil, false, apierrors.NewBadRequest(err.Error())
	}
	log.Info("update", "key", key.String())

	// isCreate tracks whether this is an update that creates an object (this happens in server-side apply)
	isCreate := false

	oldObj, err := r.configSetStore.Get(ctx, key)
	if err != nil {
		log.Info("update", "err", err.Error())
		if forceAllowCreate && strings.Contains(err.Error(), "not found") {
			// For server-side apply, we can create the object here
			isCreate = true
		} else {
			return nil, false, err
		}
	}
	// get the data of the runtime object
	oldConfigSet, ok := oldObj.(*configv1alpha1.ConfigSet)
	if !ok {
		return nil, false, apierrors.NewBadRequest(fmt.Sprintf("expected old Config object, got %T", oldConfigSet))
	}

	newObj, err := objInfo.UpdatedObject(ctx, oldObj)
	if err != nil {
		log.Info("update failed to construct UpdatedObject", "error", err.Error())
		return nil, false, err
	}

	// get the data of the runtime object
	newConfigSet, ok := newObj.(*configv1alpha1.ConfigSet)
	if !ok {
		return nil, false, apierrors.NewBadRequest(fmt.Sprintf("expected Config object, got %T", newObj))
	}
	if oldConfigSet.GetResourceVersion() != newConfigSet.GetResourceVersion() {
		return nil, false, apierrors.NewConflict(configv1alpha1.Resource("configs"), oldConfigSet.GetName(), fmt.Errorf(OptimisticLockErrorMsg))
	}
	if oldConfigSet.DeletionTimestamp != nil && len(newConfigSet.Finalizers) == 0 {
		if err := r.configSetStore.Delete(ctx, key); err != nil {
			return nil, false, apierrors.NewInternalError(err)
		}
		// deleted
		return newConfigSet, false, nil
	}

	accessor, err := meta.Accessor(newObj)
	if err != nil {
		return nil, false, apierrors.NewBadRequest(err.Error())
	}
	accessor.SetResourceVersion(generateRandomString(6))

	newConfigSet, err = r.upsertConfigSet(ctx, newConfigSet)
	if err != nil {
		return newConfigSet, false, err
	}
	// update the store
	if isCreate {
		if err := r.storeCreateConfigSet(ctx, key, newConfigSet); err != nil {
			return nil, false, apierrors.NewInternalError(err)
		}
	} else {
		if err := r.storeUpdateConfigSet(ctx, key, newConfigSet); err != nil {
			return nil, false, apierrors.NewInternalError(err)
		}
	}

	return newConfigSet, isCreate, nil
}

func (r *configCommon) deleteConfigSet(
	ctx context.Context,
	name string,
	deleteValidation rest.ValidateObjectFunc,
	options *metav1.DeleteOptions,
) (runtime.Object, bool, error) {
	// logger
	log := log.FromContext(ctx)

	// Get Key
	key, err := r.getKey(ctx, name)
	if err != nil {
		return nil, false, apierrors.NewBadRequest(err.Error())
	}
	log.Info("delete", "key", key.String())

	obj, err := r.configSetStore.Get(ctx, key)
	if err != nil {
		return nil, false, apierrors.NewNotFound(r.gr, name)
	}

	accessor, err := meta.Accessor(obj)
	if err != nil {
		return nil, false, apierrors.NewBadRequest(err.Error())
	}
	now := metav1.Now()
	accessor.SetDeletionTimestamp(&now)

	// get the data of the runtime object
	newConfigSet, ok := obj.(*configv1alpha1.ConfigSet)
	if !ok {
		return nil, false, apierrors.NewBadRequest(fmt.Sprintf("expected Config object, got %T", obj))
	}

	if deleteValidation != nil {
		err := deleteValidation(ctx, newConfigSet)
		if err != nil {
			log.Info("delete validation failed", "error", err)
			return nil, false, err
		}
	}
	if len(newConfigSet.Finalizers) > 0 {
		if err := r.configSetStore.Update(ctx, key, newConfigSet); err != nil {
			return nil, false, apierrors.NewInternalError(err)
		}
		return newConfigSet, false, nil
	}

	existingChildConfigs := r.getOrphanConfigsFromConfigSet(ctx, newConfigSet)
	log.Info("delete existingConfigs", "total", len(existingChildConfigs))

	for nsn, existingChildConfig := range existingChildConfigs {
		log.Info("delete existingChildConfig", "nsn", nsn)
		if _, _, err := r.deleteConfig(ctx, nsn.Name, nil, &metav1.DeleteOptions{
			TypeMeta: existingChildConfig.TypeMeta,
			//GracePeriodSeconds: pointer.Int64(0), // force delete
		}); err != nil {
			log.Error("delete existing childConfig failed", "error", err)
		}
	}

	if err := r.storeDeleteConfigSet(ctx, key, newConfigSet); err != nil {
		return nil, false, apierrors.NewInternalError(err)
	}

	log.Info("delete intent from store succeeded")
	return newConfigSet, true, nil
}

func (r *configCommon) upsertConfigSet(ctx context.Context, configSet *configv1alpha1.ConfigSet) (*configv1alpha1.ConfigSet, error) {
	targets, err := r.unrollDownstreamTargets(ctx, configSet)
	if err != nil {
		// Strategy we reject the resource create/update and dont store the update
		return configSet, err
	}

	return r.ensureConfigs(ctx, configSet, targets)
}

// unrollDownstreamTargets list the targets
func (r *configCommon) unrollDownstreamTargets(
	ctx context.Context,
	configSet *configv1alpha1.ConfigSet) ([]types.NamespacedName, error) {

	selector, err := metav1.LabelSelectorAsSelector(configSet.Spec.Target.TargetSelector)
	if err != nil {
		return nil, apierrors.NewBadRequest(fmt.Sprintf("parsing selector failed: err: %s", err.Error()))
	}
	opts := []client.ListOption{
		client.InNamespace(configSet.Namespace),
		client.MatchingLabelsSelector{Selector: selector},
	}

	targetList := &invv1alpha1.TargetList{}
	if err := r.client.List(ctx, targetList, opts...); err != nil {
		return nil, apierrors.NewInternalError(err)
	}
	targets := make([]types.NamespacedName, 0, len(targetList.Items))
	for _, target := range targetList.Items {
		// only add targets that are not in deleting state
		if target.GetDeletionTimestamp().IsZero() {
			targets = append(targets, types.NamespacedName{Name: target.Name, Namespace: configSet.Namespace})
		}
	}
	sort.Slice(targets, func(i, j int) bool {
		return targets[i].Name < targets[j].Name
	})
	return targets, nil
}

func (r *configCommon) ensureConfigs(ctx context.Context, configSet *configv1alpha1.ConfigSet, targets []types.NamespacedName) (*configv1alpha1.ConfigSet, error) {
	log := log.FromContext(ctx)

	// get the exisiting configs to see if the config is present; if the configset's target is no
	// longer applicable we will delete the config for this particular target
	existingConfigs := r.getOrphanConfigsFromConfigSet(ctx, configSet)

	// TODO run in parallel and/or try 1 first to see if the validation works or not
	TargetsStatus := make([]configv1alpha1.TargetStatus, len(targets))
	configSet.SetConditions(configv1alpha1.Ready())
	for i, target := range targets {
		var oldConfig *configv1alpha1.Config
		newConfig := buildConfig(ctx, configSet, target)

		// delete the config from the existingConfigs map as it is updated
		nsnKey := types.NamespacedName{Namespace: newConfig.Namespace, Name: newConfig.Name}
		isCreate := false
		changed := true
		oldConfig, ok := existingConfigs[nsnKey]
		if !ok { // config does not exist -> create it
			log.Info("config does not exist", "nsn", nsnKey.String())
			isCreate = true
			newConfig.UID = uuid.NewUUID()
			newConfig.CreationTimestamp = metav1.Now()
			oldConfig = newConfig // ensure the upsert call works
		} else {
			log.Info("config exists", "nsn", nsnKey.String())
			// TODO better logic to validate changes
			newConfig = oldConfig.DeepCopy()
			newSpec := configv1alpha1.ConfigSpec{
				Lifecycle: configSet.Spec.Lifecycle,
				Priority:  configSet.Spec.Priority,
				Config:    configSet.Spec.Config,
			}
			// check if the spec changed
			currentShaSum := configv1alpha1.GetShaSum(ctx, &oldConfig.Spec)
			newShaSum := configv1alpha1.GetShaSum(ctx, &newSpec)
			if currentShaSum != newShaSum {
				log.Info("ensureConfigs spec changed")
				newConfig.ResourceVersion = generateRandomString(6)
				newConfig.Spec = newSpec
			} else {
				log.Info("ensureConfigs spec did not change")
				changed = false
			}
			if len(newConfig.GetLabels()) == 0 {
				newConfig.Labels = make(map[string]string, len(configSet.GetLabels()))
			}
			for k, v := range configSet.GetLabels() {
				newConfig.Labels[k] = v
			}
			if len(newConfig.GetAnnotations()) == 0 {
				newConfig.Annotations = make(map[string]string, len(configSet.GetAnnotations()))
			}
			for k, v := range configSet.GetAnnotations() {
				newConfig.Annotations[k] = v
			}
		}
		// delete the config from the existing configs -> this list is emptied such that the remaining entries
		// can be deleted
		delete(existingConfigs, nsnKey)
		if changed || oldConfig.GetCondition(configv1alpha1.ConditionTypeReady).Status == metav1.ConditionFalse {
			// this is now an async
			if _, _, err := r.upsertTargetConfig(
				ctx,
				store.KeyFromNSN(types.NamespacedName{Namespace: newConfig.Namespace, Name: newConfig.Name}),
				store.KeyFromNSN(target),
				oldConfig,
				newConfig,
				isCreate,
			); err != nil {
				TargetsStatus[i] = configv1alpha1.TargetStatus{
					Name:      target.Name,
					Condition: configv1alpha1.Failed(err.Error()),
				}
				configSet.SetConditions(configv1alpha1.Failed("config not applied to all targets"))
			} else {
				TargetsStatus[i] = configv1alpha1.TargetStatus{
					Name:      target.Name,
					Condition: configv1alpha1.Ready(),
				}
			}
		}
	}

	// These configs no longer match a target
	// TODO: what to do with the delete error
	for nsn, existingConfig := range existingConfigs {
		if _, _, err := r.deleteConfig(ctx, nsn.Name, nil, &metav1.DeleteOptions{
			TypeMeta:           existingConfig.TypeMeta,
			GracePeriodSeconds: pointer.Int64(0), // force delete
		}); err != nil {
			log.Error("delete existing intent failed", "error", err)
		}
	}
	configSet.Status.Targets = TargetsStatus

	return configSet, nil
}

// getOrphanConfigsFromConfigSet returns the children owned by this configSet
func (r *configCommon) getOrphanConfigsFromConfigSet(ctx context.Context, configSet *configv1alpha1.ConfigSet) map[types.NamespacedName]*configv1alpha1.Config {
	log := log.FromContext(ctx)
	existingConfigs := map[types.NamespacedName]*configv1alpha1.Config{}
	// get existing configs
	r.configStore.List(ctx, func(ctx context.Context, key store.Key, obj runtime.Object) {
		config, ok := obj.(*configv1alpha1.Config)
		if !ok {
			log.Error("unexpected object in store")
			return
		}
		for _, ref := range config.OwnerReferences {
			if ref.APIVersion == configSet.APIVersion &&
				ref.Kind == configSet.Kind &&
				ref.Name == configSet.Name &&
				ref.UID == configSet.UID {
				existingConfigs[types.NamespacedName{Namespace: config.Namespace, Name: config.Name}] = config.DeepCopy()
			}
		}
	})
	for nsn, config := range existingConfigs {
		log.Info("existing configs", "nsn", nsn, "config", config)
	}

	return existingConfigs
}

func buildConfig(ctx context.Context, configSet *configv1alpha1.ConfigSet, target types.NamespacedName) *configv1alpha1.Config {
	labels := configSet.Labels
	if len(labels) == 0 {
		labels = map[string]string{}
	}
	labels[configv1alpha1.TargetNameKey] = target.Name
	labels[configv1alpha1.TargetNamespaceKey] = target.Namespace

	return configv1alpha1.BuildConfig(
		metav1.ObjectMeta{
			Name:        fmt.Sprintf("%s-%s", configSet.Name, target.Name),
			Namespace:   configSet.Namespace,
			Labels:      labels,
			Annotations: configSet.Annotations,
			OwnerReferences: []metav1.OwnerReference{
				{
					APIVersion: configSet.APIVersion,
					Kind:       configSet.Kind,
					Name:       configSet.Name,
					UID:        configSet.UID,
					Controller: pointer.Bool(true),
				},
			},
		},
		configv1alpha1.ConfigSpec{
			Lifecycle: configSet.Spec.Lifecycle,
			Priority:  configSet.Spec.Priority,
			Config:    configSet.Spec.Config,
		},
		configv1alpha1.ConfigStatus{},
	)
}

func (r *configCommon) storeCreateConfigSet(ctx context.Context, key store.Key, configset *configv1alpha1.ConfigSet) error {
	if err := r.configSetStore.Create(ctx, key, configset); err != nil {
		return apierrors.NewInternalError(err)
	}
	r.notifyWatcher(ctx, watch.Event{
		Type:   watch.Added,
		Object: configset,
	})
	return nil
}

func (r *configCommon) storeUpdateConfigSet(ctx context.Context, key store.Key, configset *configv1alpha1.ConfigSet) error {
	if err := r.configSetStore.Update(ctx, key, configset); err != nil {
		return apierrors.NewInternalError(err)
	}
	r.notifyWatcher(ctx, watch.Event{
		Type:   watch.Modified,
		Object: configset,
	})
	return nil
}

func (r *configCommon) storeDeleteConfigSet(ctx context.Context, key store.Key, configset *configv1alpha1.ConfigSet) error {
	if err := r.configSetStore.Delete(ctx, key); err != nil {
		return apierrors.NewInternalError(err)
	}
	r.notifyWatcher(ctx, watch.Event{
		Type:   watch.Deleted,
		Object: configset,
	})
	return nil
}
