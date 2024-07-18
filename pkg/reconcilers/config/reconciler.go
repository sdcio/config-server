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

package config

import (
	"context"
	"fmt"
	"reflect"
	"time"

	"github.com/henderiw/logger/log"
	"github.com/pkg/errors"
	condv1alpha1 "github.com/sdcio/config-server/apis/condition/v1alpha1"
	"github.com/sdcio/config-server/apis/config"
	configv1alpha1 "github.com/sdcio/config-server/apis/config/v1alpha1"
	invv1alpha1 "github.com/sdcio/config-server/apis/inv/v1alpha1"
	"github.com/sdcio/config-server/pkg/reconcilers"
	"github.com/sdcio/config-server/pkg/reconcilers/ctrlconfig"
	"github.com/sdcio/config-server/pkg/reconcilers/eventhandler"
	"github.com/sdcio/config-server/pkg/reconcilers/resource"
	"github.com/sdcio/config-server/pkg/target"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
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
	crName         = "config"
	controllerName = "ConfigController"
	finalizer      = "config.config.sdcio.dev/finalizer"
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
	r.finalizer = resource.NewAPIFinalizer(mgr.GetClient(), finalizer)
	r.targetHandler = cfg.TargetHandler
	r.recorder = mgr.GetEventRecorderFor(controllerName)

	return nil, ctrl.NewControllerManagedBy(mgr).
		Named(controllerName).
		For(&configv1alpha1.Config{}).
		Watches(&invv1alpha1.Target{}, &eventhandler.TargetForConfigEventHandler{Client: mgr.GetClient(), ControllerName: controllerName}).
		Complete(r)
}

type reconciler struct {
	client.Client
	finalizer     *resource.APIFinalizer
	targetHandler *target.TargetHandler
	recorder      record.EventRecorder
}

