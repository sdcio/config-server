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

package workspace

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
	"github.com/sdcio/config-server/pkg/reconcilers/eventhandler"
	"github.com/sdcio/config-server/pkg/reconcilers/resource"
	workspaceloader "github.com/sdcio/config-server/pkg/workspace"
	corev1 "k8s.io/api/core/v1"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/tools/events"
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
	crName         = "workspace"
	reconcilerName = "WorkspaceController"
	finalizer      = "workspace.inv.sdcio.dev/finalizer"
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

	r.client = mgr.GetClient()
	r.finalizer = resource.NewAPIFinalizer(mgr.GetClient(), finalizer, reconcilerName)
	// initializes the directory
	r.workspaceLoader, err = workspaceloader.NewLoader(
		cfg.WorkspaceDir,
		secret.NewCredentialResolver(mgr.GetClient(), []secret.Resolver{
			secret.NewBasicAuthResolver(),
		}),
	)
	if err != nil {
		return nil, errors.Wrap(err, "cannot initialize WorkspaceController")
	}
	r.recorder = mgr.GetEventRecorder(reconcilerName)

	return nil, ctrl.NewControllerManagedBy(mgr).
		Named(reconcilerName).
		For(&invv1alpha1.Workspace{}, builder.WithPredicates(predicate.GenerationChangedPredicate{})).
		Owns(&invv1alpha1.Rollout{}, builder.WithPredicates(predicate.GenerationChangedPredicate{})).
		Watches(&corev1.Secret{}, &eventhandler.SecretForWorkspaceEventHandler{Client: mgr.GetClient(), ControllerName: reconcilerName}).
		Complete(r)
}

type reconciler struct {
	client client.Client
	finalizer *resource.APIFinalizer

	workspaceLoader *workspaceloader.Loader
	recorder        events.EventRecorder
}

func (r *reconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	ctx = ctrlconfig.InitContext(ctx, reconcilerName, req.NamespacedName)
	log := log.FromContext(ctx)
	log.Info("reconcile")

	workspace := &invv1alpha1.Workspace{}
	if err := r.client.Get(ctx, req.NamespacedName, workspace); err != nil {
		// if the resource no longer exists the reconcile loop is done
		if !k8serrors.IsNotFound(err) {
			log.Error(errGetCr, "error", err)
			return ctrl.Result{}, errors.Wrap(resource.IgnoreNotFound(err), errGetCr)
		}
		return ctrl.Result{}, nil
	}
	workspaceOrig := workspace.DeepCopy()

	if !workspace.GetDeletionTimestamp().IsZero() {
		rollout := &invv1alpha1.Rollout{}
		if err := r.client.Get(ctx, workspace.GetNamespacedName(), rollout); err != nil {
			if !k8serrors.IsNotFound(err) {
				return r.handleStatus(ctx, workspaceOrig, condv1alpha1.Failed("cannot get rollout"), true, err)
			}
			// remove the finalizer
			if err := r.finalizer.RemoveFinalizer(ctx, workspace); err != nil {
				return r.handleStatus(ctx, workspaceOrig, condv1alpha1.Failed("cannot remove finalizer"), true, err)
			}
			// done deleting
			return ctrl.Result{}, nil
		}

		if err := r.client.Delete(ctx, rollout); err != nil {
			return r.handleStatus(ctx, workspaceOrig, condv1alpha1.Failed("cannot delete rollout"), true, err)
		}
		// done deleting
		return ctrl.Result{}, nil
	}

	if err := r.finalizer.AddFinalizer(ctx, workspace); err != nil {
		// we always retry when status fails -> optimistic concurrency
		return r.handleStatus(ctx, workspaceOrig, condv1alpha1.Failed("cannot add finalizer"), true, err)

	}

	rollout, rolloutCondition := r.getRolloutStatus(ctx, workspace)
	switch rolloutCondition.Type {
	case string(ConditionType_RolloutGetFailed):
		// should be handled by the previous status
		return r.handleStatus(ctx, workspaceOrig, condv1alpha1.Failed("cannot get rollout status"), true, fmt.Errorf("%s", rolloutCondition.Message))
	case string(ConditionType_RollingOut):
		return r.handleStatus(ctx, workspaceOrig, condv1alpha1.Rollout(fmt.Sprintf("rollout is still ongoing, ref %s", workspace.Spec.Ref)), false, nil)
	case string(ConditionType_RolloutDone_NoMatchRef), string(ConditionType_RolloutDone_MatchRefNew):
		// download the reference
		branch, err := r.workspaceLoader.EnsureCommit(ctx, workspace)
		if err != nil {
			return r.handleStatus(ctx, workspaceOrig, condv1alpha1.Failed("cannot get reference commit"), true, err)
		}
		if branch != "main" {
			log.Info("rollforward")
		} else {
			log.Info("rollback")
		}
		// apply the rollout CR
		if err := r.applyRollout(ctx, workspace, rollout); err != nil {
			return r.handleStatus(ctx, workspaceOrig, condv1alpha1.Failed("cannot apply rollout"), true, err)
		}
		return r.handleStatus(ctx, workspaceOrig, condv1alpha1.Rollout(fmt.Sprintf("new rollout triggered, ref %s", workspace.Spec.Ref)), false, nil)
	case string(ConditionType_RolloutFailed):
		return r.handleStatus(ctx, workspaceOrig, condv1alpha1.Failed(rolloutCondition.Message), false, nil)
	default:
		// workspace ready -> rollout done and reference match
		return r.handleStatus(ctx, workspaceOrig, condv1alpha1.Ready(), false, nil)
	}
}

