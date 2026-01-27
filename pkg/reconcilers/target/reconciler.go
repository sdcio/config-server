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

	"github.com/henderiw/logger/log"
	pkgerrors "github.com/pkg/errors"
	condv1alpha1 "github.com/sdcio/config-server/apis/condition/v1alpha1"
	invv1alpha1 "github.com/sdcio/config-server/apis/inv/v1alpha1"
	"github.com/sdcio/config-server/pkg/reconcilers"
	"github.com/sdcio/config-server/pkg/reconcilers/ctrlconfig"
	"github.com/sdcio/config-server/pkg/reconcilers/resource"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/tools/record"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/event"
)

func init() {
	reconcilers.Register(crName, &reconciler{})
}

const (
	crName         = "target"
	reconcilerName = "TargetController"
	finalizer      = "target.inv.sdcio.dev/finalizer"
	// errors
	errGetCr           = "cannot get cr"
	errUpdateDataStore = "cannot update datastore"
	errUpdateStatus    = "cannot update status"
)

// SetupWithManager sets up the controller with the Manager.
func (r *reconciler) SetupWithManager(ctx context.Context, mgr ctrl.Manager, c interface{}) (map[schema.GroupVersionKind]chan event.GenericEvent, error) {
	r.client = mgr.GetClient()
	r.finalizer = resource.NewAPIFinalizer(mgr.GetClient(), finalizer, reconcilerName)
	r.recorder = mgr.GetEventRecorderFor(reconcilerName)

	return nil, ctrl.NewControllerManagedBy(mgr).
		Named(reconcilerName).
		For(&invv1alpha1.Target{}).
		Complete(r)
}

type reconciler struct {
	client    client.Client
	finalizer *resource.APIFinalizer
	recorder  record.EventRecorder
}

func (r *reconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	ctx = ctrlconfig.InitContext(ctx, reconcilerName, req.NamespacedName)
	log := log.FromContext(ctx)
	log.Info("reconcile")

	target := &invv1alpha1.Target{}
	if err := r.client.Get(ctx, req.NamespacedName, target); err != nil {
		// if the resource no longer exists the reconcile loop is done
		if resource.IgnoreNotFound(err) != nil {
			log.Error(errGetCr, "error", err)
			return ctrl.Result{}, pkgerrors.Wrap(resource.IgnoreNotFound(err), errGetCr)
		}
		return ctrl.Result{}, nil
	}

	targetOrig := target.DeepCopy()

	if !target.GetDeletionTimestamp().IsZero() {
		return ctrl.Result{}, nil
	}

	return ctrl.Result{}, pkgerrors.Wrap(r.updatCondition(ctx, targetOrig), errUpdateStatus)
}

func (r *reconciler) updatCondition(ctx context.Context, target *invv1alpha1.Target) error {
	log := log.FromContext(ctx)
	log.Debug("handleSuccess", "key", target.GetNamespacedName(), "status old", target.DeepCopy().Status)
	// take a snapshot of the current object
	//patch := client.MergeFrom(target.DeepCopy())
	// update status
	newTarget := invv1alpha1.BuildTarget(
		metav1.ObjectMeta{
			Namespace: target.Namespace,
			Name:      target.Name,
		},
		invv1alpha1.TargetSpec{},
		invv1alpha1.TargetStatus{},
	)
	// set new conditions
	newTarget.SetOverallStatus(target)

	// we don't update the resource if no condition changed
	if newTarget.GetCondition(condv1alpha1.ConditionTypeReady).Equal(target.GetCondition(condv1alpha1.ConditionTypeReady)) {
		log.Info("updateCondition -> no change")
		return nil
	}
	log.Info("updateCondition -> change",
		"Ready condition change", newTarget.GetCondition(condv1alpha1.ConditionTypeReady).Equal(target.GetCondition(condv1alpha1.ConditionTypeReady)),
	)

	r.recorder.Eventf(newTarget, corev1.EventTypeNormal, invv1alpha1.TargetKind, "ready")

	log.Debug("handleSuccess", "key", newTarget.GetNamespacedName(), "status new", target.Status)

	return r.client.Status().Patch(ctx, newTarget, client.Apply, &client.SubResourcePatchOptions{
		PatchOptions: client.PatchOptions{
			FieldManager: reconcilerName,
		},
	})
}
