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

type TargetHandler interface {
	GetTargetContext(ctx context.Context, targetKey types.NamespacedName) (*invv1alpha1.Target, *Context, error)
	SetIntent(ctx context.Context, targetKey types.NamespacedName, config *config.Config, dryRun bool) (*config.ConfigStatusLastKnownGoodSchema, string, error)
	DeleteIntent(ctx context.Context, targetKey types.NamespacedName, config *config.Config, dryRun bool) (string, error)
	GetData(ctx context.Context, targetKey types.NamespacedName) (*config.RunningConfig, error)
	RecoverIntents(ctx context.Context, targetKey types.NamespacedName, configs []*config.Config) (*config.ConfigStatusLastKnownGoodSchema, string, error)
	SetIntents(ctx context.Context, targetKey types.NamespacedName, transactionID string, configs, deleteConfigs []*config.Config, dryRun bool) (*config.ConfigStatusLastKnownGoodSchema, string, error)
	Confirm(ctx context.Context, targetKey types.NamespacedName, transactionID string) error
	Cancel(ctx context.Context, targetKey types.NamespacedName, transactionID string) error
}

func NewTargetHandler(client client.Client, targetStore storebackend.Storer[*Context]) TargetHandler {
	return &targetHandler{
		client:      client,
		targetStore: targetStore,
	}
}

type targetHandler struct {
	client      client.Client
	targetStore storebackend.Storer[*Context]
}

// GetTargetContext returns a invTarget and targetContext when the target is ready and the ctx is found
func (r *targetHandler) GetTargetContext(ctx context.Context, targetKey types.NamespacedName) (*invv1alpha1.Target, *Context, error) {
	target := &invv1alpha1.Target{}
	if err := r.client.Get(ctx, targetKey, target); err != nil {
		return nil, nil, &sdcerrors.RecoverableError{
			Message:      "target get failed",
			WrappedError: errors.Join(ErrLookup, err),
		}
	}
	if !target.IsDatastoreReady() {
		return nil, nil, &sdcerrors.RecoverableError{
			Message:      "target not ready",
			WrappedError: pkgerrors.Wrap(ErrLookup, string(config.ConditionReasonTargetNotReady)),
		}
	}
	tctx, err := r.targetStore.Get(ctx, storebackend.Key{NamespacedName: targetKey})
	if err != nil {
		return nil, nil, &sdcerrors.RecoverableError{
			Message:      "target not found",
			WrappedError: pkgerrors.Wrap(ErrLookup, string(config.ConditionReasonTargetNotFound)),
		}
	}
	return target, tctx, nil
}

func (r *targetHandler) SetIntent(ctx context.Context, targetKey types.NamespacedName, config *config.Config, dryRun bool) (*config.ConfigStatusLastKnownGoodSchema, string, error) {
	_, tctx, err := r.GetTargetContext(ctx, targetKey)
	if err != nil {
		return nil, "", err
	}
	schema := tctx.GetSchema()
	msg, err := tctx.SetIntent(ctx, storebackend.Key{NamespacedName: targetKey}, config, dryRun)
	return schema, msg, err
}

func (r *targetHandler) DeleteIntent(ctx context.Context, targetKey types.NamespacedName, config *config.Config, dryRun bool) (string, error) {
	_, tctx, err := r.GetTargetContext(ctx, targetKey)
	if err != nil {
		return "", err
	}
	return tctx.DeleteIntent(ctx, storebackend.Key{NamespacedName: targetKey}, config, dryRun)
}

func (r *targetHandler) GetData(ctx context.Context, targetKey types.NamespacedName) (*config.RunningConfig, error) {
	_, tctx, err := r.GetTargetContext(ctx, targetKey)
	if err != nil {
		return nil, err
	}
	return tctx.GetData(ctx, storebackend.Key{NamespacedName: targetKey})
}

func (r *targetHandler) RecoverIntents(ctx context.Context, targetKey types.NamespacedName, configs []*config.Config) (*config.ConfigStatusLastKnownGoodSchema, string, error) {
	_, tctx, err := r.GetTargetContext(ctx, targetKey)
	if err != nil {
		return nil, "", err
	}
	schema := tctx.GetSchema()
	msg, err := tctx.RecoverIntents(ctx, storebackend.Key{NamespacedName: targetKey}, configs)
	return schema, msg, err
}

func (r *targetHandler) SetIntents(ctx context.Context, targetKey types.NamespacedName, transactionID string, configs, deleteConfigs []*config.Config, dryRun bool) (*config.ConfigStatusLastKnownGoodSchema, string, error) {
	_, tctx, err := r.GetTargetContext(ctx, targetKey)
	if err != nil {
		return nil, "", err
	}
	schema := tctx.GetSchema()
	msg, err := tctx.SetIntents(ctx, storebackend.Key{NamespacedName: targetKey}, transactionID, configs, deleteConfigs, dryRun)
	return schema, msg, err
}

func (r *targetHandler) Confirm(ctx context.Context, targetKey types.NamespacedName, transactionID string) error {
	_, tctx, err := r.GetTargetContext(ctx, targetKey)
	if err != nil {
		return err
	}
	return tctx.Confirm(ctx, storebackend.Key{NamespacedName: targetKey}, transactionID)
}

func (r *targetHandler) Cancel(ctx context.Context, targetKey types.NamespacedName, transactionID string) error {
	_, tctx, err := r.GetTargetContext(ctx, targetKey)
	if err != nil {
		return err
	}
	return tctx.Cancel(ctx, storebackend.Key{NamespacedName: targetKey}, transactionID)
}
