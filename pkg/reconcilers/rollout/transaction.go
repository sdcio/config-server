/*
Copyright 2025 Nokia.

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

package rollout

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/henderiw/apiserver-store/pkg/storebackend"
	memstore "github.com/henderiw/apiserver-store/pkg/storebackend/memory"
	"github.com/henderiw/logger/log"
	"github.com/sdcio/config-server/apis/config"
	configv1alpha1 "github.com/sdcio/config-server/apis/config/v1alpha1"
	invv1alpha1 "github.com/sdcio/config-server/apis/inv/v1alpha1"
	dsclient "github.com/sdcio/config-server/pkg/sdc/dataserver/client"
	sdcpb "github.com/sdcio/sdc-protos/sdcpb"
	v1 "k8s.io/api/flowcontrol/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/sets"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

type transactionManager struct {
	newTargetUpdateConfigStore storebackend.Storer[storebackend.Storer[*config.Config]]
	newTargetDeleteConfigStore storebackend.Storer[storebackend.Storer[*config.Config]]
	//targetHandler              target.TargetHandler
	client                client.Client
	globalTimeout         time.Duration
	targetTimeout         time.Duration
	skipUnavailableTarget bool
	// derived
	targets      sets.Set[types.NamespacedName]
	targetStatus storebackend.Storer[invv1alpha1.RolloutTargetStatus]
}

func NewTransactionManager(
	newTargetUpdateConfigStore storebackend.Storer[storebackend.Storer[*config.Config]],
	newTargetDeleteConfigStore storebackend.Storer[storebackend.Storer[*config.Config]],
	client client.Client,
	globalTimeout, targetTimeout time.Duration,
	skipUnavailableTarget bool,
) *transactionManager {
	tm := &transactionManager{
		newTargetUpdateConfigStore: newTargetUpdateConfigStore,
		newTargetDeleteConfigStore: newTargetDeleteConfigStore,
		client:                     client,
		globalTimeout:              globalTimeout,
		targetTimeout:              targetTimeout,
		targetStatus:               memstore.NewStore[invv1alpha1.RolloutTargetStatus](),
		targets:                    sets.New[types.NamespacedName](),
		skipUnavailableTarget:      skipUnavailableTarget,
	}
	tm.calculateTargets(context.Background())
	return tm
}

func (r *transactionManager) calculateTargets(ctx context.Context) {
	log := log.FromContext(ctx)

	if err := r.newTargetUpdateConfigStore.List(ctx, func(ctx context.Context, k storebackend.Key, s storebackend.Storer[*config.Config]) {
		r.targets.Insert(k.NamespacedName)
	}); err != nil {
		log.Error("list failed", "err", err)
	}
	if err := r.newTargetDeleteConfigStore.List(ctx, func(ctx context.Context, k storebackend.Key, s storebackend.Storer[*config.Config]) {
		r.targets.Insert(k.NamespacedName)
	}); err != nil {
		log.Error("list failed", "err", err)
	}
}

func (r *transactionManager) TransactToAllTargets(ctx context.Context, transactionID string) (storebackend.Storer[invv1alpha1.RolloutTargetStatus], error) {
	// initialize the global transaction timeout
	ctx, globalCancel := context.WithTimeout(ctx, r.globalTimeout)
	defer globalCancel()

	log := log.FromContext(ctx).With("transactionID", transactionID)

	var wg sync.WaitGroup
	errChan := make(chan error, r.targets.Len())
	done := make(chan struct{})

	for _, targetKey := range r.targets.UnsortedList() {
		t := &configv1alpha1.Target{}
		if err := r.client.Get(ctx, targetKey, t); err != nil {
			// target unavailable -> we continue for now
			targetStatus := invv1alpha1.RolloutTargetStatus{Name: targetKey.String()}
			targetStatus.SetConditions(invv1alpha1.ConfigApplyUnavailable(fmt.Sprintf("target unavailable %s", err.Error())))
			_ = r.targetStatus.Update(ctx, storebackend.KeyFromNSN(targetKey), targetStatus)
			if r.skipUnavailableTarget {
				globalCancel()
				return r.targetStatus, fmt.Errorf("transaction aborted: target %s is unavailable", targetKey.String())
			}
			continue
		}
		if !t.IsReady() {
			// target unavailable -> we continue for now
			targetStatus := invv1alpha1.RolloutTargetStatus{Name: targetKey.String()}
			targetStatus.SetConditions(invv1alpha1.ConfigApplyUnavailable("target not ready"))
			_ = r.targetStatus.Update(ctx, storebackend.KeyFromNSN(targetKey), targetStatus)
			if r.skipUnavailableTarget {
				globalCancel()
				return r.targetStatus, fmt.Errorf("transaction aborted: target %s is not ready", targetKey.String())
			}
			continue
		}

		wg.Add(1)
		go func(targetKey types.NamespacedName) {
			targetStatus := invv1alpha1.RolloutTargetStatus{Name: targetKey.String()}
			defer wg.Done()
			select {
			case <-ctx.Done():
				log.Error("global timeout reached")
				// Timeout reached, exit goroutine
				targetStatus.SetConditions(invv1alpha1.ConfigApplyFailed("transaction timeout"))
				_ = r.targetStatus.Update(ctx, storebackend.KeyFromNSN(targetKey), targetStatus)
				return
			default:
				if err := r.applyConfigToTarget(ctx, targetKey, transactionID); err != nil {
					// Apply failed
					log.Error("apply failed", "target", targetKey.String(), "error", err.Error())
					targetStatus.SetConditions(invv1alpha1.ConfigApplyFailed(err.Error()))
					_ = r.targetStatus.Update(ctx, storebackend.KeyFromNSN(targetKey), targetStatus)
					errChan <- fmt.Errorf("target %s failed: %w", targetKey.String(), err)
					return
				}
				// Apply success
				log.Debug("apply success", "target", targetKey.String())
				targetStatus.SetConditions(invv1alpha1.ConfigApplyReady())
				_ = r.targetStatus.Update(ctx, storebackend.KeyFromNSN(targetKey), targetStatus)
				return
			}
		}(targetKey)
	}

	// Wait for all goroutines to finish
	go func() {
		wg.Wait()
		close(done)
	}()

	select {
	case <-done:
		close(errChan)
		if len(errChan) > 0 {
			// A transaction on a target failed
			r.RollbackTargets(ctx, transactionID)
			return r.targetStatus, fmt.Errorf("target transaction failed: %w", <-errChan)
		}
		// success, but commits could fails hence we return an error here
		return r.targetStatus, r.ConfirmTargets(ctx, transactionID)
	case <-ctx.Done():
		// Timeout
		r.RollbackTargets(ctx, transactionID)
		return r.targetStatus, fmt.Errorf("target transaction timed out")
	}
}

func (r *transactionManager) RollbackTargets(ctx context.Context, transactionID string) {
	var wg sync.WaitGroup
	log := log.FromContext(ctx)

	for _, targetKey := range r.targets.UnsortedList() {
		targetStatus, err := r.targetStatus.Get(ctx, storebackend.KeyFromNSN(targetKey))
		if err != nil {
			targetStatus := invv1alpha1.RolloutTargetStatus{Name: targetKey.String()}
			targetStatus.SetConditions(invv1alpha1.ConfigApplyUnknown())
			if err := r.targetStatus.Update(ctx, storebackend.KeyFromNSN(targetKey), targetStatus); err != nil {
				log.Error("target status update failed", "err", err)
			}
			continue
		}
		if targetStatus.GetCondition(invv1alpha1.ConditionTypeConfigApply).Reason != string(invv1alpha1.ConditionReasonUnavailable) &&
			v1.ConditionStatus(targetStatus.GetCondition(invv1alpha1.ConditionTypeConfigApply).Status) == v1.ConditionTrue {
			wg.Add(1)
			go func(targetKey types.NamespacedName) {
				defer wg.Done()
				if err := r.cancel(ctx, targetKey, transactionID); err != nil {
					targetStatus.SetConditions(invv1alpha1.ConfigCancelFailed(err.Error()))
				} else {
					targetStatus.SetConditions(invv1alpha1.ConfigCancelReady())
				}
				if err := r.targetStatus.Update(ctx, storebackend.KeyFromNSN(targetKey), targetStatus); err != nil {
					log.Error("target status update failed", "err", err)
				}
			}(targetKey)
		}
	}
	wg.Wait()
}

func (r *transactionManager) ConfirmTargets(ctx context.Context, transactionID string) error {
	var wg sync.WaitGroup
	var mu sync.Mutex
	var errs error
	log := log.FromContext(ctx)

	for _, targetKey := range r.targets.UnsortedList() {
		// We dont check the skip flag here since this is covered when we enter the loop
		targetStatus, err := r.targetStatus.Get(ctx, storebackend.KeyFromNSN(targetKey))
		if err != nil {
			targetStatus := invv1alpha1.RolloutTargetStatus{Name: targetKey.String()}
			targetStatus.SetConditions(invv1alpha1.ConfigApplyUnknown())
			if err := r.targetStatus.Update(ctx, storebackend.KeyFromNSN(targetKey), targetStatus); err != nil {
				log.Error("target status update failed", "err", err)
			}
			continue
		}
		if targetStatus.GetCondition(invv1alpha1.ConditionTypeConfigApply).Reason != string(invv1alpha1.ConditionReasonUnavailable) &&
			v1.ConditionStatus(targetStatus.GetCondition(invv1alpha1.ConditionTypeConfigApply).Status) == v1.ConditionTrue {
			wg.Add(1)
			go func(targetKey types.NamespacedName) {
				defer wg.Done()
				if err := r.confirm(ctx, targetKey, transactionID); err != nil {
					targetStatus.SetConditions(invv1alpha1.ConfigConfirmFailed(err.Error()))
					mu.Lock()
					errs = errors.Join(errs, err)
					mu.Unlock()
				} else {
					targetStatus.SetConditions(invv1alpha1.ConfigConfirmReady())
				}
				if err := r.targetStatus.Update(ctx, storebackend.KeyFromNSN(targetKey), targetStatus); err != nil {
					log.Error("target status update failed", "err", err)
				}
			}(targetKey)
		}
	}
	wg.Wait()
	return errs
}

func (r *transactionManager) cancel(ctx context.Context, targetKey types.NamespacedName, transactionID string) error {
	log := log.FromContext(ctx).With("target", targetKey.String(), "transactionID", transactionID)
	log.Info("transaction rollback")

	cancelCtx, cancel := context.WithTimeout(ctx, r.targetTimeout)
	defer cancel()

	done := make(chan error, 1)

	go func() {
		cfg := &dsclient.Config{
			Address:  dsclient.GetDataServerAddress(),
			Insecure: true,
		}

		done <- dsclient.OneShot(ctx, cfg, func(ctx context.Context, c sdcpb.DataServerClient) error {
			_, err := c.TransactionCancel(ctx, &sdcpb.TransactionCancelRequest{
				TransactionId: transactionID,
				DatastoreName: storebackend.KeyFromNSN(targetKey).String(),
			})
			if err != nil {
				return err
			}
			return nil
		})
		close(done)
	}()

	select {
	case err := <-done:
		if err != nil {
			log.Error("Transaction cancel failed", "error", err)
			return err
		}
		log.Debug("Transaction cancel successfully")
		return nil
	case <-cancelCtx.Done():
		log.Warn("Context timeout while cancelling transaction")
		return fmt.Errorf("transaction timeout reached before cancelling transaction to %s", targetKey)
	}
}

func (r *transactionManager) confirm(ctx context.Context, targetKey types.NamespacedName, transactionID string) error {
	log := log.FromContext(ctx).With("target", targetKey.String(), "transactionID", transactionID)
	log.Info("transaction confirm")

	cancelCtx, cancel := context.WithTimeout(ctx, r.targetTimeout)
	defer cancel()

	done := make(chan error, 1)

	go func() {
		cfg := &dsclient.Config{
			Address:  dsclient.GetDataServerAddress(),
			Insecure: true,
		}

		done <- dsclient.OneShot(ctx, cfg, func(ctx context.Context, c sdcpb.DataServerClient) error {
			_, err := c.TransactionConfirm(ctx, &sdcpb.TransactionConfirmRequest{
				TransactionId: transactionID,
				DatastoreName: storebackend.KeyFromNSN(targetKey).String(),
			})
			if err != nil {
				return err
			}
			return nil
		})
		close(done)
	}()

	select {
	case err := <-done:
		if err != nil {
			log.Error("Transaction confirm failed", "error", err)
			return err
		}
		log.Debug("Transaction confirm successfully")
		return nil
	case <-cancelCtx.Done():
		log.Warn("Context timeout while confirming transaction")
		return fmt.Errorf("transaction timeout reached before confirming transaction to %s", targetKey)
	}
}

func (r *transactionManager) applyConfigToTarget(ctx context.Context, targetKey types.NamespacedName, transactionID string) error {
	log := log.FromContext(ctx).With("target", targetKey.String(), "transactionID", transactionID)

	configUpdates := map[string]*config.Config{}
	configDeletes := map[string]*config.Config{}
	storeConfigUpdates, err := r.newTargetUpdateConfigStore.Get(ctx, storebackend.KeyFromNSN(targetKey))
	if err == nil {
		if err := storeConfigUpdates.List(ctx, func(ctx context.Context, k storebackend.Key, config *config.Config) {
			configUpdates[config.GetNamespacedName().String()] = config
		}); err != nil {
			log.Error("cannot list config", "err", err)
		}
	}
	storeConfigDeletes, err := r.newTargetDeleteConfigStore.Get(ctx, storebackend.KeyFromNSN(targetKey))
	if err == nil {
		if err := storeConfigDeletes.List(ctx, func(ctx context.Context, k storebackend.Key, config *config.Config) {
			configDeletes[config.GetNamespacedName().String()] = config
		}); err != nil {
			log.Error("cannot list config", "err", err)
		}
	}

	cancelCtx, cancel := context.WithTimeout(ctx, r.targetTimeout)
	defer cancel()

	done := make(chan error, 1)

	//deviations := map[string]*config.Deviation{}

	go func() {
		cfg := &dsclient.Config{
			Address:  dsclient.GetDataServerAddress(),
			Insecure: true,
		}

		// ToBeUpdated
		done <- dsclient.OneShot(ctx, cfg, func(ctx context.Context, c sdcpb.DataServerClient) error {
			_, err := c.TransactionSet(ctx, &sdcpb.TransactionSetRequest{
				TransactionId: transactionID,
				DatastoreName: storebackend.KeyFromNSN(targetKey).String(),
			})
			if err != nil {
				return err
			}
			return nil
		})
		close(done)

	}()

	select {
	case err := <-done:
		if err != nil {
			log.Error("Transaction apply failed", "error", err)
			return err
		}
		log.Debug("Transaction applied successfully")
		return nil
	case <-cancelCtx.Done():
		log.Warn("Context timeout while applying transaction")
		return fmt.Errorf("transaction timeout reached before applying config to %s", targetKey)
	}
}
