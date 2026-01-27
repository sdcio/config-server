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

package targetconfigserver

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
	"github.com/sdcio/config-server/pkg/reconcilers/eventhandler"
	"github.com/sdcio/config-server/pkg/reconcilers/resource"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/discovery"
	"k8s.io/client-go/tools/record"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/event"
	targetmanager "github.com/sdcio/config-server/pkg/sdc/target/manager"
)

func init() {
	reconcilers.Register(crName, &reconciler{})
}

const (
	crName                = "targetconfigserver"
	fieldmanagerfinalizer = "targetconfigfinalizerr"
	reconcilerName        = "TargetConfigServerController"
	finalizer             = "targetconfigserver.inv.sdcio.dev/finalizer"
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
	var err error
	r.discoveryClient, err = ctrlconfig.GetDiscoveryClient(mgr)
	if err != nil {
		return nil, fmt.Errorf("cannot get discoveryClient from manager")
	}

	if cfg.TargetManager == nil {
		return nil, fmt.Errorf("TargetManager is nil: set LOCAL_DATASERVER=true or disable TargetConfigServerController")
	}

	r.client = mgr.GetClient()
	r.finalizer = resource.NewAPIFinalizer(mgr.GetClient(), finalizer, reconcilerName)
	r.targetMgr = cfg.TargetManager
	r.recorder = mgr.GetEventRecorderFor(reconcilerName)
	r.transactor = targetmanager.NewTransactor(r.client, crName, fieldmanagerfinalizer)

	return nil, ctrl.NewControllerManagedBy(mgr).
		Named(reconcilerName).
		For(&invv1alpha1.Target{}).
		Watches(&configv1alpha1.Config{}, &eventhandler.ConfigForTargetEventHandler{Client: mgr.GetClient(), ControllerName: reconcilerName}).
		Watches(&configv1alpha1.Deviation{}, &eventhandler.DeviationForTargetEventHandler{Client: mgr.GetClient(), ControllerName: reconcilerName}).
		Complete(r)
}

type reconciler struct {
	client client.Client
	discoveryClient *discovery.DiscoveryClient
	finalizer       *resource.APIFinalizer
	targetMgr       *targetmanager.TargetManager
	recorder        record.EventRecorder
	transactor      *targetmanager.Transactor
}

func (r *reconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	ctx = ctrlconfig.InitContext(ctx, reconcilerName, req.NamespacedName)
	log := log.FromContext(ctx)
	log.Info("reconcile")

	if _, err := r.discoveryClient.ServerResourcesForGroupVersion(configv1alpha1.SchemeGroupVersion.String()); err != nil {
		log.Info("API group not available, retrying...", "groupversion", configv1alpha1.SchemeGroupVersion.String())
		return ctrl.Result{RequeueAfter: 10 * time.Second}, nil
	}

	targetKey := storebackend.KeyFromNSN(req.NamespacedName)

	target := &invv1alpha1.Target{}
	if err := r.client.Get(ctx, req.NamespacedName, target); err != nil {
		// if the resource no longer exists the reconcile loop is done
		if resource.IgnoreNotFound(err) != nil {
			log.Error(errGetCr, "error", err)
			return ctrl.Result{}, errors.Wrap(resource.IgnoreNotFound(err), errGetCr)
		}
		return ctrl.Result{}, nil
	}

	targetOrig := target.DeepCopy()
	if !target.GetDeletionTimestamp().IsZero() {
		return ctrl.Result{}, nil
	}

	dsctx, ok := r.targetMgr.GetDatastore(ctx, targetKey)
	if !ok || dsctx == nil {
		return ctrl.Result{RequeueAfter: 5 * time.Second},
			errors.Wrap(r.handleError(ctx, targetOrig,
				"target runtime not ready (no dsctx yet)",
				nil),
			errUpdateStatus)
	}

	if dsctx.Client == nil {
		return ctrl.Result{RequeueAfter: 5 * time.Second},
			errors.Wrap(r.handleError(ctx, targetOrig,
				fmt.Sprintf("target runtime not ready phase=%s dsReady=%t dsStoreReady=%t recovered=%t err=%v",
					dsctx.Status.Phase, dsctx.Status.DSReady, dsctx.Status.DSStoreReady, dsctx.Status.Recovered, dsctx.Status.LastError),
				nil),
			errUpdateStatus)
	}
	

	// if the config is not receovered we stop the reconcile loop
	if !dsctx.Status.Recovered {
		log.Info("config transaction -> target not recovered yet")
		return ctrl.Result{}, nil
	}

	retry, err := r.transactor.Transact(ctx, target, dsctx)
	if err != nil {
		log.Warn("config transaction failed", "retry", retry, "err", err)
		if retry {
			return ctrl.Result{
				RequeueAfter: 500 * time.Millisecond,
				Requeue: true,
			}, 
			errors.Wrap(r.handleError(ctx, targetOrig, "", err), errUpdateStatus)	
		}
		return ctrl.Result{}, errors.Wrap(r.handleError(ctx, targetOrig, "", err), errUpdateStatus)
	}
	log.Info("config transaction success", "retry", retry)
	if retry {
			return ctrl.Result{
				RequeueAfter: 500 * time.Millisecond,
				Requeue: true,
			}, 
			errors.Wrap(r.handleSuccess(ctx, targetOrig), errUpdateStatus)
		}
	return ctrl.Result{}, errors.Wrap(r.handleSuccess(ctx, targetOrig), errUpdateStatus)
}

func (r *reconciler) handleSuccess(ctx context.Context, target *invv1alpha1.Target) error {
	log := log.FromContext(ctx)
	log.Debug("handleSuccess", "key", target.GetNamespacedName(), "status old", target.DeepCopy().Status)
	return nil
	
}

func (r *reconciler) handleError(ctx context.Context, target *invv1alpha1.Target, msg string, err error) error {
	log := log.FromContext(ctx)

	if err != nil {
		msg = fmt.Sprintf("%s err %s", msg, err.Error())
	}

	log.Warn("config transaction failed", "msg", msg, "err", err)
	return nil
	
}

