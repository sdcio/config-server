/*
Copyright 2026 Nokia.

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

package targetdatastore

import (
	"context"
	"testing"

	condv1alpha1 "github.com/sdcio/config-server/apis/condition/v1alpha1"
	configv1alpha1 "github.com/sdcio/config-server/apis/config/v1alpha1"
	targetmanager "github.com/sdcio/config-server/pkg/sdc/target/manager"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
)

func TestReadyFromRuntime(t *testing.T) {
	cases := map[string]struct {
		st          targetmanager.RuntimeStatus
		ready       bool
		reason      condv1alpha1.ConditionReason
		msgContains string
		requeue     bool
		datastoreUp bool
	}{
		"running": {
			st:    targetmanager.RuntimeStatus{Phase: targetmanager.PhaseRunning},
			ready: true, reason: condv1alpha1.ConditionReasonReady, datastoreUp: true,
		},
		"running with recovery warnings": {
			st:    targetmanager.RuntimeStatus{Phase: targetmanager.PhaseRunning, RecoveryMsg: "1 warning"},
			ready: true, reason: condv1alpha1.ConditionReasonReady, msgContains: "1 warning", datastoreUp: true,
		},
		"recovering": {
			st:     targetmanager.RuntimeStatus{Phase: targetmanager.PhaseRecovering, LastError: "not connected"},
			reason: configv1alpha1.TargetReasonRecovering, msgContains: "not connected", requeue: true, datastoreUp: true,
		},
		"recovery failed is terminal": {
			st:     targetmanager.RuntimeStatus{Phase: targetmanager.PhaseRecoveryFailed, LastError: "invalid value"},
			reason: configv1alpha1.TargetReasonRecoveryFailed, msgContains: configv1alpha1.AnnotationRecoveryRetry,
			requeue: false, datastoreUp: true,
		},
		"degraded": {
			st:     targetmanager.RuntimeStatus{Phase: targetmanager.PhaseDegraded, LastError: "create datastore"},
			reason: configv1alpha1.TargetReasonDatastoreFailed, msgContains: "create datastore", requeue: true,
		},
		"ensuring datastore": {
			st:     targetmanager.RuntimeStatus{Phase: targetmanager.PhaseEnsuringDatastore},
			reason: configv1alpha1.TargetReasonDatastoreNotReady, requeue: true,
		},
		"waiting for dataserver": {
			st:     targetmanager.RuntimeStatus{Phase: targetmanager.PhaseWaitingForDS},
			reason: configv1alpha1.TargetReasonDataServerNotReady, requeue: true,
		},
		"pending": {
			st:     targetmanager.RuntimeStatus{Phase: targetmanager.PhasePending},
			reason: configv1alpha1.TargetReasonInitializing, requeue: true,
		},
		"unknown phase": {
			st:     targetmanager.RuntimeStatus{Phase: "Bogus"},
			reason: condv1alpha1.ConditionReasonUnknown, msgContains: "Bogus", requeue: true,
		},
	}

	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			cond, requeue, up := readyFromRuntime(tc.st)

			assert.Equal(t, string(condv1alpha1.ConditionTypeReady), cond.Type)
			assert.Equal(t, tc.ready, cond.IsTrue())
			assert.Equal(t, string(tc.reason), cond.Reason)
			if tc.msgContains != "" {
				assert.Contains(t, cond.Message, tc.msgContains)
			}
			assert.Equal(t, tc.requeue, requeue > 0)
			assert.Equal(t, tc.datastoreUp, up)
		})
	}
}

// deleteDeviation deletes the Deviation of the Target by its canonical
// name, and leaves a Config Deviation that has the Target name alone.
func TestDeleteDeviation(t *testing.T) {
	const namespace = "default"
	// a Target named like the Deviation of Config "cfg1"
	target := &configv1alpha1.Target{ObjectMeta: metav1.ObjectMeta{Name: "config-cfg1", Namespace: namespace}}
	targetDeviation := configv1alpha1.DeviationName(configv1alpha1.DeviationType_TARGET, target.Name)
	configDeviation := configv1alpha1.DeviationName(configv1alpha1.DeviationType_CONFIG, "cfg1")

	scheme := runtime.NewScheme()
	require.NoError(t, configv1alpha1.AddToScheme(scheme))
	c := fake.NewClientBuilder().WithScheme(scheme).WithObjects(
		&configv1alpha1.Deviation{ObjectMeta: metav1.ObjectMeta{Name: targetDeviation, Namespace: namespace}},
		&configv1alpha1.Deviation{ObjectMeta: metav1.ObjectMeta{Name: configDeviation, Namespace: namespace}},
	).Build()
	r := &reconciler{client: c}

	require.NoError(t, r.deleteDeviation(context.Background(), target))

	err := c.Get(context.Background(), types.NamespacedName{Namespace: namespace, Name: targetDeviation}, &configv1alpha1.Deviation{})
	assert.True(t, apierrors.IsNotFound(err), "target deviation %s must be deleted, got err: %v", targetDeviation, err)
	assert.NoError(t, c.Get(context.Background(), types.NamespacedName{Namespace: namespace, Name: configDeviation}, &configv1alpha1.Deviation{}),
		"config deviation %s must be kept", configDeviation)

	// the Deviation is gone: deleting it again is not an error
	assert.NoError(t, r.deleteDeviation(context.Background(), target))
}
