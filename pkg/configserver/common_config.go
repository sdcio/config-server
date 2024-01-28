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
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/henderiw/logger/log"
	configv1alpha1 "github.com/iptecharch/config-server/apis/config/v1alpha1"
	invv1alpha1 "github.com/iptecharch/config-server/apis/inv/v1alpha1"
	"github.com/iptecharch/config-server/pkg/store"
	"github.com/iptecharch/config-server/pkg/target"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/util/uuid"
	"k8s.io/apimachinery/pkg/watch"
	"k8s.io/apiserver/pkg/registry/rest"
)

func (r *configCommon) getTargetContext(ctx context.Context, targetKey store.Key) (*target.Context, error) {
	target := &invv1alpha1.Target{}
	if err := r.client.Get(ctx, targetKey.NamespacedName, target); err != nil {
		return nil, err
	}
	if !target.IsConfigReady() {
		return nil, errors.New(string(configv1alpha1.ConditionReasonTargetNotReady))
	}
	tctx, err := r.targetStore.Get(ctx, targetKey)
	if err != nil {
		return nil, errors.New(string(configv1alpha1.ConditionReasonTargetNotFound))
	}
	return &tctx, nil
}

func (r *configCommon) createConfig(ctx context.Context,
	runtimeObject runtime.Object,
	createValidation rest.ValidateObjectFunc,
	options *metav1.CreateOptions) (runtime.Object, error) {

	// setting a uid for the element
	accessor, err := meta.Accessor(runtimeObject)
	if err != nil {
		return nil, apierrors.NewBadRequest(err.Error())
	}
	accessor.SetUID(uuid.NewUUID())
	accessor.SetCreationTimestamp(metav1.Now())
	accessor.SetResourceVersion(generateRandomString(6))

	key, targetKey, err := r.getKeys(ctx, runtimeObject)
	if err != nil {
		return nil, apierrors.NewBadRequest(err.Error())
	}
	log := log.FromContext(ctx).With("operation", "create", "key", key.String(), "targetKey", targetKey)
	log.Info("start")

	// get the data of the runtime object
	newConfig, ok := runtimeObject.(*configv1alpha1.Config)
	if !ok {
		return nil, apierrors.NewBadRequest(fmt.Sprintf("expected Config object, got %T", runtimeObject))
	}

	newConfig, _, err = r.upsertTargetConfig(ctx, key, targetKey, newConfig, newConfig, true)
	return newConfig, err

	/*
		// get targetCtx is the target is ready, otherwise update the condition with the failure indication
		// and store the config.
		tctx, err := r.getTargetContext(ctx, targetKey)
		if err != nil {
			newConfig.SetConditions(configv1alpha1.Failed(err.Error()))
			return newConfig, r.storeCreateConfig(ctx, key, newConfig)
		}

		// ready to transact with the datastore
		log.Info("transacting with target")
		newConfig.Status.SetConditions(configv1alpha1.Creating())
		if err := r.storeCreateConfig(ctx, key, newConfig); err != nil {
			return newConfig, err
		}
		// async transaction to the datastore
		go func() {
			if err := r.setIntent(ctx, key, targetKey, tctx, newConfig, true); err != nil {
				log.Error("transaction failed", "error", err)
			}
			log.Info("transaction succeeded", "error", err)
		}()

		return newConfig, nil
	*/
}

func (r *configCommon) updateConfig(
	ctx context.Context,
	name string,
	objInfo rest.UpdatedObjectInfo,
	createValidation rest.ValidateObjectFunc,
	updateValidation rest.ValidateObjectUpdateFunc,
	forceAllowCreate bool,
	options *metav1.UpdateOptions,
) (runtime.Object, bool, error) {

	// Get Key
	key, err := r.getKey(ctx, name)
	if err != nil {
		return nil, false, apierrors.NewBadRequest(err.Error())
	}
	log := log.FromContext(ctx).With("operation", "update", "key", key.String())
	log.Info("start")

	// isCreate tracks whether this is an update that creates an object (this happens in server-side apply)
	isCreate := false

	oldObj, err := r.configStore.Get(ctx, key)
	if err != nil {
		log.Error("update", "err", err.Error())
		if forceAllowCreate && strings.Contains(err.Error(), "not found") {
			// For server-side apply, we can create the object here
			isCreate = true
		} else {
			return nil, false, err
		}
	}
	// get the data of the runtime object
	oldConfig, ok := oldObj.(*configv1alpha1.Config)
	if !ok {
		return nil, false, apierrors.NewBadRequest(fmt.Sprintf("expected old Config object, got %T", oldConfig))
	}

	newObj, err := objInfo.UpdatedObject(ctx, oldObj)
	if err != nil {
		log.Error("update failed to construct UpdatedObject", "error", err.Error())
		return nil, false, err
	}

	// get the data of the runtime object
	newConfig, ok := newObj.(*configv1alpha1.Config)
	if !ok {
		return nil, false, apierrors.NewBadRequest(fmt.Sprintf("expected Config object, got %T", newObj))
	}

	if oldConfig.GetResourceVersion() != newConfig.GetResourceVersion() {
		return nil, false, apierrors.NewConflict(configv1alpha1.Resource("configs"), oldConfig.GetName(), fmt.Errorf(OptimisticLockErrorMsg))
	}
	if oldConfig.DeletionTimestamp != nil && len(newConfig.Finalizers) == 0 {
		if err := r.configStore.Delete(ctx, key); err != nil {
			return nil, false, apierrors.NewInternalError(err)
		}
		// deleted
		return newConfig, false, nil
	}

	accessor, err := meta.Accessor(newObj)
	if err != nil {
		return nil, false, apierrors.NewBadRequest(err.Error())
	}
	accessor.SetResourceVersion(generateRandomString(6))

	targetKey, err := getTargetKey(newConfig.GetLabels())
	if err != nil {
		return nil, false, apierrors.NewBadRequest(err.Error())
	}
	return r.upsertTargetConfig(ctx, key, targetKey, oldConfig, newConfig, isCreate)
}

