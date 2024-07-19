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

package target

import (
	"context"
	errors "errors"

	"github.com/henderiw/apiserver-store/pkg/storebackend"
	pkgerrors "github.com/pkg/errors"
	"github.com/sdcio/config-server/apis/config"
	invv1alpha1 "github.com/sdcio/config-server/apis/inv/v1alpha1"
	sdcerrors "github.com/sdcio/config-server/pkg/errors"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

var LookupError = errors.New("target lookup error")

func NewTargetHandler(client client.Client, targetStore storebackend.Storer[*Context]) *TargetHandler {
	return &TargetHandler{
		client:      client,
		targetStore: targetStore,
	}
}

type TargetHandler struct {
	client      client.Client
	targetStore storebackend.Storer[*Context]
}

// GetTargetContext returns a invTarget and targetContext when the target is ready and the ctx is found
func (r *TargetHandler) GetTargetContext(ctx context.Context, targetKey types.NamespacedName) (*invv1alpha1.Target, *Context, error) {
	target := &invv1alpha1.Target{}
	if err := r.client.Get(ctx, targetKey, target); err != nil {
		return nil, nil, &sdcerrors.RecoverableError{
			Message:      "target get failed",
			WrappedError: errors.Join(LookupError, err),
		}
	}
	if !target.IsReady() {
		return nil, nil, &sdcerrors.RecoverableError{
			Message:      "target not ready",
			WrappedError: pkgerrors.Wrap(LookupError, string(config.ConditionReasonTargetNotReady)),
		}
	}
	tctx, err := r.targetStore.Get(ctx, storebackend.Key{NamespacedName: targetKey})
	if err != nil {
		return nil, nil, &sdcerrors.RecoverableError{
			Message:      "target not found",
			WrappedError: pkgerrors.Wrap(LookupError, string(config.ConditionReasonTargetNotFound)),
		}
	}
	return target, tctx, nil
}

func (r *TargetHandler) SetIntent(ctx context.Context, targetKey types.NamespacedName, config *config.Config, useSpec, dryRun bool) (*config.Config, *config.ConfigStatusLastKnownGoodSchema, error) {
	_, tctx, err := r.GetTargetContext(ctx, targetKey)
	if err != nil {
		return nil, nil, err
	}
	schema := tctx.GetSchema()
	c, err := tctx.SetIntent(ctx, storebackend.Key{NamespacedName: targetKey}, config, useSpec, dryRun)
	if err != nil {
		return nil, nil, err
	}
	return c, schema, nil
}

func (r *TargetHandler) DeleteIntent(ctx context.Context, targetKey types.NamespacedName, config *config.Config, dryRun bool) (*config.Config, error) {
	_, tctx, err := r.GetTargetContext(ctx, targetKey)
	if err != nil {
		return nil, err
	}
	return tctx.DeleteIntent(ctx, storebackend.Key{NamespacedName: targetKey}, config, dryRun)
}

func (r *TargetHandler) GetData(ctx context.Context, targetKey types.NamespacedName) (*config.RunningConfig, error) {
	_, tctx, err := r.GetTargetContext(ctx, targetKey)
	if err != nil {
		return nil, err
	}
	return tctx.GetData(ctx, storebackend.Key{NamespacedName: targetKey})
}
