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

package discoveryrule

import (
	"context"
	"fmt"
	"reflect"

	"github.com/henderiw/apiserver-store/pkg/storebackend"
	memstore "github.com/henderiw/apiserver-store/pkg/storebackend/memory"
	"github.com/henderiw/logger/log"
	errors "github.com/pkg/errors"
	invv1alpha1 "github.com/sdcio/config-server/apis/inv/v1alpha1"
	"github.com/sdcio/config-server/pkg/discovery/discoveryrule"
	"github.com/sdcio/config-server/pkg/reconcilers"
	"github.com/sdcio/config-server/pkg/reconcilers/ctrlconfig"
	discoveryeventhandler "github.com/sdcio/config-server/pkg/reconcilers/discoveryeventhandlers"
	"github.com/sdcio/config-server/pkg/reconcilers/resource"
	"github.com/sdcio/config-server/pkg/target"
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
	crName         = "discoveryrule"
	controllerName = "DiscoveryRuleController"
	finalizer      = "discoveryrule.inv.sdcio.dev/finalizer"
	// errors
	errGetCr        = "cannot get cr"
	errUpdateStatus = "cannot update status"
)

type adder interface {
	Add(item interface{})
}

//+kubebuilder:rbac:groups=inv.sdcio.dev,resources=discoveryrules,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups=inv.sdcio.dev,resources=discoveryrules/status,verbs=get;update;patch

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
	r.discoveryStore = memstore.NewStore[discoveryrule.DiscoveryRule]()
	r.targetStore = cfg.TargetStore
	r.recorder = mgr.GetEventRecorderFor(controllerName)

	return nil, ctrl.NewControllerManagedBy(mgr).
		Named(controllerName).
		For(&invv1alpha1.DiscoveryRule{}).
		//Owns(&invv1alpha1.DiscoveryRule{}).
		Watches(&invv1alpha1.TargetConnectionProfile{},
			&discoveryeventhandler.TargetConnProfileEventHandler{
				Client:  mgr.GetClient(),
				ObjList: &invv1alpha1.DiscoveryRuleList{},
			}).
		Watches(&invv1alpha1.TargetSyncProfile{},
			&discoveryeventhandler.TargetSyncProfileEventHandler{
				Client:  mgr.GetClient(),
				ObjList: &invv1alpha1.DiscoveryRuleList{},
			}).
		Watches(&corev1.Secret{}, &secretEventHandler{client: mgr.GetClient()}).
		Complete(r)
}

type reconciler struct {
	client.Client
	finalizer *resource.APIFinalizer

	discoveryStore storebackend.Storer[discoveryrule.DiscoveryRule]
	targetStore    storebackend.Storer[*target.Context]
	recorder       record.EventRecorder
}

func (r *reconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	ctx = ctrlconfig.InitContext(ctx, controllerName, req.NamespacedName)
	log := log.FromContext(ctx)
	log.Info("reconcile")

	key := storebackend.KeyFromNSN(req.NamespacedName)

	cr := &invv1alpha1.DiscoveryRule{}
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
		// check if this is the last one -> if so stop the client to the dataserver
		dr, err := r.discoveryStore.Get(ctx, key)
		if err != nil {
			// discovery rule does not exist
			if err := r.finalizer.RemoveFinalizer(ctx, cr); err != nil {
				r.handleError(ctx, cr, "cannot remove finalizer", err)
				return ctrl.Result{Requeue: true}, errors.Wrap(r.Status().Update(ctx, cr), errUpdateStatus)
			}
			//log.Info("Successfully deleted resource, with non existing client -> strange")
			return ctrl.Result{}, nil
		}
		// stop and delete the discovery rule
		dr.Stop(ctx)
		if err := r.discoveryStore.Delete(ctx, key); err != nil {
			r.handleError(ctx, cr, "cannot delete discovery rule from store", err)
			return ctrl.Result{Requeue: true}, errors.Wrap(r.Status().Update(ctx, cr), errUpdateStatus)
		}
		// remove the finalizer
		if err := r.finalizer.RemoveFinalizer(ctx, cr); err != nil {
			r.handleError(ctx, cr, "cannot remove finalizer", err)
			return ctrl.Result{Requeue: true}, errors.Wrap(r.Status().Update(ctx, cr), errUpdateStatus)
		}
		//log.Info("Successfully deleted resource")
		return ctrl.Result{}, nil
	}

	if err := cr.Validate(); err != nil {
		r.handleError(ctx, cr, "validation failed", err)
		return ctrl.Result{Requeue: true}, errors.Wrap(r.Status().Update(ctx, cr), errUpdateStatus)
	}

	if err := r.finalizer.AddFinalizer(ctx, cr); err != nil {
		r.handleError(ctx, cr, "cannot add finalizer", err)
		return ctrl.Result{Requeue: true}, errors.Wrap(r.Status().Update(ctx, cr), errUpdateStatus)
	}
	// check if the discovery rule is running
	isDRRunning := false
	dr, err := r.discoveryStore.Get(ctx, key)
	if err == nil {
		isDRRunning = true
	}

	// gather the references from the CR in a normalized format
	newDRConfig, err := r.getDRConfig(ctx, cr)
	if err != nil {
		//log.Error("cannot get discovery rule config", "error", err)
		if isDRRunning {
			// we stop the discovery rule
			dr.Stop(ctx)
			if err := r.discoveryStore.Delete(ctx, key); err != nil { // we don't fail
				log.Error("cannot delete discovery rule from store", "error", err)
			}
		}
		cr.Status.StartTime = metav1.Time{}
		r.handleError(ctx, cr, "cannot create discovery rule in discovery store", err)
		cr.SetConditions(invv1alpha1.Failed(err.Error()))
		r.recorder.Eventf(cr, corev1.EventTypeNormal,
			"discoveryRule", "ready")
		return ctrl.Result{}, errors.Wrap(r.Status().Update(ctx, cr), errUpdateStatus) // do not requeue
	}

	if isDRRunning {
		currentDRConfig := dr.GetDiscoveryRulConfig()
		if !r.HasChanged(ctx, newDRConfig, currentDRConfig) {
			//log.Info("refs -> no change")
			cr.SetConditions(invv1alpha1.Ready())
			r.recorder.Eventf(cr, corev1.EventTypeNormal,
				"discoveryRule", "ready")
			return ctrl.Result{}, errors.Wrap(r.Status().Update(ctx, cr), errUpdateStatus)
		}
		//log.Info("refs -> changed")
		// we stop the discovery rule
		dr.Stop(ctx)
		if err = r.discoveryStore.Delete(ctx, key); err != nil {
			// we dont fail
			//log.Error("cannot delete discovery rule from store", "error", err)
			r.recorder.Eventf(cr, corev1.EventTypeWarning,
				"Error", "error %s", err.Error())
		}
	}
	// create a new discoveryRule with the latest parameters
	dr = discoveryrule.New(r.Client, newDRConfig, r.targetStore)
	// new discovery initialization -> create or update (we deleted the DRConfig before)
	if err := r.discoveryStore.Create(ctx, key, dr); err != nil {
		r.handleError(ctx, cr, "cannot create discovery rule in discovery store", err)
		// given this is a ummutable field this means the CR will have to be deleted/recreated
		return ctrl.Result{Requeue: true}, errors.Wrap(r.Status().Update(ctx, cr), errUpdateStatus)
	}

	go dr.Run(ctx)

	// update discovery rule start time
	cr.Status.StartTime = metav1.Now()
	cr.SetConditions(invv1alpha1.Ready())
	r.recorder.Eventf(cr, corev1.EventTypeNormal,
		"discoveryRule", "ready")
	return ctrl.Result{}, errors.Wrap(r.Status().Update(ctx, cr), errUpdateStatus)
}