func (r *reconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	ctx = ctrlconfig.InitContext(ctx, controllerName, req.NamespacedName)
	log := log.FromContext(ctx)
	log.Info("reconcile")

	v1alpha1config := &configv1alpha1.Config{}
	if err := r.Get(ctx, req.NamespacedName, v1alpha1config); err != nil {
		// if the resource no longer exists the reconcile loop is done
		if resource.IgnoreNotFound(err) != nil {
			log.Error(errGetCr, "error", err)
			return ctrl.Result{}, errors.Wrap(resource.IgnoreNotFound(err), errGetCr)
		}
		return ctrl.Result{}, nil
	}
	v1alpha1config = v1alpha1config.DeepCopy()
	cfg := &config.Config{}
	if err := configv1alpha1.Convert_v1alpha1_Config_To_config_Config(v1alpha1config, cfg, nil); err != nil {
		r.handleError(ctx, v1alpha1config, "cannot convert config", err)
		return ctrl.Result{}, errors.Wrap(r.Status().Update(ctx, v1alpha1config), errUpdateStatus)
	}

	targetKey, err := config.GetTargetKey(v1alpha1config.GetLabels())
	if err != nil {
		// this should never fail since validation covered this
		return ctrl.Result{}, nil
	}

	if !v1alpha1config.GetDeletionTimestamp().IsZero() {
		_, err := r.targetHandler.DeleteIntent(ctx, targetKey, cfg, false)
		if err != nil {
			if errors.Is(err, target.LookupError) {
				// Since the target is not available we delete the resource
				// The target config might not be deleted
				if err := r.finalizer.RemoveFinalizer(ctx, v1alpha1config); err != nil {
					r.handleError(ctx, v1alpha1config, "cannot remove finalizer", err)
					return ctrl.Result{Requeue: true}, errors.Wrap(r.Status().Update(ctx, v1alpha1config), errUpdateStatus)
				}
				return ctrl.Result{}, nil
			}
			r.handleError(ctx, v1alpha1config, "delete intent failed", err)
			// all grpc errors except resource exhausted will not retry
			// and a human need to intervene
			if er, ok := status.FromError(err); ok {
				if er.Code() == codes.ResourceExhausted {
					return ctrl.Result{Requeue: true, RequeueAfter: 5 * time.Second}, errors.Wrap(r.Status().Update(ctx, v1alpha1config), errUpdateStatus)
				}
			}
			return ctrl.Result{}, errors.Wrap(r.Status().Update(ctx, v1alpha1config), errUpdateStatus)

		}
		if err := r.finalizer.RemoveFinalizer(ctx, v1alpha1config); err != nil {
			r.handleError(ctx, v1alpha1config, "cannot remove finalizer", err)
			return ctrl.Result{Requeue: true}, errors.Wrap(r.Status().Update(ctx, v1alpha1config), errUpdateStatus)
		}
		return ctrl.Result{}, nil
	}

	if err := r.finalizer.AddFinalizer(ctx, v1alpha1config); err != nil {
		r.handleError(ctx, v1alpha1config, "cannot add finalizer", err)
		return ctrl.Result{Requeue: true}, errors.Wrap(r.Status().Update(ctx, v1alpha1config), errUpdateStatus)
	}

	// check if we have to reapply the config
	// if condition is false -> reapply the config
	// if the applied Config is not set -> reapply the config
	// if the applied Config is different than the spec -> reapply the config
	// if the deviation is having the reason xx -> reapply the config
	if v1alpha1config.GetCondition(condv1alpha1.ConditionTypeReady).Status == metav1.ConditionTrue &&
		v1alpha1config.Status.AppliedConfig != nil &&
		v1alpha1config.Spec.GetShaSum(ctx) == v1alpha1config.Status.AppliedConfig.GetShaSum(ctx) &&
		!v1alpha1config.Status.HasNotAppliedDeviation() {
		return ctrl.Result{}, nil
	}

	_, schema, err := r.targetHandler.SetIntent(ctx, targetKey, cfg, true, false)
	if err != nil {
		r.handleError(ctx, v1alpha1config, "setIntent failed", err)
		// all grpc errors except resource exhausted will not retry
		// and a human need to intervene
		if er, ok := status.FromError(err); ok {
			if er.Code() == codes.ResourceExhausted {
				return ctrl.Result{Requeue: true, RequeueAfter: 5 * time.Second}, errors.Wrap(r.Update(ctx, v1alpha1config), errUpdateStatus)
			}
		}
		return ctrl.Result{}, errors.Wrap(r.Status().Update(ctx, v1alpha1config), errUpdateStatus)
	}

	v1alpha1Schema := &configv1alpha1.ConfigStatusLastKnownGoodSchema{}
	if err := configv1alpha1.Convert_config_ConfigStatusLastKnownGoodSchema_To_v1alpha1_ConfigStatusLastKnownGoodSchema(schema, v1alpha1Schema, nil); err != nil {
		r.handleError(ctx, v1alpha1config, "cannot comvert schema", err)
		return ctrl.Result{}, errors.Wrap(r.Status().Update(ctx, v1alpha1config), errUpdateStatus)
	}

	v1alpha1config.SetConditions(condv1alpha1.Ready())
	v1alpha1config.Status.LastKnownGoodSchema = v1alpha1Schema
	v1alpha1config.Status.Deviations = []configv1alpha1.Deviation{} // reset deviations
	v1alpha1config.Status.AppliedConfig = &v1alpha1config.Spec
	r.recorder.Eventf(v1alpha1config, corev1.EventTypeNormal,
		"config", "ready")
	return ctrl.Result{}, errors.Wrap(r.Status().Update(ctx, v1alpha1config), errUpdateStatus)
}

func (r *reconciler) handleError(ctx context.Context, v1alpha1config *configv1alpha1.Config, msg string, err error) {
	log := log.FromContext(ctx)
	if err == nil {
		v1alpha1config.SetConditions(condv1alpha1.Failed(msg))
		log.Error(msg)
		r.recorder.Eventf(v1alpha1config, corev1.EventTypeWarning, crName, msg)
	} else {
		v1alpha1config.SetConditions(condv1alpha1.Failed(err.Error()))
		log.Error(msg, "error", err)
		r.recorder.Eventf(v1alpha1config, corev1.EventTypeWarning, crName, fmt.Sprintf("%s, err: %s", msg, err.Error()))
	}
}
