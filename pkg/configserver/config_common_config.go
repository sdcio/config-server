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
	"strings"

	"github.com/henderiw/logger/log"
	configv1alpha1 "github.com/iptecharch/config-server/apis/config/v1alpha1"
	"github.com/iptecharch/config-server/pkg/store"
	"github.com/pkg/errors"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/util/uuid"
	"k8s.io/apiserver/pkg/registry/rest"
)

func (r *configCommon) createConfig(ctx context.Context,
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

	key, targetKey, err := r.getKeys(ctx, runtimeObject)
	if err != nil {
		return nil, apierrors.NewBadRequest(err.Error())
	}
	log.Info("create", "key", key.String(), "targetKey", targetKey)

	// get the data of the runtime object
	newConfig, ok := runtimeObject.(*configv1alpha1.Config)
	if !ok {
		return nil, apierrors.NewBadRequest(fmt.Sprintf("expected Config object, got %T", runtimeObject))
	}
	log.Info("create", "obj", string(newConfig.Spec.Config[0].Value.Raw))

	// interact with the data server
	tctx, err := r.targetStore.Get(ctx, targetKey)
	if err != nil {
		return nil, apierrors.NewInternalError(errors.Wrap(err, "target not found"))
	}
	if err := tctx.SetIntent(ctx, targetKey, newConfig); err != nil {
		return nil, apierrors.NewInternalError(err)
	}
	log.Info("create intent succeeded")

	newConfig.Status.SetConditions(configv1alpha1.Ready())
	newConfig.Status.LastKnownGoodSchema = &configv1alpha1.ConfigStatusLastKnownGoodSchema{
		Type:    tctx.DataStore.Schema.Name,
		Vendor:  tctx.DataStore.Schema.Vendor,
		Version: tctx.DataStore.Schema.Version,
	}
	if err := r.configStore.Create(ctx, key, newConfig); err != nil {
		return nil, apierrors.NewInternalError(err)
	}

	return newConfig, nil
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

	oldObj, err := r.configStore.Get(ctx, key)
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
	oldConfig, ok := oldObj.(*configv1alpha1.Config)
	if !ok {
		return nil, false, apierrors.NewBadRequest(fmt.Sprintf("expected old Config object, got %T", oldConfig))
	}

	newObj, err := objInfo.UpdatedObject(ctx, oldObj)
	if err != nil {
		log.Info("update failed to construct UpdatedObject", "error", err.Error())
		return nil, false, err
	}
	accessor, err := meta.Accessor(newObj)
	if err != nil {
		return nil, false, apierrors.NewBadRequest(err.Error())
	}
	accessor.SetResourceVersion(generateRandomString(6))

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

	targetKey, err := getTargetKey(newConfig.GetLabels())
	if err != nil {
		return nil, false, apierrors.NewBadRequest(err.Error())
	}
	return r.upsertTargetConfig(ctx, key, targetKey, newConfig, isCreate)
}

func (r *configCommon) deleteConfig(
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
			log.Info("delete validation failed", "error", err)
			return nil, false, err
		}
	}
	if len(newConfig.Finalizers) > 0 {
		if err := r.configSetStore.Update(ctx, key, newConfig); err != nil {
			return nil, false, apierrors.NewInternalError(err)
		}
		return newConfig, false, nil
	}

	targetKey, err := getTargetKey(newConfig.GetLabels())
	if err != nil {
		return nil, false, apierrors.NewBadRequest(err.Error())
	}

	// interact with the data server
	tctx, err := r.targetStore.Get(ctx, targetKey)
	if err != nil {
		return nil, false, apierrors.NewInternalError(err)
	}
	if err := tctx.DeleteIntent(ctx, targetKey, newConfig); err != nil {
		if options.GracePeriodSeconds != nil && *options.GracePeriodSeconds == 0 {
			log.Info("delete intent from target failed, ignoring error and delete from cache", "error", err)
			if err := r.configStore.Delete(ctx, key); err != nil {
				return nil, false, apierrors.NewInternalError(err)
			}
			log.Info("delete intent from store succeeded")

			return newConfig, true, nil
		}
		return nil, false, apierrors.NewInternalError(err)
	}
	log.Info("delete intent from target succeeded")

	if err := r.configStore.Delete(ctx, key); err != nil {
		return nil, false, apierrors.NewInternalError(err)
	}
	log.Info("delete intent from store succeeded")
	return newConfig, true, nil
}

func (r *configCommon) upsertTargetConfig(ctx context.Context, key, targetKey store.Key, config *configv1alpha1.Config, isCreate bool) (*configv1alpha1.Config, bool, error) {
	// interact with the data server
	tctx, err := r.targetStore.Get(ctx, targetKey)
	if err != nil {
		return nil, false, apierrors.NewInternalError(err)
	}
	if !isCreate {
		if err := tctx.SetIntent(ctx, targetKey, config); err != nil {
			return nil, false, apierrors.NewInternalError(err)
		}

		config.Status.SetConditions(configv1alpha1.Ready())
		config.Status.LastKnownGoodSchema = &configv1alpha1.ConfigStatusLastKnownGoodSchema{
			Type:    tctx.DataStore.Schema.Name,
			Vendor:  tctx.DataStore.Schema.Vendor,
			Version: tctx.DataStore.Schema.Version,
		}
		if err := r.configStore.Update(ctx, key, config); err != nil {
			return nil, false, apierrors.NewInternalError(err)
		}
		return config, false, nil
	}
	if err := tctx.SetIntent(ctx, targetKey, config); err != nil {
		return nil, false, apierrors.NewInternalError(err)
	}
	config.Status.SetConditions(configv1alpha1.Ready())
	config.Status.LastKnownGoodSchema = &configv1alpha1.ConfigStatusLastKnownGoodSchema{
		Type:    tctx.DataStore.Schema.Name,
		Vendor:  tctx.DataStore.Schema.Vendor,
		Version: tctx.DataStore.Schema.Version,
	}
	if err := r.configStore.Create(ctx, key, config); err != nil {
		return nil, false, apierrors.NewInternalError(err)
	}
	return config, true, nil

}