func (r *reconciler) handleStatus(
	ctx context.Context,
	workspace *invv1alpha1.Workspace,
	condition condv1alpha1.Condition,
	requeue bool,
	err error,
) (ctrl.Result, error) {
	log := log.FromContext(ctx)

	patch := client.MergeFrom(workspace.DeepCopy())

	if err != nil {
		condition.Message = fmt.Sprintf("%s err %s", condition.Message, err.Error())
	}

	workspace.ManagedFields = nil
	workspace.SetConditions(condition)

	// Determine event type based on condition type
	if condition.Type == string(condv1alpha1.ConditionTypeReady) {
		r.recorder.Eventf(workspace, nil, corev1.EventTypeNormal, crName, fmt.Sprintf("ready ref %s", workspace.Spec.Ref), "")
	} else {
		log.Error(condition.Message)
		r.recorder.Eventf(workspace, nil, corev1.EventTypeWarning, crName, condition.Message, "")
	}

	result := ctrl.Result{Requeue: requeue}
	return result, errors.Wrap(r.client.Status().Patch(ctx, workspace, patch, &client.SubResourcePatchOptions{
		PatchOptions: client.PatchOptions{
			FieldManager: reconcilerName,
		},
	}), errUpdateStatus)
}

/*

func (r *reconciler) handleSuccess(ctx context.Context, workspace *invv1alpha1.Workspace) (ctrl.Result, error) {
	log := log.FromContext(ctx)
	log.Debug("handleSuccess", "key", workspace.GetNamespacedName(), "status old", workspace.DeepCopy().Status)
	// take a snapshot of the current object
	patch := client.MergeFrom(workspace.DeepCopy())
	// update status
	workspace.SetConditions(condv1alpha1.Ready())
	r.recorder.Eventf(workspace, corev1.EventTypeNormal, crName, "ready")

	log.Debug("handleSuccess", "key", workspace.GetNamespacedName(), "status new", workspace.Status)

	return ctrl.Result{}, errors.Wrap(r.Client.Status().Patch(ctx, workspace, patch, &client.SubResourcePatchOptions{
		PatchOptions: client.PatchOptions{
			FieldManager: reconcilerName,
		},
	}), errUpdateStatus)
}

func (r *reconciler) handleFailed(ctx context.Context, workspace *invv1alpha1.Workspace, msg string) (ctrl.Result, error) {
	log := log.FromContext(ctx)
	log.Debug("handleFailure", "key", workspace.GetNamespacedName(), "status old", workspace.DeepCopy().Status)
	// take a snapshot of the current object
	patch := client.MergeFrom(workspace.DeepCopy())
	// update status
	workspace.SetConditions(condv1alpha1.Failed(msg))
	r.recorder.Eventf(workspace, corev1.EventTypeNormal, crName, "failed")

	log.Debug("handleFailure", "key", workspace.GetNamespacedName(), "status new", workspace.Status)

	return ctrl.Result{}, errors.Wrap(r.Client.Status().Patch(ctx, workspace, patch, &client.SubResourcePatchOptions{
		PatchOptions: client.PatchOptions{
			FieldManager: reconcilerName,
		},
	}), errUpdateStatus)
}

func (r *reconciler) handleRollout(ctx context.Context, workspace *invv1alpha1.Workspace, msg string) (ctrl.Result, error) {
	log := log.FromContext(ctx)

	// take a snapshot of the current object
	patch := client.MergeFrom(workspace.DeepCopy())

	workspace.SetConditions(condv1alpha1.Rollout(msg))
	log.Debug(msg)
	r.recorder.Eventf(workspace, corev1.EventTypeNormal, crName, msg)

	result := ctrl.Result{}
	return result, errors.Wrap(r.Client.Status().Patch(ctx, workspace, patch, &client.SubResourcePatchOptions{
		PatchOptions: client.PatchOptions{
			FieldManager: reconcilerName,
		},
	}), errUpdateStatus)
}

func (r *reconciler) handleError(ctx context.Context, workspace *invv1alpha1.Workspace, msg string, err error) (ctrl.Result, error) {
	log := log.FromContext(ctx)

	// take a snapshot of the current object
	patch := client.MergeFrom(workspace.DeepCopy())

	if err != nil {
		msg = fmt.Sprintf("%s err %s", msg, err.Error())
	}

	workspace.SetConditions(condv1alpha1.Failed(msg))
	log.Error(msg)
	r.recorder.Eventf(workspace, corev1.EventTypeWarning, crName, msg)

	result := ctrl.Result{Requeue: true}
	return result, errors.Wrap(r.Client.Status().Patch(ctx, workspace, patch, &client.SubResourcePatchOptions{
		PatchOptions: client.PatchOptions{
			FieldManager: reconcilerName,
		},
	}), errUpdateStatus)
}
*/
