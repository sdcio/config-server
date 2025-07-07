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
	"sync"
	"time"

	"github.com/henderiw/apiserver-store/pkg/storebackend"
	"github.com/henderiw/logger/log"
	pkgerrors "github.com/pkg/errors"
	configapi "github.com/sdcio/config-server/apis/config"
	invv1alpha1 "github.com/sdcio/config-server/apis/inv/v1alpha1"
	sdcerrors "github.com/sdcio/config-server/pkg/errors"
	"k8s.io/apimachinery/pkg/types"
)

type MockContext struct {
	Ready              bool
	Busy               *time.Duration
	Message            string
	SetIntentError     error
	DeleteIntentError  error
	RecoverIntentError error
	CancelError        error
	ConfirmError       error
}

func NewMockTargetHandler(targetStore storebackend.Storer[*MockContext]) TargetHandler {
	return &mockTargetHandler{
		targetStore: targetStore,
	}
}

type mockTargetHandler struct {
	targetStore storebackend.Storer[*MockContext]
	mu          sync.RWMutex
}

func (r *mockTargetHandler) getMockContext(ctx context.Context, targetKey types.NamespacedName) (*MockContext, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	mtctx, err := r.targetStore.Get(ctx, storebackend.Key{NamespacedName: targetKey})
	if err != nil {
		return nil, &sdcerrors.RecoverableError{
			Message:      "target not found",
			WrappedError: pkgerrors.Wrap(ErrLookup, string(configapi.ConditionReasonTargetNotFound)),
		}
	}
	if !mtctx.Ready {
		return mtctx, &sdcerrors.RecoverableError{
			Message:      "target not ready",
			WrappedError: pkgerrors.Wrap(ErrLookup, string(configapi.ConditionReasonTargetNotReady)),
		}
	}
	return mtctx, nil
}

func (r *mockTargetHandler) GetTargetContext(ctx context.Context, targetKey types.NamespacedName) (*invv1alpha1.Target, *Context, error) {
	if _, err := r.getMockContext(ctx, targetKey); err != nil {
		return nil, nil, err
	}
	return nil, nil, nil
}

func (r *mockTargetHandler) SetIntent(ctx context.Context, targetKey types.NamespacedName, config *configapi.Config, deviation *configapi.Deviation, dryRun bool) (*configapi.ConfigStatusLastKnownGoodSchema, string, error) {
	log := log.FromContext(ctx).With("target", targetKey.String(), "intent", getGVKNSN(config))
	log.Info("SetIntent")
	mctx, err := r.getMockContext(ctx, targetKey)
	if err != nil {
		return nil, mctx.Message, err
	}
	if mctx.Busy != nil {
		log.Info("setIntents", "sleep", mctx.Busy.String())
		time.Sleep(*mctx.Busy)
		return nil, mctx.Message, err
	}
	return nil, mctx.Message, mctx.SetIntentError
}

func (r *mockTargetHandler) DeleteIntent(ctx context.Context, targetKey types.NamespacedName, config *configapi.Config, dryRun bool) (string, error) {
	log := log.FromContext(ctx).With("target", targetKey.String(), "intent", getGVKNSN(config))
	log.Info("DeleteIntent")
	mctx, err := r.getMockContext(ctx, targetKey)
	if err != nil {
		return mctx.Message, err
	}
	if mctx.Busy != nil {
		log.Info("setIntents", "sleep", mctx.Busy.String())
		time.Sleep(*mctx.Busy)
		return mctx.Message, err
	}
	return mctx.Message, mctx.DeleteIntentError
}

func (r *mockTargetHandler) GetData(ctx context.Context, targetKey types.NamespacedName) (*configapi.RunningConfig, error) {
	return nil, &sdcerrors.RecoverableError{
		Message: "GetData not implemented",
	}
}

func (r *mockTargetHandler) GetBlameConfig(ctx context.Context, targetKey types.NamespacedName) (*configapi.ConfigBlame, error) {
	return nil, &sdcerrors.RecoverableError{
		Message: "GetData not implemented",
	}
}

func (r *mockTargetHandler) RecoverIntents(ctx context.Context, targetKey types.NamespacedName, configs []*configapi.Config, deviations []*configapi.Deviation) (*configapi.ConfigStatusLastKnownGoodSchema, string, error) {
	log := log.FromContext(ctx).With("target", targetKey.String())
	log.Info("RecoverIntents")
	mctx, err := r.getMockContext(ctx, targetKey)
	if err != nil {
		return nil, mctx.Message, err
	}
	if mctx.Busy != nil {
		log.Info("setIntents", "sleep", mctx.Busy.String())
		time.Sleep(*mctx.Busy)
		return nil, mctx.Message, err
	}
	return nil, mctx.Message, mctx.RecoverIntentError
}

func (r *mockTargetHandler) SetIntents(ctx context.Context, targetKey types.NamespacedName, transactionID string, configs, deleteConfigs []*configapi.Config, dryRun bool) (*configapi.ConfigStatusLastKnownGoodSchema, string, error) {
	log := log.FromContext(ctx).With("target", targetKey.String(), "transactionID", transactionID)
	log.Info("setIntents")
	mctx, err := r.getMockContext(ctx, targetKey)
	if err != nil {
		return nil, mctx.Message, err
	}
	if mctx.Busy != nil {
		log.Info("setIntents", "sleep", mctx.Busy.String())
		time.Sleep(*mctx.Busy)
		return nil, mctx.Message, err
	}
	return nil, mctx.Message, mctx.SetIntentError
}

func (r *mockTargetHandler) Cancel(ctx context.Context, targetKey types.NamespacedName, transactionID string) error {
	log := log.FromContext(ctx).With("target", targetKey.String(), "transactionID", transactionID)
	log.Info("cancel")
	mctx, err := r.getMockContext(ctx, targetKey)
	if err != nil {
		return err
	}
	return mctx.CancelError
}

func (r *mockTargetHandler) Confirm(ctx context.Context, targetKey types.NamespacedName, transactionID string) error {
	log := log.FromContext(ctx).With("target", targetKey.String(), "transactionID", transactionID)
	log.Info("confirm")
	mctx, err := r.getMockContext(ctx, targetKey)
	if err != nil {
		return err
	}

	return mctx.ConfirmError
}
