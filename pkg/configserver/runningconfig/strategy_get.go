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

package runningconfig

import (
	"context"
	"fmt"

	"github.com/henderiw/apiserver-store/pkg/storebackend"
	invv1alpha1 "github.com/sdcio/config-server/apis/inv/v1alpha1"
	"github.com/sdcio/config-server/pkg/target"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
)

func (r *strategy) Get(ctx context.Context, key types.NamespacedName) (runtime.Object, error) {
	target, tctx, err := r.getTargetRunningContext(ctx, key)
	if err != nil {
		return nil, apierrors.NewNotFound(r.gr, key.Name)
	}

	rc, err := tctx.GetData(ctx, storebackend.KeyFromNSN(key))
	if err != nil {
		return nil, apierrors.NewInternalError(err)
	}
	rc.SetCreationTimestamp(target.CreationTimestamp)
	rc.SetResourceVersion(target.ResourceVersion)
	rc.SetAnnotations(target.Annotations)
	rc.SetLabels(target.Labels)
	obj := rc

	return obj, nil
}

func (r *strategy) getTargetRunningContext(ctx context.Context, targetKey types.NamespacedName) (*invv1alpha1.Target, *target.Context, error) {
	target := &invv1alpha1.Target{}
	if err := r.client.Get(ctx, targetKey, target); err != nil {
		return nil, nil, apierrors.NewNotFound(r.gr, targetKey.Name)
	}
	if !target.DeletionTimestamp.IsZero() {
		return nil, nil, apierrors.NewNotFound(r.gr, targetKey.Name)
	}
	if !target.IsDatastoreReady() {
		return nil, nil, apierrors.NewInternalError(fmt.Errorf("target not ready"))
	}
	tctx, err := r.targetStore.Get(ctx, storebackend.KeyFromNSN(targetKey))
	if err != nil {
		return nil, nil, apierrors.NewNotFound(r.gr, targetKey.Name)
	}
	return target, &tctx, nil
}
