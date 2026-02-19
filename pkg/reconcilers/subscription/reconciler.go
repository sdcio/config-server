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
	"fmt"
	"reflect"

	"github.com/henderiw/logger/log"
	pkgerrors "github.com/pkg/errors"
	condv1alpha1 "github.com/sdcio/config-server/apis/condition/v1alpha1"
	invv1alpha1 "github.com/sdcio/config-server/apis/inv/v1alpha1"
	invv1alpha1apply "github.com/sdcio/config-server/pkg/generated/applyconfiguration/inv/v1alpha1"
	"github.com/sdcio/config-server/pkg/reconcilers"
	"github.com/sdcio/config-server/pkg/reconcilers/ctrlconfig"
	"github.com/sdcio/config-server/pkg/reconcilers/eventhandler"
	"github.com/sdcio/config-server/pkg/reconcilers/resource"
	targetmanager "github.com/sdcio/config-server/pkg/sdc/target/manager"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/tools/events"
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

	r.client = mgr.GetClient()
	r.finalizer = resource.NewAPIFinalizer(mgr.GetClient(), finalizer, reconcilerName)
	r.targetMgr = cfg.TargetManager
	r.recorder = mgr.GetEventRecorder(reconcilerName)

	return nil, ctrl.NewControllerManagedBy(mgr).
		Named(reconcilerName).
		For(&invv1alpha1.Subscription{}).
		Watches(&invv1alpha1.Target{}, &eventhandler.TargetForSubscriptionEventHandler{Client: mgr.GetClient(), ControllerName: reconcilerName}).
		Complete(r)
}

type reconciler struct {
	client    client.Client
	finalizer *resource.APIFinalizer
	targetMgr *targetmanager.TargetManager
	recorder  events.EventRecorder
}

func (r *reconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	ctx = ctrlconfig.InitContext(ctx, reconcilerName, req.NamespacedName)
	log := log.FromContext(ctx)
	log.Info("reconcile")

	subscription := &invv1alpha1.Subscription{}
	if err := r.client.Get(ctx, req.NamespacedName, subscription); err != nil {
		// if the resource no longer exists the reconcile loop is done
		if resource.IgnoreNotFound(err) != nil {
			log.Error(errGetCr, "error", err)
			return ctrl.Result{}, pkgerrors.Wrap(resource.IgnoreNotFound(err), errGetCr)
		}
		return ctrl.Result{}, nil
	}

	subscriptionOrig := subscription.DeepCopy()

	if !subscription.GetDeletionTimestamp().IsZero() {

		if err := r.targetMgr.RemoveSubscription(ctx, subscription); err != nil {
			return r.handleError(ctx, subscription, "cannot delete state from target collector", err)
		}

		if err := r.finalizer.RemoveFinalizer(ctx, subscription); err != nil {
			// we always retry when status fails -> optimistic concurrency
			return r.handleError(ctx, subscriptionOrig, "cannot remove finalizer", err)
		}

		return ctrl.Result{}, nil
	}

	if err := r.finalizer.AddFinalizer(ctx, subscription); err != nil {
		// we always retry when status fails -> optimistic concurrency
		return r.handleError(ctx, subscriptionOrig, "cannot remove finalizer", err)
	}

	targets, err := r.targetMgr.ApplySubscription(ctx, subscription)
	if err != nil {
		return r.handleError(ctx, subscription, "cannot delete state from target collector", err)
	}
	return r.handleSuccess(ctx, subscription, targets)
}

func (r *reconciler) handleSuccess(ctx context.Context, state *invv1alpha1.Subscription, targets []string) (ctrl.Result, error) {
	log := log.FromContext(ctx)
	log.Debug("handleSuccess", "key", state.GetNamespacedName(), "status old", state.DeepCopy().Status)

	newCond := condv1alpha1.Ready()
	oldCond := state.GetCondition(condv1alpha1.ConditionTypeReady)

	// no-change guard
	if newCond.Equal(oldCond) && reflect.DeepEqual(targets, state.Status.Targets) {
		log.Info("handleSuccess -> no change")
		return ctrl.Result{}, nil
	}

	r.recorder.Eventf(state, nil, corev1.EventTypeNormal, invv1alpha1.SubscriptionKind, "ready", "")

	applyConfig := invv1alpha1apply.Subscription(state.Name, state.Namespace).
		WithStatus(invv1alpha1apply.SubscriptionStatus().
			WithConditions(newCond).
			WithTargets(targets...),
		)

	return ctrl.Result{}, pkgerrors.Wrap(r.client.Status().Apply(ctx, applyConfig, &client.SubResourceApplyOptions{
		ApplyOptions: client.ApplyOptions{
			FieldManager: reconcilerName,
		},
	}), errUpdateStatus)
}

func (r *reconciler) handleError(ctx context.Context, state *invv1alpha1.Subscription, msg string, err error) (ctrl.Result, error) {
	log := log.FromContext(ctx)

	if err != nil {
		msg = fmt.Sprintf("%s err %s", msg, err.Error())
	}

	newCond := condv1alpha1.Failed(msg)
	oldCond := state.GetCondition(condv1alpha1.ConditionTypeReady)

	if newCond.Equal(oldCond) {
		log.Info("handleError -> no change")
		return ctrl.Result{}, nil
	}

	log.Error(msg)
	r.recorder.Eventf(state, nil, corev1.EventTypeWarning, crName, msg, "")

	applyConfig := invv1alpha1apply.Subscription(state.Name, state.Namespace).
		WithStatus(invv1alpha1apply.SubscriptionStatus().
			WithConditions(newCond),
		)

	return ctrl.Result{}, pkgerrors.Wrap(r.client.Status().Apply(ctx, applyConfig, &client.SubResourceApplyOptions{
		ApplyOptions: client.ApplyOptions{
			FieldManager: reconcilerName,
		},
	}), errUpdateStatus)
}
