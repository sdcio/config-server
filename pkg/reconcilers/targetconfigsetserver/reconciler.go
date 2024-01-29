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

package targetconfigsetserver

import (
	"context"
	"fmt"
	"reflect"

	"github.com/pkg/errors"
	"k8s.io/apimachinery/pkg/apis/meta/internalversion"
	"k8s.io/apimachinery/pkg/fields"
	"k8s.io/apimachinery/pkg/runtime/schema"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/event"
	"github.com/henderiw/logger/log"

	configv1alpha1 "github.com/iptecharch/config-server/apis/config/v1alpha1"
	invv1alpha1 "github.com/iptecharch/config-server/apis/inv/v1alpha1"
	"github.com/iptecharch/config-server/pkg/configserver"
	"github.com/iptecharch/config-server/pkg/lease"
	"github.com/iptecharch/config-server/pkg/reconcilers"
	"github.com/iptecharch/config-server/pkg/reconcilers/ctrlconfig"
	"github.com/iptecharch/config-server/pkg/reconcilers/resource"
	"github.com/iptecharch/config-server/pkg/store"
	"github.com/iptecharch/config-server/pkg/target"
	genericapirequest "k8s.io/apiserver/pkg/endpoints/request"
)

func init() {
	reconcilers.Register("targetconfigSetserver", &reconciler{})
}

const (
	finalizer = "targetconfigsetserver.inv.sdcio.dev/finalizer"
	// errors
	errGetCr           = "cannot get cr"
	errUpdateDataStore = "cannot update datastore"
	errUpdateStatus    = "cannot update status"
)

//+kubebuilder:rbac:groups=inv.sdcio.dev,resources=targets,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups=inv.sdcio.dev,resources=targets/status,verbs=get;update;patch

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
	r.configSetProvider = cfg.ConfigSetProvider
	r.targetStore = cfg.TargetStore

	return nil, ctrl.NewControllerManagedBy(mgr).
		Named("TargetConfigSetServerController").
		For(&invv1alpha1.Target{}).
		Complete(r)
}

type reconciler struct {
	client.Client
	finalizer *resource.APIFinalizer

	configSetProvider configserver.ResourceProvider
	//targetTransitionStore store.Storer[bool] // keeps track of the target status locally
	targetStore store.Storer[target.Context]
}

func (r *reconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	log := log.FromContext(ctx).With("req", req)
	log.Info("reconcile")

	targetKey := store.KeyFromNSN(req.NamespacedName)

	cr := &invv1alpha1.Target{}
	if err := r.Get(ctx, req.NamespacedName, cr); err != nil {
		// if the resource no longer exists the reconcile loop is done
		if resource.IgnoreNotFound(err) != nil {
			log.Error(errGetCr, "error", err)
			return ctrl.Result{}, errors.Wrap(resource.IgnoreNotFound(err), errGetCr)
		}
		return ctrl.Result{}, nil
	}

	cr = cr.DeepCopy()

	if !cr.GetDeletionTimestamp().IsZero() {
		// list the configs per target
		configSetList, err := r.listConfigSets(ctx, cr)
		if err != nil {
			log.Error("cannot list configSets", "error", err)
			return ctrl.Result{Requeue: true}, err
		}
		for _, configset := range configSetList.Items {
			if err := r.configSetProvider.Apply(ctx, store.Key{}, store.Key{}, nil, &configset); err != nil {
				log.Error("canot apply configSets", "confifSetName", configset.Name, "error", err)
				return ctrl.Result{Requeue: true}, err
			}
		}
		if err := r.finalizer.RemoveFinalizer(ctx, cr); err != nil {
			log.Error("cannot remove finalizer", "error", err)
			return ctrl.Result{Requeue: true}, err
		}
		log.Info("Successfully deleted resource")
		return ctrl.Result{}, nil
	}

	if err := r.finalizer.AddFinalizer(ctx, cr); err != nil {
		log.Error("cannot add finalizer", "error", err)
		return ctrl.Result{Requeue: true}, err
	}

	l := r.getLease(ctx, targetKey)
	if err := l.AcquireLease(ctx, "TargetConfigSetServerController"); err != nil {
		log.Info("cannot acquire lease", "targetKey", targetKey.String(), "error", err.Error())
		return ctrl.Result{Requeue: true, RequeueAfter: lease.RequeueInterval}, nil
	}

	configSetList, err := r.listConfigSets(ctx, cr)
	if err != nil {
		log.Error("cannot list configSets", "error", err)
		return ctrl.Result{Requeue: true}, err
	}
	for _, configset := range configSetList.Items {
		if err := r.configSetProvider.Apply(ctx, store.Key{}, store.Key{}, nil, &configset); err != nil {
			log.Error("canot apply configSets", "confifSetName", configset.Name, "error", err)
			return ctrl.Result{Requeue: true}, err
		}
	}

	return ctrl.Result{}, nil
}

func (r *reconciler) listConfigSets(ctx context.Context, cr *invv1alpha1.Target) (*configv1alpha1.ConfigSetList, error) {
	ctx = genericapirequest.WithNamespace(ctx, cr.GetNamespace())

	obj, err := r.configSetProvider.List(ctx, &internalversion.ListOptions{
		FieldSelector: fields.OneTermEqualSelector("metadata.namespace", cr.GetNamespace()),
	})
	if err != nil {
		return nil, err
	}
	configSetList, ok := obj.(*configv1alpha1.ConfigSetList)
	if !ok {
		return nil, fmt.Errorf("listConfigSets, unexpected object, wanted %s, got : %s",
			reflect.TypeOf(configv1alpha1.ConfigSetList{}).Name(),
			reflect.TypeOf(obj).Name(),
		)
	}
	return configSetList, nil
}

func (r *reconciler) getLease(ctx context.Context, targetKey store.Key) lease.Lease {
	tctx, err := r.targetStore.Get(ctx, targetKey)
	if err != nil {
		lease := lease.New(r.Client, targetKey.NamespacedName)
		r.targetStore.Create(ctx, targetKey, target.Context{Lease: lease})
		return lease
	}
	if tctx.Lease == nil {
		lease := lease.New(r.Client, targetKey.NamespacedName)
		tctx.Lease = lease
		r.targetStore.Update(ctx, targetKey, target.Context{Lease: lease})
		return lease
	}
	return tctx.Lease
}
