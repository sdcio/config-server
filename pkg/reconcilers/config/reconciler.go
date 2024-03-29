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

	"github.com/henderiw/apiserver-store/pkg/storebackend"
	"github.com/henderiw/logger/log"
	"github.com/pkg/errors"
	configv1alpha1 "github.com/sdcio/config-server/apis/config/v1alpha1"
	invv1alpha1 "github.com/sdcio/config-server/apis/inv/v1alpha1"
	"github.com/sdcio/config-server/pkg/reconcilers"
	"github.com/sdcio/config-server/pkg/reconcilers/ctrlconfig"
	"github.com/sdcio/config-server/pkg/reconcilers/resource"
	"github.com/sdcio/config-server/pkg/target"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
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
	reconcilers.Register("config", &reconciler{})
}

const (
	controllerName = "ConfigController"
	finalizer      = "config.config.sdcio.dev/finalizer"
	// errors
	errGetCr           = "cannot get cr"
	errUpdateDataStore = "cannot update datastore"
	errUpdateStatus    = "cannot update status"
)

type adder interface {
	Add(item interface{})
}

//+kubebuilder:rbac:groups=config.sdcio.dev,resources=configs,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups=config.sdcio.dev,resources=configs/status,verbs=get;update;patch

// SetupWithManager sets up the controller with the Manager.
func (r *reconciler) SetupWithManager(ctx context.Context, mgr ctrl.Manager, c interface{}) (map[schema.GroupVersionKind]chan event.GenericEvent, error) {
	cfg, ok := c.(*ctrlconfig.ControllerConfig)
	if !ok {
		return nil, fmt.Errorf("cannot initialize, expecting controllerConfig, got: %s", reflect.TypeOf(c).Name())
	}

	/*
		if err := invv1alpha1.AddToScheme(mgr.GetScheme()); err != nil {
			return nil, err
		}
	*/

	r.Client = mgr.GetClient()
	r.finalizer = resource.NewAPIFinalizer(mgr.GetClient(), finalizer)
	r.targetStore = cfg.TargetStore
	r.recorder = mgr.GetEventRecorderFor(controllerName)

	return nil, ctrl.NewControllerManagedBy(mgr).
		Named(controllerName).
		For(&configv1alpha1.Config{}).
		Watches(&invv1alpha1.Target{}, &targetEventHandler{client: mgr.GetClient()}).
		Complete(r)
}

type reconciler struct {
	client.Client
	finalizer   *resource.APIFinalizer
	targetStore storebackend.Storer[*target.Context]
	recorder    record.EventRecorder
}

