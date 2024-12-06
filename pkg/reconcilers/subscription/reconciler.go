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

package subscription

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"sort"

	"github.com/henderiw/apiserver-store/pkg/storebackend"
	"github.com/henderiw/logger/log"
	pkgerrors "github.com/pkg/errors"
	condv1alpha1 "github.com/sdcio/config-server/apis/condition/v1alpha1"
	invv1alpha1 "github.com/sdcio/config-server/apis/inv/v1alpha1"
	"github.com/sdcio/config-server/pkg/reconcilers"
	"github.com/sdcio/config-server/pkg/reconcilers/ctrlconfig"
	"github.com/sdcio/config-server/pkg/reconcilers/eventhandler"
	"github.com/sdcio/config-server/pkg/reconcilers/resource"
	sdcctx "github.com/sdcio/config-server/pkg/sdc/ctx"
	sdctarget "github.com/sdcio/config-server/pkg/target"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/tools/record"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/event"
)

func init() {
	reconcilers.Register(crName, &reconciler{})
}

const (
	crName         = "subscription"
	reconcilerName = "SubscriptionController"
	finalizer      = "subscription.inv.sdcio.dev/finalizer"
	// errors
	errGetCr           = "cannot get cr"
	errUpdateDataStore = "cannot update datastore"
	errUpdateStatus    = "cannot update status"
)

// SetupWithManager sets up the controller with the Manager.
func (r *reconciler) SetupWithManager(ctx context.Context, mgr ctrl.Manager, c interface{}) (map[schema.GroupVersionKind]chan event.GenericEvent, error) {
	cfg, ok := c.(*ctrlconfig.ControllerConfig)
	if !ok {
		return nil, fmt.Errorf("cannot initialize, expecting controllerConfig, got: %s", reflect.TypeOf(c).Name())
	}

	r.Client = mgr.GetClient()
	r.finalizer = resource.NewAPIFinalizer(mgr.GetClient(), finalizer, reconcilerName)
	r.targetStore = cfg.TargetStore
	r.recorder = mgr.GetEventRecorderFor(reconcilerName)

	return nil, ctrl.NewControllerManagedBy(mgr).
		Named(reconcilerName).
		For(&invv1alpha1.Subscription{}).
		Watches(&invv1alpha1.Target{}, &eventhandler.TargetForSubscriptionEventHandler{Client: mgr.GetClient(), ControllerName: reconcilerName}).
		Complete(r)
}

type reconciler struct {
	client.Client
	finalizer       *resource.APIFinalizer
	targetStore     storebackend.Storer[*sdctarget.Context]
	dataServerStore storebackend.Storer[sdcctx.DSContext]
	recorder        record.EventRecorder
}

func (r *reconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	ctx = ctrlconfig.InitContext(ctx, reconcilerName, req.NamespacedName)
	log := log.FromContext(ctx)
	log.Info("reconcile")

	subscription := &invv1alpha1.Subscription{}
	if err := r.Get(ctx, req.NamespacedName, subscription); err != nil {
		// if the resource no longer exists the reconcile loop is done
		if resource.IgnoreNotFound(err) != nil {
			log.Error(errGetCr, "error", err)
			return ctrl.Result{}, pkgerrors.Wrap(resource.IgnoreNotFound(err), errGetCr)
		}
		return ctrl.Result{}, nil
	}

	subscriptionOrig := subscription.DeepCopy()

	if !subscription.GetDeletionTimestamp().IsZero() {
		targets, err := r.getDownstreamTargets(ctx, subscription)
		if err != nil {
			return r.handleError(ctx, subscriptionOrig, "cannot get downstream targets", err)
		}
		var errs error
		for _, targetName := range targets {
			targetKey := storebackend.KeyFromNSN(types.NamespacedName{Name: targetName, Namespace: subscription.Namespace})

			if err := r.targetStore.UpdateWithKeyFn(ctx, targetKey, func(ctx context.Context, tctx *sdctarget.Context) *sdctarget.Context {
				tctx.DeleteSubscription(ctx, subscription)
				return tctx
			}); err != nil {
				errs = errors.Join(errs, err)
			}

		}
		if errs != nil {
			return r.handleError(ctx, subscription, "cannot delete state from target collector", err)
		}

		if err := r.finalizer.RemoveFinalizer(ctx, subscription); err != nil {
			// we always retry when status fails -> optimistic concurrency
			return r.handleError(ctx, subscription, "cannot remove finalizer", err)
		}

		return ctrl.Result{}, nil
	}

	if err := r.finalizer.AddFinalizer(ctx, subscription); err != nil {
		// we always retry when status fails -> optimistic concurrency
		return r.handleError(ctx, subscription, "cannot remove finalizer", err)
	}

	// get existing targets in a set on which the target state was applied
	existingTargetSet := subscription.GetExistingTargets()
	// get the new targets based on the current state
	targets, err := r.getDownstreamTargets(ctx, subscription)
	if err != nil {
		return r.handleError(ctx, subscriptionOrig, "cannot get downstream targets", err)
	}
	var errs error
	for _, targetName := range targets {
		targetKey := storebackend.KeyFromNSN(types.NamespacedName{Name: targetName, Namespace: subscription.Namespace})
		if err := r.targetStore.UpdateWithKeyFn(ctx, targetKey, func(ctx context.Context, tctx *sdctarget.Context) *sdctarget.Context {
			tctx.UpsertSubscription(ctx, subscription)
			return tctx
		}); err != nil {
			errs = errors.Join(errs, err)
		}

		if !existingTargetSet.Has(targetName) {
			if err := r.targetStore.UpdateWithKeyFn(ctx, targetKey, func(ctx context.Context, tctx *sdctarget.Context) *sdctarget.Context {
				tctx.DeleteSubscription(ctx, subscription)
				return tctx
			}); err != nil {
				errs = errors.Join(errs, err)
			}
		}
	}
	if errs != nil {
		return r.handleError(ctx, subscriptionOrig, "cannot update target collectors", err)
	}
	return r.handleSuccess(ctx, subscription, targets)
}

