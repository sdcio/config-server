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

package handlers

import (
	"context"
	"fmt"

	"github.com/sdcio/config-server/apis/config"
	configv1alpha1 "github.com/sdcio/config-server/apis/config/v1alpha1"
	targetmanager "github.com/sdcio/config-server/pkg/sdc/target/manager"
	sdcpb "github.com/sdcio/sdc-protos/sdcpb"
	"k8s.io/apimachinery/pkg/api/meta"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

type ConfigStoreHandler struct {
	Client client.Client
}

func (r *ConfigStoreHandler) DryRunCreateFn(ctx context.Context, key types.NamespacedName, obj runtime.Object, dryrun bool) (runtime.Object, error) {
	c, target, err := r.prepareConfigAndTarget(ctx, key, obj)
	if err != nil {
		return obj, err
	}

	updates, err := config.GetIntentUpdate(c, true)
	if err != nil {
		return nil, err
	}

	intents := []*sdcpb.TransactionIntent{
		{
			Intent:       config.GetGVKNSN(c),
			Priority:     int32(c.Spec.Priority),
			Update:       updates,
			// Dont set not Revertive
		},
	}

	return targetmanager.RunDryRunTransaction(ctx, key, c, target, intents, dryrun)
}
func (r *ConfigStoreHandler) DryRunUpdateFn(ctx context.Context, key types.NamespacedName, obj, old runtime.Object, dryrun bool) (runtime.Object, error) {
	c, target, err := r.prepareConfigAndTarget(ctx, key, obj)
	if err != nil {
		return obj, err
	}

	updates, err := config.GetIntentUpdate(c, true)
	if err != nil {
		return nil, err
	}

	intents := []*sdcpb.TransactionIntent{
		{
			Intent:       config.GetGVKNSN(c),
			Priority:     int32(c.Spec.Priority),
			Update:       updates,
			// Dont set not Revertive
		},
	}

	return targetmanager.RunDryRunTransaction(ctx, key, c, target, intents, dryrun)
}
func (r *ConfigStoreHandler) DryRunDeleteFn(ctx context.Context, key types.NamespacedName, obj runtime.Object, dryrun bool) (runtime.Object, error) {
	c, target, err := r.prepareConfigAndTarget(ctx, key, obj)
	if err != nil {
		return obj, err
	}

	intents := []*sdcpb.TransactionIntent{
		{
			Intent: config.GetGVKNSN(c),
			Delete: true,
		},
	}

	return targetmanager.RunDryRunTransaction(ctx, key, c, target, intents, dryrun)
}

// prepareConfigAndTarget validates labels, casts the object, fetches the Target
// and ensures it's ready.
func (r *ConfigStoreHandler) prepareConfigAndTarget(
	ctx context.Context,
	key types.NamespacedName,
	obj runtime.Object,
) (*config.Config, *configv1alpha1.Target, error) {
	accessor, err := meta.Accessor(obj)
	if err != nil {
		return nil, nil, err
	}

	if _, err := config.GetTargetKey(accessor.GetLabels()); err != nil {
		return nil, nil, err
	}

	c, ok := obj.(*config.Config)
	if !ok {
		return nil, nil, fmt.Errorf("expected *config.Config, got %T", obj)
	}

	target := &configv1alpha1.Target{}
	if err := r.Client.Get(ctx, key, target); err != nil {
		return nil, nil, err
	}

	if !target.IsReady() {
		return nil, nil, fmt.Errorf("target not ready %s", key)
	}

	return c, target, nil
}
