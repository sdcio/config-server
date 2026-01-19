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

/*

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/henderiw/apiserver-store/pkg/storebackend"
	memstore "github.com/henderiw/apiserver-store/pkg/storebackend/memory"
	"github.com/henderiw/logger/log"
	condv1alpha1 "github.com/sdcio/config-server/apis/condition/v1alpha1"
	"github.com/sdcio/config-server/apis/config"
	invv1alpha1 "github.com/sdcio/config-server/apis/inv/v1alpha1"
	"github.com/sdcio/config-server/pkg/target"
	"github.com/stretchr/testify/assert"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/utils/ptr"
)

const (
	namespace = "default"
)

func TestTransactionManager(t *testing.T) {
	cases := map[string]struct {
		targets          map[string]*target.MockContext            // Mocked targets
		updateCount      int                                       // Number of config updates per target
		deleteCount      int                                       // Number of config deletes per target
		expectError      bool                                      // Whether the transaction should fail
		expectedStatuses map[string]condv1alpha1.ConditionedStatus // Expected final conditions per target
	}{
		"Success": {
			targets: map[string]*target.MockContext{
				"target1": {Ready: true},
				"target2": {Ready: true},
			},
			updateCount: 2,
			deleteCount: 2,
			expectError: false,
			expectedStatuses: map[string]condv1alpha1.ConditionedStatus{
				"target1": {Conditions: []condv1alpha1.Condition{invv1alpha1.ConfigApplyReady(), invv1alpha1.ConfigConfirmReady()}},
				"target2": {Conditions: []condv1alpha1.Condition{invv1alpha1.ConfigApplyReady(), invv1alpha1.ConfigConfirmReady()}},
			},
		},
		"TargetFailure": {
			targets: map[string]*target.MockContext{
				"target1": {Ready: true},
				"target2": {Ready: true, SetIntentError: assert.AnError},
			},
			updateCount: 2,
			deleteCount: 2,
			expectError: true,
			expectedStatuses: map[string]condv1alpha1.ConditionedStatus{
				"target1": {Conditions: []condv1alpha1.Condition{invv1alpha1.ConfigApplyReady(), invv1alpha1.ConfigCancelReady()}},
				"target2": {Conditions: []condv1alpha1.Condition{invv1alpha1.ConfigApplyFailed("dummy")}},
			},
		},
		"TargetTimeout": {
			targets: map[string]*target.MockContext{
				"target1": {Ready: true, Busy: ptr.To(10 * time.Minute)},
				"target2": {Ready: true},
			},
			updateCount: 2,
			deleteCount: 2,
			expectError: true,
			expectedStatuses: map[string]condv1alpha1.ConditionedStatus{
				"target1": {Conditions: []condv1alpha1.Condition{invv1alpha1.ConfigApplyFailed("dummy")}},
				"target2": {Conditions: []condv1alpha1.Condition{invv1alpha1.ConfigApplyReady(), invv1alpha1.ConfigCancelReady()}},
			},
		},
		"TargetConfirmError": {
			targets: map[string]*target.MockContext{
				"target1": {Ready: true, ConfirmError: assert.AnError},
				"target2": {Ready: true},
			},
			updateCount: 2,
			deleteCount: 2,
			expectError: true,
			expectedStatuses: map[string]condv1alpha1.ConditionedStatus{
				"target1": {Conditions: []condv1alpha1.Condition{invv1alpha1.ConfigApplyReady(), invv1alpha1.ConfigConfirmFailed("dummy")}},
				"target2": {Conditions: []condv1alpha1.Condition{invv1alpha1.ConfigApplyReady(), invv1alpha1.ConfigConfirmReady()}},
			},
		},
	}

	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			tm := getTransctionManager(tc.targets, tc.updateCount, tc.deleteCount)
			status, err := tm.TransactToAllTargets(context.Background(), "txn-123")

			if tc.expectError {
				assert.Error(t, err, "Expected transaction to fail")
			} else {
				assert.NoError(t, err, "Transaction should succeed")
			}

			for targetName := range tc.targets {
				targetKey := storebackend.KeyFromNSN(types.NamespacedName{Name: targetName, Namespace: namespace})
				targetStatus, _ := status.Get(context.Background(), targetKey)
				fmt.Println("status", name, targetName, targetStatus.Name)
				for _, condition := range targetStatus.Conditions {
					fmt.Println("targetStatus", targetStatus.Name, condition.String())
				}
			}

			if tc.expectError {
				assert.Error(t, err, "Expected transaction to fail")
			} else {
				assert.NoError(t, err, "Transaction should succeed")
			}

			for name, expectedConditions := range tc.expectedStatuses {
				targetKey := storebackend.KeyFromNSN(types.NamespacedName{Name: name, Namespace: namespace})
				targetStatus, _ := status.Get(context.Background(), targetKey)

				// 🔍 **Check each expected condition**
				for _, expectedCond := range expectedConditions.Conditions {
					actualCond := targetStatus.GetCondition(condv1alpha1.ConditionType(expectedCond.Type))
					assert.Equal(t, expectedCond.Status, actualCond.Status, "Condition status should be correct for "+name)
				}

				actualConditions := targetStatus.Conditions
				expectedConditionTypes := make(map[condv1alpha1.ConditionType]bool)

				for _, expectedCond := range expectedConditions.Conditions {
					expectedConditionTypes[condv1alpha1.ConditionType(expectedCond.Type)] = true
				}
				for _, actualCond := range actualConditions {
					if !expectedConditionTypes[condv1alpha1.ConditionType(actualCond.Type)] {
						t.Errorf("Unexpected condition %s found in target %s", actualCond.Type, name)
					}
				}

			}
		})
	}
}

func getTransctionManager(targets map[string]*target.MockContext, updates, deletes int) *transactionManager {
	ctx := context.Background()
	log := log.FromContext(ctx)
	targetStore := memstore.NewStore[*target.MockContext]()
	for name, mctx := range targets {
		targetKey := storebackend.KeyFromNSN(types.NamespacedName{Name: name, Namespace: namespace})
		if err := targetStore.Create(context.Background(), targetKey, mctx); err != nil {
			log.Error("cannot create target", "err", err)
		}
	}
	//mockHandler := target.NewMockTargetHandler(targetStore)
	updateStore := memstore.NewStore[storebackend.Storer[*config.Config]]()
	deleteStore := memstore.NewStore[storebackend.Storer[*config.Config]]()

	for name := range targets {
		targetKey := storebackend.KeyFromNSN(types.NamespacedName{Name: name, Namespace: namespace})
		updateConfigStore := memstore.NewStore[*config.Config]()
		deleteConfigStore := memstore.NewStore[*config.Config]()

		for i := 0; i < updates; i++ {
			if err := updateConfigStore.Create(context.Background(), storebackend.KeyFromNSN(types.NamespacedName{Name: fmt.Sprintf("update-%d", i), Namespace: namespace}), &config.Config{}); err != nil {
				log.Error("cannot update store", "err", err)
			}
		}
		for i := 0; i < deletes; i++ {
			if err := deleteConfigStore.Create(context.Background(), storebackend.KeyFromNSN(types.NamespacedName{Name: fmt.Sprintf("delete-%d", i), Namespace: namespace}), &config.Config{}); err != nil {
				log.Error("cannot delete store", "err", err)
			}
		}
		if err := updateStore.Create(context.Background(), targetKey, updateConfigStore); err != nil {
			log.Error("cannot create store", "err", err)
		}

		if err := deleteStore.Create(context.Background(), targetKey, deleteConfigStore); err != nil {
			log.Error("cannot delete store", "err", err)
		}
	}

	return NewTransactionManager(updateStore, deleteStore, nil, 5*time.Second, 2*time.Second, true)
}
*/