func (r *reconciler) handleSuccess(ctx context.Context, state *invv1alpha1.Subscription, targets []string) (ctrl.Result, error) {
	log := log.FromContext(ctx)
	log.Debug("handleSuccess", "key", state.GetNamespacedName(), "status old", state.DeepCopy().Status)
	// take a snapshot of the current object
	patch := client.MergeFrom(state.DeepCopy())
	// update status
	state.SetConditions(condv1alpha1.Ready())
	state.SetTargets(targets)
	r.recorder.Eventf(state, corev1.EventTypeNormal, invv1alpha1.SubscriptionKind, "ready")

	log.Debug("handleSuccess", "key", state.GetNamespacedName(), "status new", state.Status)

	return ctrl.Result{}, pkgerrors.Wrap(r.Client.Status().Patch(ctx, state, patch, &client.SubResourcePatchOptions{
		PatchOptions: client.PatchOptions{
			FieldManager: reconcilerName,
		},
	}), errUpdateStatus)
}

func (r *reconciler) handleError(ctx context.Context, state *invv1alpha1.Subscription, msg string, err error) (ctrl.Result, error) {
	log := log.FromContext(ctx)

	// take a snapshot of the current object
	patch := client.MergeFrom(state.DeepCopy())

	if err != nil {
		msg = fmt.Sprintf("%s err %s", msg, err.Error())
	}

	state.SetConditions(condv1alpha1.Failed(msg))
	log.Error(msg)
	r.recorder.Eventf(state, corev1.EventTypeWarning, crName, msg)

	return ctrl.Result{}, pkgerrors.Wrap(r.Client.Status().Patch(ctx, state, patch, &client.SubResourcePatchOptions{
		PatchOptions: client.PatchOptions{
			FieldManager: reconcilerName,
		},
	}), errUpdateStatus)
}

// getDownstreamTargets list the targets
func (r *reconciler) getDownstreamTargets(ctx context.Context, state *invv1alpha1.Subscription) ([]string, error) {
	selector, err := metav1.LabelSelectorAsSelector(state.Spec.Target.TargetSelector)
	if err != nil {
		return nil, fmt.Errorf("parsing selector failed: err: %s", err.Error())
	}
	opts := []client.ListOption{
		client.InNamespace(state.Namespace),
		client.MatchingLabelsSelector{Selector: selector},
	}

	targetList := &invv1alpha1.TargetList{}
	if err := r.List(ctx, targetList, opts...); err != nil {
		return nil, err
	}
	targets := make([]string, 0, len(targetList.Items))
	for _, target := range targetList.Items {
		// only add targets that are not in deleting state
		if target.GetDeletionTimestamp().IsZero() {
			targets = append(targets, target.Name)
		}
	}
	targets = sort.StringSlice(targets)
	return targets, nil
}
