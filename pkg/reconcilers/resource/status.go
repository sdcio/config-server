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

package resource

import (
	"context"

	"github.com/henderiw/logger/log"
	condv1alpha1 "github.com/sdcio/config-server/apis/condition/v1alpha1"
	"github.com/sdcio/config-server/pkg/reconcilers"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/tools/events"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// StatusWriter is the single writer of the status fields one field manager
// owns on a kind, the way APIFinalizer is for a finalizer. Every status write
// of that manager goes through Write.
//
// It applies with SSA: an apply is the manager's complete desired state for
// the fields it owns, so a field it applied before and leaves out now is
// removed. Give each manager only the fields its input determines; then an
// apply never has to carry a value it has no fresh source for.
type StatusWriter struct {
	client       client.Client
	fieldManager string
	recorder     events.EventRecorder
	force        bool
}

type StatusWriterOption func(*StatusWriter)

// WithEventRecorder records an event when an owned condition changes status
// or reason, after the apply succeeded. Message-only changes record nothing.
func WithEventRecorder(recorder events.EventRecorder) StatusWriterOption {
	return func(w *StatusWriter) { w.recorder = recorder }
}

// WithForceOwnership takes the fields over when another field manager owns
// them, e.g. a retired manager the fields were moved away from.
func WithForceOwnership() StatusWriterOption {
	return func(w *StatusWriter) { w.force = true }
}

func NewStatusWriter(c client.Client, fieldManager string, opts ...StatusWriterOption) *StatusWriter {
	w := &StatusWriter{client: c, fieldManager: fieldManager}
	for _, opt := range opts {
		opt(w)
	}
	return w
}

// FieldManager is the name the writer applies with.
func (w *StatusWriter) FieldManager() string { return w.fieldManager }

// ConditionedObject is an object with condv1alpha1 conditions.
type ConditionedObject interface {
	client.Object
	GetCondition(t condv1alpha1.ConditionType) condv1alpha1.Condition
}

// StatusApplyFn builds the apply configuration with the owned status fields.
// conditions are the owned conditions, lastTransitionTime resolved.
// resourceVersion is "" for an unconditional apply; otherwise the apply
// configuration must carry it (WithResourceVersion).
type StatusApplyFn func(conditions []condv1alpha1.Condition, resourceVersion string) runtime.ApplyConfiguration

// StatusUpdate is the desired state of the fields a manager owns.
type StatusUpdate struct {
	// Conditions owned by this manager, as desired.
	Conditions []condv1alpha1.Condition
	// Conditional is set when the update is derived from obj itself (the
	// cache) rather than from fresh input. The apply carries obj's
	// resourceVersion; a 409 means obj changed since it was read, that
	// change is already queued as an event, and the next reconcile derives
	// the update again, so the conflict is dropped.
	Conditional bool
	// Build returns the apply configuration.
	Build StatusApplyFn
}

// Write always applies u: the apiserver computes the delta, and a status
// update that changes nothing is dropped there (no resourceVersion, no watch
// event).
//
// obj is the object as last read. It is only used to keep lastTransitionTime
// when a condition's status does not flip (a new timestamp on every apply
// would make every apply a change), and to detect transitions for events.
func (w *StatusWriter) Write(ctx context.Context, obj ConditionedObject, u StatusUpdate) error {
	log := log.FromContext(ctx)

	conds := make([]condv1alpha1.Condition, 0, len(u.Conditions))
	var transitions []condv1alpha1.Condition
	for _, c := range u.Conditions {
		// GetCondition returns {Type, False} without a reason when absent
		old := obj.GetCondition(condv1alpha1.ConditionType(c.Type))
		if old.Status == c.Status && !old.LastTransitionTime.IsZero() {
			c.LastTransitionTime = old.LastTransitionTime
		}
		if old.Status != c.Status || old.Reason != c.Reason {
			transitions = append(transitions, c)
		}
		conds = append(conds, c)
	}

	rv := ""
	if u.Conditional {
		rv = obj.GetResourceVersion()
	}
	opts := client.ApplyOptions{FieldManager: w.fieldManager}
	if w.force {
		opts.Force = ptr.To(true)
	}
	log.Debug("apply status", "object", client.ObjectKeyFromObject(obj).String(),
		"fieldManager", w.fieldManager, "conditional", u.Conditional)

	err := w.client.Status().Apply(ctx, u.Build(conds, rv), &client.SubResourceApplyOptions{ApplyOptions: opts})
	if u.Conditional && apierrors.IsConflict(err) {
		log.Debug("status not written, object changed since it was read",
			"object", client.ObjectKeyFromObject(obj).String(), "fieldManager", w.fieldManager)
		return nil
	}
	if err != nil {
		return err
	}

	if w.recorder != nil {
		for _, c := range transitions {
			eventType := corev1.EventTypeNormal
			if !c.IsTrue() {
				eventType = corev1.EventTypeWarning
			}
			reconcilers.RecordEvent(w.recorder, obj, eventType, c.Reason, c.Message)
		}
	}
	return nil
}