func (r *configCommon) deleteConfig(
	ctx context.Context,
	name string,
	deleteValidation rest.ValidateObjectFunc,
	options *metav1.DeleteOptions,
) (runtime.Object, bool, error) {

	// Get Key
	key, err := r.getKey(ctx, name)
	if err != nil {
		return nil, false, apierrors.NewBadRequest(err.Error())
	}
	log := log.FromContext(ctx).With("operation", "delete", "key", key.String())
	log.Info("start")

	obj, err := r.configStore.Get(ctx, key)
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
	newConfig, ok := obj.(*configv1alpha1.Config)
	if !ok {
		return nil, false, apierrors.NewBadRequest(fmt.Sprintf("expected Config object, got %T", obj))
	}

	if deleteValidation != nil {
		err := deleteValidation(ctx, newConfig)
		if err != nil {
			log.Error("validation failed", "error", err)
			return nil, false, err
		}
	}
	if len(newConfig.Finalizers) > 0 {
		if err := r.storeUpdateConfig(ctx, key, newConfig); err != nil {
			return nil, false, apierrors.NewInternalError(err)
		}
		return newConfig, false, nil
	}

	targetKey, err := getTargetKey(newConfig.GetLabels())
	if err != nil {
		return nil, false, apierrors.NewBadRequest(err.Error())
	}

	// this is a forced delete - we delete irrespective if the target is available ot not
	if options.GracePeriodSeconds != nil && *options.GracePeriodSeconds == 0 {
		log.Info("transation failed, ignoring error, target config must be manually cleaned up", "error", err)
		if err := r.storeDeleteConfig(ctx, key, newConfig); err != nil {
			log.Error("cannot delete config from store", "err", err.Error())
			return newConfig, true, nil
		}
		return newConfig, true, nil
	}
	// get targetCtx is the target is ready, otherwise update the condition with the failure indication
	// and store the config.
	tctx, err := r.getTargetContext(ctx, targetKey)
	if err != nil {
		newConfig.SetConditions(configv1alpha1.Failed(err.Error()))
		if err := r.storeUpdateConfig(ctx, key, newConfig); err != nil {
			return nil, false, apierrors.NewInternalError(err)
		}
		return newConfig, false, nil
	}
	// This is a bit tricky but async config interaction with the k8s client is challenging
	// the client by default has a wait option which has the following behavior
	// watch (gets a modify event), client does a get (if a response is received) it does a new watch
	// For large Configs the wait option should be set to off
	newConfig.Status.SetConditions(configv1alpha1.Deleting())
	if err := r.storeUpdateConfig(ctx, key, newConfig); err != nil {
		return nil, false, apierrors.NewInternalError(err)
	}

	// async transaction to the datastore
	go func() {
		if err := r.deleteIntent(ctx, key, targetKey, tctx, newConfig, options); err != nil {
			log.Error("transaction failed", "error", err)
		}
		log.Info("transaction succeeded", "error", err)
	}()

	return newConfig, true, nil
}

