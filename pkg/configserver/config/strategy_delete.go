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

package config

import (
	"context"
	"fmt"

	"github.com/henderiw/apiserver-store/pkg/storebackend"
	configv1alpha1 "github.com/sdcio/config-server/apis/config/v1alpha1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/watch"
)

func (r *strategy) BeginDelete(ctx context.Context) error { return nil }

func (r *strategy) Delete(ctx context.Context, key types.NamespacedName, obj runtime.Object, dryrun bool) (runtime.Object, error) {
	if dryrun {
		accessor, err := meta.Accessor(obj)
		if err != nil {
			return obj, err
		}
		tctx, targetKey, err := r.getTargetInfo(ctx, accessor)
		if err != nil {
			return obj, err
		}
		config, ok := obj.(*configv1alpha1.Config)
		if !ok {
			return obj, fmt.Errorf("unexpected objext, got")
		}
		return tctx.DeleteIntent(ctx, targetKey, config, dryrun)
	}

	if err := r.store.Delete(ctx, storebackend.KeyFromNSN(key)); err != nil {
		return obj, apierrors.NewInternalError(err)
	}
	r.notifyWatcher(ctx, watch.Event{
		Type:   watch.Deleted,
		Object: obj,
	})
	return obj, nil
}
