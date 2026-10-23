package eventhandler

import (
	"context"
	"testing"

	configv1alpha1 "github.com/sdcio/config-server/apis/config/v1alpha1"
	"github.com/stretchr/testify/assert"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
)

type recordingAdder struct{ items []reconcile.Request }

func (a *recordingAdder) Add(r reconcile.Request) { a.items = append(a.items, r) }

// Objects without target labels cannot be mapped to a target; handlers must skip them.
func TestConfigForTargetSkipsUnresolvableTarget(t *testing.T) {
	q := &recordingAdder{}
	h := &ConfigForTargetEventHandler{ControllerName: "test"}
	cfg := &configv1alpha1.Config{ObjectMeta: metav1.ObjectMeta{Name: "c", Namespace: "ns"}}

	assert.NotPanics(t, func() { h.add(context.Background(), cfg, q) })
	assert.Empty(t, q.items)
}

func TestDeviationForTargetSkipsUnresolvableTarget(t *testing.T) {
	q := &recordingAdder{}
	h := &DeviationForTargetEventHandler{ControllerName: "test"}
	dev := &configv1alpha1.Deviation{ObjectMeta: metav1.ObjectMeta{Name: "d", Namespace: "ns"}}

	assert.NotPanics(t, func() { h.add(context.Background(), dev, q) })
	assert.Empty(t, q.items)
}

// DeviationForConfig never resolves a target key; it must still tolerate unowned deviations.
func TestDeviationForConfigIgnoresUnownedDeviation(t *testing.T) {
	q := &recordingAdder{}
	h := &DeviationForConfigEventHandler{ControllerName: "test"}
	dev := &configv1alpha1.Deviation{ObjectMeta: metav1.ObjectMeta{Name: "d", Namespace: "ns"}}

	assert.NotPanics(t, func() { h.add(context.Background(), dev, q) })
	assert.Empty(t, q.items)
}
