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
	"time"

	"github.com/henderiw/logger/log"
	configv1alpha1 "github.com/iptecharch/config-server/apis/config/v1alpha1"
	invv1alpha1 "github.com/iptecharch/config-server/apis/inv/v1alpha1"
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

	// interact with the data server if the targets are ready
	target := &invv1alpha1.Target{}
	if err := r.client.Get(ctx, targetKey.NamespacedName, target); err != nil {
		return nil, apierrors.NewInternalError(err)
	}
	if !target.IsReady() {
		return nil, apierrors.NewInternalError(fmt.Errorf("target not ready"))
	}
	tctx, err := r.targetStore.Get(ctx, targetKey)
	if err != nil {
		return nil, apierrors.NewInternalError(errors.Wrap(err, "target not found"))
	}
	log.Info("create intent validation succeeded, transacting async to the target")
	newConfig.Status.SetConditions(configv1alpha1.Creating())
	if err := r.configStore.Create(ctx, key, newConfig); err != nil {
		return nil, apierrors.NewInternalError(err)
	}

	go func() {
		nctx, cancel := context.WithTimeout(context.TODO(), 5*time.Minute)
		defer cancel()
		if err := tctx.SetIntent(nctx, targetKey, newConfig); err != nil {
			newConfig.SetConditions(configv1alpha1.Failed(err.Error()))
			if err := r.configStore.Update(ctx, key, newConfig); err != nil {
				log.Info("cannot update store", "err", err.Error())
			}
			log.Info("create transaction failed", "err", err.Error())
			return
		}
		log.Info("create transaction succeeded")

		newConfig.Status.SetConditions(configv1alpha1.Ready())
		newConfig.Status.LastKnownGoodSchema = &configv1alpha1.ConfigStatusLastKnownGoodSchema{
			Type:    tctx.DataStore.Schema.Name,
			Vendor:  tctx.DataStore.Schema.Vendor,
			Version: tctx.DataStore.Schema.Version,
		}
		if err := r.configStore.Update(ctx, key, newConfig); err != nil {
			log.Info("cannot update store", "err", err.Error())
		}
	}()

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

	// interact with the data server if the target is ready
	target := &invv1alpha1.Target{}
	if err := r.client.Get(ctx, targetKey.NamespacedName, target); err != nil {
		return nil, false, apierrors.NewInternalError(err)
	}
	if !target.IsReady() {
		return nil, false, apierrors.NewInternalError(fmt.Errorf("target not ready"))
	}
	tctx, err := r.targetStore.Get(ctx, targetKey)
	if err != nil {
		return nil, false, apierrors.NewInternalError(err)
	}
	log.Info("delete intent validation succeeded, transacting async to the target")
	newConfig.Status.SetConditions(configv1alpha1.Deleting())
	if err := r.configStore.Update(ctx, key, newConfig); err != nil {
		return nil, false, apierrors.NewInternalError(err)
	}

	go func() {
		nctx, cancel := context.WithTimeout(context.TODO(), 5*time.Minute)
		defer cancel()
		if err := tctx.DeleteIntent(nctx, targetKey, newConfig); err != nil {
			if options.GracePeriodSeconds != nil && *options.GracePeriodSeconds == 0 {
				log.Info("delete config from target failed, ignoring error and delete from store", "error", err)
				if err := r.configStore.Delete(ctx, key); err != nil {
					log.Info("cannot delete config from store", "err", err.Error())
				}
				log.Info("delete intent from store succeeded")
				return
			}
			newConfig.SetConditions(configv1alpha1.Failed(err.Error()))
			if err := r.configStore.Update(ctx, key, newConfig); err != nil {
				log.Info("cannot update config in store", "err", err.Error())
			}
			log.Info("delete transaction failed", "err", err.Error())
			return
		}
		time.Sleep(2 *time.Second)
		log.Info("delete transaction succeeded")
		if err := r.configStore.Delete(ctx, key); err != nil {
			log.Info("cannot delete config from store", "err", err.Error())
		}
		log.Info("delete config from store succeeded")
	}()

	return newConfig, true, nil
}

