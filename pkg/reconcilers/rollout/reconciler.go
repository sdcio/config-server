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

package rollout

import (
	"context"
	"fmt"
	"reflect"

	"github.com/henderiw/logger/log"
	"github.com/pkg/errors"
	condv1alpha1 "github.com/sdcio/config-server/apis/condition/v1alpha1"
	invv1alpha1 "github.com/sdcio/config-server/apis/inv/v1alpha1"
	"github.com/sdcio/config-server/pkg/git/auth/secret"
	"github.com/sdcio/config-server/pkg/reconcilers"
	"github.com/sdcio/config-server/pkg/reconcilers/ctrlconfig"
	"github.com/sdcio/config-server/pkg/reconcilers/resource"
	workspaceloader "github.com/sdcio/config-server/pkg/workspace"
	corev1 "k8s.io/api/core/v1"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/tools/record"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/builder"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/event"
	"sigs.k8s.io/controller-runtime/pkg/predicate"
)

func init() {
	reconcilers.Register(crName, &reconciler{})
}

const (
	crName         = "rollout"
	reconcilerName = "RolloutController"
	finalizer      = "rollout.inv.sdcio.dev/finalizer"
	// errors
	errGetCr           = "cannot get cr"
	errUpdateDataStore = "cannot update datastore"
	errUpdateStatus    = "cannot update status"
)

// SetupWithManager sets up the controller with the Manager.
func (r *reconciler) SetupWithManager(ctx context.Context, mgr ctrl.Manager, c interface{}) (map[schema.GroupVersionKind]chan event.GenericEvent, error) {
	var err error
	cfg, ok := c.(*ctrlconfig.ControllerConfig)
	if !ok {
		return nil, fmt.Errorf("cannot initialize, expecting controllerConfig, got: %s", reflect.TypeOf(c).Name())
	}

	r.Client = mgr.GetClient()
	r.finalizer = resource.NewAPIFinalizer(mgr.GetClient(), finalizer, reconcilerName)
	// initializes the directory
	r.workspaceLoader, err = workspaceloader.NewLoader(
		cfg.WorkspaceDir,
		secret.NewCredentialResolver(mgr.GetClient(), []secret.Resolver{
			secret.NewBasicAuthResolver(),
		}),
	)
	if err != nil {
		return nil, errors.Wrap(err, "cannot initialize RolloutController")
	}
	r.recorder = mgr.GetEventRecorderFor(reconcilerName)

	return nil, ctrl.NewControllerManagedBy(mgr).
		Named(reconcilerName).
		For(&invv1alpha1.Rollout{}, builder.WithPredicates(predicate.GenerationChangedPredicate{})).
		Complete(r)
}

type reconciler struct {
	client.Client
	finalizer *resource.APIFinalizer

	workspaceLoader *workspaceloader.Loader
	recorder        record.EventRecorder
}

func (r *reconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	ctx = ctrlconfig.InitContext(ctx, reconcilerName, req.NamespacedName)
	log := log.FromContext(ctx)
	log.Info("reconcile")

	rollout := &invv1alpha1.Rollout{}
	if err := r.Get(ctx, req.NamespacedName, rollout); err != nil {
		// if the resource no longer exists the reconcile loop is done
		if !k8serrors.IsNotFound(err) {
			log.Error(errGetCr, "error", err)
			return ctrl.Result{}, errors.Wrap(resource.IgnoreNotFound(err), errGetCr)
		}
		return ctrl.Result{}, nil
	}
	rolloutOrig := rollout.DeepCopy()
	//spec := &workspace.Spec

	if !rollout.GetDeletionTimestamp().IsZero() {
		// TODO delete the configs through transactions

		// remove the finalizer
		if err := r.finalizer.RemoveFinalizer(ctx, rollout); err != nil {
			return r.handleError(ctx, rolloutOrig, "cannot remove finalizer", err)
		}
		// done deleting
		return ctrl.Result{}, nil
	}

	if err := r.finalizer.AddFinalizer(ctx, rollout); err != nil {
		// we always retry when status fails -> optimistic concurrency
		return r.handleError(ctx, rolloutOrig, "cannot add finalizer", err)
	}

	// workspace ready -> rollout done and reference match
	return r.handleSuccess(ctx, rolloutOrig)

}

func (r *reconciler) handleSuccess(ctx context.Context, rollout *invv1alpha1.Rollout) (ctrl.Result, error) {
	log := log.FromContext(ctx)
	log.Debug("handleSuccess", "key", rollout.GetNamespacedName(), "status old", rollout.DeepCopy().Status)
	// take a snapshot of the current object
	patch := client.MergeFrom(rollout.DeepCopy())
	// update status
	rollout.SetConditions(condv1alpha1.Ready())
	r.recorder.Eventf(rollout, corev1.EventTypeNormal, crName, "ready")

	log.Debug("handleSuccess", "key", rollout.GetNamespacedName(), "status new", rollout.Status)

	return ctrl.Result{}, errors.Wrap(r.Client.Status().Patch(ctx, rollout, patch, &client.SubResourcePatchOptions{
		PatchOptions: client.PatchOptions{
			FieldManager: reconcilerName,
		},
	}), errUpdateStatus)
}

func (r *reconciler) handleError(ctx context.Context, rollout *invv1alpha1.Rollout, msg string, err error) (ctrl.Result, error) {
	log := log.FromContext(ctx)

	// take a snapshot of the current object
	patch := client.MergeFrom(rollout.DeepCopy())

	if err != nil {
		msg = fmt.Sprintf("%s err %s", msg, err.Error())
	}

	rollout.SetConditions(condv1alpha1.Failed(msg))
	log.Error(msg)
	r.recorder.Eventf(rollout, corev1.EventTypeWarning, crName, msg)

	result := ctrl.Result{Requeue: true}
	return result, errors.Wrap(r.Client.Status().Patch(ctx, rollout, patch, &client.SubResourcePatchOptions{
		PatchOptions: client.PatchOptions{
			FieldManager: reconcilerName,
		},
	}), errUpdateStatus)
}