func (r *reconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	ctx = ctrlconfig.InitContext(ctx, controllerName, req.NamespacedName)
	log := log.FromContext(ctx)
	log.Info("reconcile")

	cr := &configv1alpha1.Config{}
	if err := r.Get(ctx, req.NamespacedName, cr); err != nil {
		// if the resource no longer exists the reconcile loop is done
		if resource.IgnoreNotFound(err) != nil {
			log.Error(errGetCr, "error", err)
			return ctrl.Result{}, errors.Wrap(resource.IgnoreNotFound(err), errGetCr)
		}
		return ctrl.Result{}, nil
	}
	//log.Info("get object", "resourceVersion", cr.GetResourceVersion())
	cr = cr.DeepCopy()

	if !cr.GetDeletionTimestamp().IsZero() {
		//log.Info("delete")
		tctx, targetKey, err := r.getTargetInfo(ctx, cr)
		if err != nil {
			// Since the target is not available we delete the resource
			// The target config might not be deleted
			//log.Error("delete config with unavailable target", "error", err)
			if err := r.finalizer.RemoveFinalizer(ctx, cr); err != nil {
				//log.Error("cannot remove finalizer", "error", err)
				r.recorder.Eventf(cr, corev1.EventTypeWarning,
					"Error", "error %s", err.Error())
				return ctrl.Result{Requeue: true}, errors.Wrap(r.Update(ctx, cr), errUpdateStatus)
			}
			return ctrl.Result{}, nil
		}
		if !tctx.IsReady() {
			// Since the target is not available we delete the resource
			// The target config might not be deleted
			//log.Error("delete config with unavailable target", "error", err)
			if err := r.finalizer.RemoveFinalizer(ctx, cr); err != nil {
				//log.Error("cannot remove finalizer", "error", err)
				r.recorder.Eventf(cr, corev1.EventTypeWarning,
					"Error", "error %s", err.Error())
				return ctrl.Result{Requeue: true}, errors.Wrap(r.Update(ctx, cr), errUpdateStatus)
			}
			return ctrl.Result{}, nil
		}
		log.Info("config delete", "targetKey", targetKey, "tctx", tctx)
		if _, err := tctx.DeleteIntent(ctx, targetKey, cr, false); err != nil {
			//log.Error("delete intent failed", "error", err.Error())
			cr.SetConditions(configv1alpha1.Failed(err.Error()))
			r.recorder.Eventf(cr, corev1.EventTypeWarning,
				"Error", "delete error %s", err.Error())
			// all grpc errors except resource exhausted will not retry
			// and a human need to intervene
			if er, ok := status.FromError(err); ok {
				if er.Code() == codes.ResourceExhausted {
					return ctrl.Result{Requeue: true, RequeueAfter: 5 * time.Second}, errors.Wrap(r.Update(ctx, cr), errUpdateStatus)
				}
			}
			return ctrl.Result{}, errors.Wrap(r.Update(ctx, cr), errUpdateStatus)
		}

		if err := r.finalizer.RemoveFinalizer(ctx, cr); err != nil {
			//log.Error("cannot remove finalizer", "error", err)
			r.recorder.Eventf(cr, corev1.EventTypeWarning,
				"Error", "error %s", err.Error())
			return ctrl.Result{Requeue: true}, errors.Wrap(r.Update(ctx, cr), errUpdateStatus)
		}
		//log.Info("Successfully deleted resource")
		return ctrl.Result{}, nil
	}

	tctx, targetKey, err := r.getTargetInfo(ctx, cr)
	if err != nil {
		// we do not reconcile again since the input was invalid
		// validation already does some checks before accepting the object,
		// but there can still be errors.
		// The target watch should retrigger the reconciler
		cr.SetConditions(configv1alpha1.Failed(err.Error()))
		r.recorder.Eventf(cr, corev1.EventTypeWarning,
			"Error", "error %s", err.Error())
		return ctrl.Result{}, errors.Wrap(r.Update(ctx, cr), errUpdateStatus)
	}

	if err := r.finalizer.AddFinalizer(ctx, cr); err != nil {
		log.Debug("cannot add finalizer", "error", err)
		r.recorder.Eventf(cr, corev1.EventTypeWarning,
			"Error", "error %s", err.Error())
		return ctrl.Result{Requeue: true}, err
	}

	// check if we have to reapply the config
	// if condition is false -> reapply the config
	// if the applied Config is not set -> reapply the config
	// if the applied Config is different than the spec -> reapply the config
	// if the deviation is having the reason xx -> reapply the config
	if cr.GetCondition(configv1alpha1.ConditionTypeReady).Status == metav1.ConditionTrue &&
		cr.Status.AppliedConfig != nil &&
		cr.Spec.GetShaSum(ctx) == cr.Status.AppliedConfig.GetShaSum(ctx) &&
		!cr.Status.HasNotAppliedDeviation() {
		return ctrl.Result{}, nil
	}

	if _, err := tctx.SetIntent(ctx, targetKey, cr, true, false); err != nil {
		cr.SetConditions(configv1alpha1.Failed(err.Error()))
		r.recorder.Eventf(cr, corev1.EventTypeWarning,
			"Error", "error %s", err.Error())
		// all grpc errors except resource exhausted will not retry
		// and a human need to intervene
		if er, ok := status.FromError(err); ok {
			if er.Code() == codes.ResourceExhausted {
				return ctrl.Result{Requeue: true, RequeueAfter: 5 * time.Second}, errors.Wrap(r.Update(ctx, cr), errUpdateStatus)
			}
		}
		return ctrl.Result{}, errors.Wrap(r.Update(ctx, cr), errUpdateStatus)
	}

	cr.SetConditions(configv1alpha1.Ready())
	cr.Status.LastKnownGoodSchema = tctx.GetSchema()
	cr.Status.Deviations = []configv1alpha1.Deviation{} // reset deviations
	cr.Status.AppliedConfig = &cr.Spec
	r.recorder.Eventf(cr, corev1.EventTypeNormal,
		"config", "ready")
	return ctrl.Result{}, errors.Wrap(r.Update(ctx, cr), errUpdateStatus)
}

func (r *reconciler) getTargetInfo(ctx context.Context, cr *configv1alpha1.Config) (*target.Context, storebackend.Key, error) {
	targetKey, err := getTargetKey(cr.GetLabels())
	if err != nil {
		return nil, storebackend.Key{}, errors.Wrap(err, "target key invalid")
	}

	tctx, err := r.getTargetContext(ctx, targetKey)
	if err != nil {
		return nil, storebackend.Key{}, err
	}
	return tctx, targetKey, nil
}

func (r *reconciler) getTargetContext(ctx context.Context, targetKey storebackend.Key) (*target.Context, error) {
	target := &invv1alpha1.Target{}
	if err := r.Get(ctx, targetKey.NamespacedName, target); err != nil {
		return nil, err
	}
	if !target.IsConfigReady() {
		return nil, errors.New(string(configv1alpha1.ConditionReasonTargetNotReady))
	}
	tctx, err := r.targetStore.Get(ctx, targetKey)
	if err != nil {
		return nil, errors.New(string(configv1alpha1.ConditionReasonTargetNotFound))
	}
	return tctx, nil
}

func getTargetKey(labels map[string]string) (storebackend.Key, error) {
	var targetName, targetNamespace string
	if labels != nil {
		targetName = labels[configv1alpha1.TargetNameKey]
		targetNamespace = labels[configv1alpha1.TargetNamespaceKey]
	}
	if targetName == "" || targetNamespace == "" {
		return storebackend.Key{}, fmt.Errorf(" target namespace and name is required got %s.%s", targetNamespace, targetName)
	}
	return storebackend.Key{NamespacedName: types.NamespacedName{Namespace: targetNamespace, Name: targetName}}, nil
}