func (r *configCommon) upsertTargetConfig(ctx context.Context, key, targetKey store.Key, oldConfig, newConfig *configv1alpha1.Config, isCreate bool) (*configv1alpha1.Config, bool, error) {
	log := log.FromContext(ctx)
	// interact with the data server if the target is ready
	target := &invv1alpha1.Target{}
	if err := r.client.Get(ctx, targetKey.NamespacedName, target); err != nil {
		return nil, false, apierrors.NewInternalError(err)
	}
	if !target.IsReady() {
		return nil, false, apierrors.NewInternalError(fmt.Errorf("target not ready"))
	}
	tctx, err := r.targetStore.Get(ctx, targetKey)
	if err != nil {
		return nil, false, apierrors.NewInternalError(err)
	}

	if newConfig.IsTransacting() {
		return nil, false, apierrors.NewInternalError(fmt.Errorf("transacting ongoing"))
	}

	if !isCreate {
		log.Info("create intent validation succeeded, transacting async to the target")
		newConfig.Status.SetConditions(configv1alpha1.Creating())
		if err := r.configStore.Create(ctx, key, newConfig); err != nil {
			return nil, false, apierrors.NewInternalError(err)
		}

		go func() {
			nctx, cancel := context.WithTimeout(context.TODO(), 5*time.Minute)
			defer cancel()
			if err := tctx.SetIntent(nctx, targetKey, newConfig); err != nil {
				newConfig.SetConditions(configv1alpha1.Failed(err.Error()))
				if err := r.configStore.Update(ctx, key, newConfig); err != nil {
					log.Info("cannot update store", "err", err.Error())
				}
				log.Info("create transaction failed", "err", err.Error())
				return
			}
			log.Info("create transaction succeeded")

			newConfig.Status.SetConditions(configv1alpha1.Ready())
			newConfig.Status.LastKnownGoodSchema = &configv1alpha1.ConfigStatusLastKnownGoodSchema{
				Type:    tctx.DataStore.Schema.Name,
				Vendor:  tctx.DataStore.Schema.Vendor,
				Version: tctx.DataStore.Schema.Version,
			}
			if err := r.configStore.Update(ctx, key, newConfig); err != nil {
				log.Info("cannot update store", "err", err.Error())
			}
		}()
		return newConfig, false, nil
	}

	log.Info("update config validation succeeded, transacting async to the target")
	// Here we keep the old config since the update might fail, as such we always
	// can go back to the original state
	oldConfig.Status.SetConditions(configv1alpha1.Updating())
	if err := r.configStore.Update(ctx, key, oldConfig); err != nil {
		return nil, false, apierrors.NewInternalError(err)
	}

	go func() {
		nctx, cancel := context.WithTimeout(context.TODO(), 5*time.Minute)
		defer cancel()
		if err := tctx.SetIntent(nctx, targetKey, newConfig); err != nil {
			newConfig.SetConditions(configv1alpha1.Failed(err.Error()))
			if err := r.configStore.Update(ctx, key, newConfig); err != nil {
				log.Info("cannot update store", "err", err.Error())
			}
			log.Info("update transaction failed", "err", err.Error())
			return
		}
		log.Info("update transaction succeeded")

		newConfig.Status.SetConditions(configv1alpha1.Ready())
		newConfig.Status.LastKnownGoodSchema = &configv1alpha1.ConfigStatusLastKnownGoodSchema{
			Type:    tctx.DataStore.Schema.Name,
			Vendor:  tctx.DataStore.Schema.Vendor,
			Version: tctx.DataStore.Schema.Version,
		}
		if err := r.configStore.Update(ctx, key, newConfig); err != nil {
			log.Info("cannot update store", "err", err.Error())
		}
	}()
	return newConfig, true, nil
}
