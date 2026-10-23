package eventhandler

import (
	"context"
	"testing"

	"github.com/sdcio/config-server/apis/config"
	configv1alpha1 "github.com/sdcio/config-server/apis/config/v1alpha1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/util/workqueue"
	"sigs.k8s.io/controller-runtime/pkg/event"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
)

func newDeviation(path string, resourceVersion string) *configv1alpha1.Deviation {
	return &configv1alpha1.Deviation{
		ObjectMeta: metav1.ObjectMeta{
			Name:            "target-srl1",
			Namespace:       "default",
			ResourceVersion: resourceVersion,
			Labels: map[string]string{
				config.TargetNamespaceKey: "default",
				config.TargetNameKey:      "srl1",
			},
		},
		Spec: configv1alpha1.DeviationSpec{
			Deviations: []configv1alpha1.ConfigDeviation{{Path: path}},
		},
	}
}

func newTestQueue() workqueue.TypedRateLimitingInterface[reconcile.Request] {
	return workqueue.NewTypedRateLimitingQueue(workqueue.DefaultTypedControllerRateLimiter[reconcile.Request]())
}

func TestDeviationForTarget_UpdateFiresOnSpecChange(t *testing.T) {
	var woken []types.NamespacedName
	h := &DeviationForTargetEventHandler{
		ControllerName: "test",
		OnSpecChange:   func(_ context.Context, key types.NamespacedName) { woken = append(woken, key) },
	}
	q := newTestQueue()

	h.Update(context.Background(), event.UpdateEvent{
		ObjectOld: newDeviation("/a", "1"),
		ObjectNew: newDeviation("/b", "2"),
	}, q)

	if q.Len() != 1 {
		t.Fatalf("expected the target to be enqueued, queue len=%d", q.Len())
	}
	want := types.NamespacedName{Namespace: "default", Name: "srl1"}
	if len(woken) != 1 || woken[0] != want {
		t.Fatalf("expected one wake for %v, got %v", want, woken)
	}
}

func TestDeviationForTarget_StatusOnlyUpdateDoesNotFire(t *testing.T) {
	called := false
	h := &DeviationForTargetEventHandler{
		ControllerName: "test",
		OnSpecChange:   func(context.Context, types.NamespacedName) { called = true },
	}
	q := newTestQueue()

	// same spec, newer resourceVersion: the watcher re-applying status
	h.Update(context.Background(), event.UpdateEvent{
		ObjectOld: newDeviation("/a", "1"),
		ObjectNew: newDeviation("/a", "2"),
	}, q)

	if q.Len() != 0 || called {
		t.Fatalf("status-only update must not fire: queue len=%d called=%t", q.Len(), called)
	}
}