func (r *reconciler) handleError(ctx context.Context, dr *invv1alpha1.DiscoveryRule, msg string, err error) {
	log := log.FromContext(ctx)
	if err == nil {
		dr.SetConditions(invv1alpha1.Failed(msg))
		log.Error(msg)
		r.recorder.Eventf(dr, corev1.EventTypeWarning, crName, msg)
	} else {
		dr.SetConditions(invv1alpha1.Failed(err.Error()))
		log.Error(msg, "error", err)
		r.recorder.Eventf(dr, corev1.EventTypeWarning, crName, fmt.Sprintf("%s, err: %s", msg, err.Error()))
	}
}

func (r *reconciler) HasChanged(ctx context.Context, newDRConfig, currentDRConfig *discoveryrule.DiscoveryRuleConfig) bool {
	log := log.FromContext(ctx)

	// if the resource version changed the config has changed

	if newDRConfig.DiscoveryProfile != nil {
		log.Info("HasChanged",
			"CR RV", fmt.Sprintf("%s/%s",
				newDRConfig.CR.GetResourceVersion(),
				currentDRConfig.CR.GetResourceVersion(),
			),
			"DiscoveryProfile Secret RV", fmt.Sprintf("%s/%s",
				newDRConfig.DiscoveryProfile.SecretResourceVersion,
				currentDRConfig.DiscoveryProfile.SecretResourceVersion,
			),
			"DiscoveryProfile Conn Profile len", fmt.Sprintf("%d/%d",
				len(newDRConfig.DiscoveryProfile.Connectionprofiles),
				len(currentDRConfig.DiscoveryProfile.Connectionprofiles),
			),
		)
	} else {
		log.Info("HasChanged",
			"CR RV", fmt.Sprintf("%s/%s",
				newDRConfig.CR.GetResourceVersion(),
				currentDRConfig.CR.GetResourceVersion(),
			),
		)
	}
	if newDRConfig.CR.GetResourceVersion() != currentDRConfig.CR.GetResourceVersion() {
		return true
	}

	if newDRConfig.DiscoveryProfile != nil {
		// Validate Discovery profile
		if newDRConfig.DiscoveryProfile.SecretResourceVersion != currentDRConfig.DiscoveryProfile.SecretResourceVersion {
			return true
		}
		// check if a reference has changed
		if len(newDRConfig.DiscoveryProfile.Connectionprofiles) != len(currentDRConfig.DiscoveryProfile.Connectionprofiles) {
			return true
		}
		for idx, newConnProfile := range newDRConfig.DiscoveryProfile.Connectionprofiles {
			if newConnProfile.ResourceVersion != currentDRConfig.DiscoveryProfile.Connectionprofiles[idx].ResourceVersion {
				return true
			}
		}
	}

	if len(newDRConfig.TargetConnectionProfiles) != len(currentDRConfig.TargetConnectionProfiles) {
		return true
	}
	for i := range newDRConfig.TargetConnectionProfiles {
		// Validate Target Connetcion profiles profile
		if newDRConfig.TargetConnectionProfiles[i].SecretResourceVersion != currentDRConfig.TargetConnectionProfiles[i].SecretResourceVersion {
			return true
		}
		if newDRConfig.TargetConnectionProfiles[i].Connectionprofile.ResourceVersion != currentDRConfig.TargetConnectionProfiles[i].Connectionprofile.ResourceVersion {
			return true
		}
		if newDRConfig.TargetConnectionProfiles[i].Syncprofile.ResourceVersion != currentDRConfig.TargetConnectionProfiles[i].Syncprofile.ResourceVersion {
			return true
		}
	}

	return false
}
