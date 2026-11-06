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

package eventhandler

import (
	"context"
	"testing"

	configv1alpha1 "github.com/sdcio/config-server/apis/config/v1alpha1"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/util/workqueue"
	"sigs.k8s.io/controller-runtime/pkg/event"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
)

func newQueue() workqueue.TypedRateLimitingInterface[reconcile.Request] {
	return workqueue.NewTypedRateLimitingQueue(workqueue.DefaultTypedControllerRateLimiter[reconcile.Request]())
}

func TestConfigForTargetEnqueuesTargetFromLabels(t *testing.T) {
	q := newQueue()
	defer q.ShutDown()

	cfg := &configv1alpha1.Config{ObjectMeta: metav1.ObjectMeta{
		Name:      "dunder.apn",
		Namespace: "upf843",
		Labels: map[string]string{
			"config.sdcio.dev/targetName":      "upf843",
			"config.sdcio.dev/targetNamespace": "upf843",
		},
	}}

	h := &ConfigForTargetEventHandler{ControllerName: "test"}
	h.Create(context.Background(), event.CreateEvent{Object: cfg}, q)

	require.Equal(t, 1, q.Len())
	req, _ := q.Get()
	// enqueued even though no such Target exists: the reconciler reports it
	assert.Equal(t, types.NamespacedName{Namespace: "upf843", Name: "upf843"}, req.NamespacedName)
}

// A Config without target labels used to dereference a nil key and panic.
func TestConfigForTargetWithoutLabelsDoesNotPanic(t *testing.T) {
	q := newQueue()
	defer q.ShutDown()

	cfg := &configv1alpha1.Config{ObjectMeta: metav1.ObjectMeta{Name: "nolabels", Namespace: "default"}}

	h := &ConfigForTargetEventHandler{ControllerName: "test"}
	assert.NotPanics(t, func() {
		h.Create(context.Background(), event.CreateEvent{Object: cfg}, q)
	})
	assert.Equal(t, 0, q.Len())
}