func (r *configCommon) upsertTargetConfig(ctx context.Context, key, targetKey store.Key, oldConfig, newConfig *configv1alpha1.Config, isCreate bool) (*configv1alpha1.Config, bool, error) {
	log := log.FromContext(ctx)
	// interact with the data server if the target is ready
	tctx, err := r.getTargetContext(ctx, targetKey)
	if err != nil {
		newConfig.SetConditions(configv1alpha1.Failed(err.Error()))
		if err := r.storeUpdateConfig(ctx, key, newConfig); err != nil {
			return nil, false, apierrors.NewInternalError(err)
		}
		return newConfig, false, nil
	}

	if oldConfig.IsTransacting() {
		return nil, false, apierrors.NewInternalError(fmt.Errorf("transacting ongoing"))
	}

	// transacting with target
	if isCreate {
		newConfig.Status.SetConditions(configv1alpha1.Creating())
		if err := r.storeCreateConfig(ctx, key, newConfig); err != nil {
			return nil, false, apierrors.NewInternalError(err)
		}

		go func() {
			if err := r.setIntent(ctx, key, targetKey, tctx, newConfig, true); err != nil {
				log.Error("transaction failed", "error", err)
			}
			log.Info("transaction succeeded", "error", err)
		}()
		return newConfig, false, nil
	}
	// Here we keep the old config since the update might fail, as such we always
	// can go back to the original state
	oldConfig.Status.SetConditions(configv1alpha1.Updating())
	if err := r.storeUpdateConfig(ctx, key, oldConfig); err != nil {
		return nil, false, apierrors.NewInternalError(err)
	}

	go func() {
		if err := r.setIntent(ctx, key, targetKey, tctx, newConfig, true); err != nil {
			log.Error("transaction failed", "error", err)
		}
		log.Info("transaction succeeded", "error", err)
	}()
	return newConfig, true, nil
}

func (r *configCommon) setIntent(ctx context.Context, key, targetKey store.Key, tctx *target.Context, newConfig *configv1alpha1.Config, spec bool) error {
	log := log.FromContext(ctx)
	log.Info("transacting with target", "config", newConfig, "spec", spec)
	nctx, cancel := context.WithTimeout(context.TODO(), 5*time.Minute)
	defer cancel()
	if err := tctx.SetIntent(nctx, targetKey, newConfig, spec); err != nil {
		newConfig.SetConditions(configv1alpha1.Failed(err.Error()))
		if err := r.storeUpdateConfig(ctx, key, newConfig); err != nil {
			log.Error("cannot update store", "err", err.Error())
			return err
		}
		return err
	}

	newConfig.Status.SetConditions(configv1alpha1.Ready())
	newConfig.Status.LastKnownGoodSchema = &configv1alpha1.ConfigStatusLastKnownGoodSchema{
		Type:    tctx.DataStore.Schema.Name,
		Vendor:  tctx.DataStore.Schema.Vendor,
		Version: tctx.DataStore.Schema.Version,
	}
	newConfig.Status.AppliedConfig = &newConfig.Spec
	if err := r.storeUpdateConfig(ctx, key, newConfig); err != nil {
		log.Error("cannot update store", "err", err.Error())
		return err
	}
	return nil
}

func (r *configCommon) deleteIntent(ctx context.Context, key, targetKey store.Key, tctx *target.Context, newConfig *configv1alpha1.Config, options *metav1.DeleteOptions) error {
	log := log.FromContext(ctx)
	log.Info("transacting with target")
	nctx, cancel := context.WithTimeout(context.TODO(), 5*time.Minute)
	defer cancel()
	if err := tctx.DeleteIntent(nctx, targetKey, newConfig); err != nil {
		newConfig.SetConditions(configv1alpha1.Failed(err.Error()))
		if err := r.storeUpdateConfig(ctx, key, newConfig); err != nil {
			log.Error("cannot update config in store", "err", err.Error())
			return err
		}
		return err
	}
	if err := r.storeDeleteConfig(ctx, key, newConfig); err != nil {
		log.Error("cannot delete config from store", "err", err.Error())
		return err
	}
	return nil
}

func (r *configCommon) storeCreateConfig(ctx context.Context, key store.Key, config *configv1alpha1.Config) error {
	if err := r.configStore.Create(ctx, key, config); err != nil {
		return apierrors.NewInternalError(err)
	}
	r.notifyWatcher(ctx, watch.Event{
		Type:   watch.Added,
		Object: config,
	})
	return nil
}

func (r *configCommon) storeUpdateConfig(ctx context.Context, key store.Key, config *configv1alpha1.Config) error {
	if err := r.configStore.Update(ctx, key, config); err != nil {
		return apierrors.NewInternalError(err)
	}
	r.notifyWatcher(ctx, watch.Event{
		Type:   watch.Modified,
		Object: config,
	})
	return nil
}

func (r *configCommon) storeDeleteConfig(ctx context.Context, key store.Key, config *configv1alpha1.Config) error {
	if err := r.configStore.Delete(ctx, key); err != nil {
		return apierrors.NewInternalError(err)
	}
	r.notifyWatcher(ctx, watch.Event{
		Type:   watch.Deleted,
		Object: config,
	})
	return nil
}
