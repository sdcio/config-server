
package eventhandler

import (
	"context"

	"github.com/henderiw/logger/log"
	"github.com/sdcio/config-server/apis/config"
	"github.com/sdcio/config-server/pkg/reconcilers/ctrlconfig"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/util/workqueue"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/event"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
)

// TargetLabelEventHandler maps an event on any target-bound object (Config,
// SensitiveConfig, Deviation, ...) onto a request for the Target named in its
// target label. The request is enqueued even when that Target does not exist,
// so the reconciler can report it (sdcio/config-server#469).
type TargetLabelEventHandler struct {
	Client         client.Client
	ControllerName string
	// EventSource tags the log context, e.g. "config-event", "deviation-event".
	EventSource string
}

func (r *TargetLabelEventHandler) Create(ctx context.Context, evt event.CreateEvent, q workqueue.TypedRateLimitingInterface[reconcile.Request]) {
	r.add(ctx, evt.Object, q)
}

// Update enqueues both old and new target (the target label may have changed).
func (r *TargetLabelEventHandler) Update(ctx context.Context, evt event.UpdateEvent, q workqueue.TypedRateLimitingInterface[reconcile.Request]) {
	r.add(ctx, evt.ObjectOld, q)
	r.add(ctx, evt.ObjectNew, q)
}

func (r *TargetLabelEventHandler) Delete(ctx context.Context, evt event.DeleteEvent, q workqueue.TypedRateLimitingInterface[reconcile.Request]) {
	r.add(ctx, evt.Object, q)
}

func (r *TargetLabelEventHandler) Generic(ctx context.Context, evt event.GenericEvent, q workqueue.TypedRateLimitingInterface[reconcile.Request]) {
	r.add(ctx, evt.Object, q)
}

func (r *TargetLabelEventHandler) add(ctx context.Context, obj runtime.Object, queue adder) {
	o, ok := obj.(client.Object)
	if !ok {
		return
	}
	ctx = ctrlconfig.InitContext(ctx, r.ControllerName, types.NamespacedName{Namespace: r.EventSource, Name: o.GetName()})
	log := log.FromContext(ctx)

	targetKey, err := config.GetTargetKey(o)
	if err != nil {
		// No valid target label: nothing to enqueue. Validation should reject these.
		log.Error("object has no valid target reference, not enqueued",
			"kind", o.GetObjectKind().GroupVersionKind().Kind,
			"object", client.ObjectKeyFromObject(o).String(),
			"error", err)
		return
	}
	queue.Add(reconcile.Request{NamespacedName: targetKey})
}
