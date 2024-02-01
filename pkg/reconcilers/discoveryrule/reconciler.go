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

	invv1alpha1 "github.com/iptecharch/config-server/apis/inv/v1alpha1"
	"github.com/iptecharch/config-server/pkg/discovery/discoveryrule"
	"github.com/iptecharch/config-server/pkg/reconcilers"
	"github.com/iptecharch/config-server/pkg/reconcilers/ctrlconfig"
	discoveryeventhandler "github.com/iptecharch/config-server/pkg/reconcilers/discoveryeventhandlers"
	"github.com/iptecharch/config-server/pkg/reconcilers/resource"
	"github.com/iptecharch/config-server/pkg/store"
	memstore "github.com/iptecharch/config-server/pkg/store/memory"
	"github.com/iptecharch/config-server/pkg/target"
	errors "github.com/pkg/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/event"
	"github.com/henderiw/logger/log"
)

func init() {
	reconcilers.Register("discoveryrule", &reconciler{})
}

const (
	controllerName = "DiscoveryRuleController"
	finalizer = "discoveryruleip.inv.sdcio.dev/finalizer"
	// errors
	errGetCr        = "cannot get cr"
	errUpdateStatus = "cannot update status"
)

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
		Complete(r)
}

type reconciler struct {
	client.Client
	finalizer *resource.APIFinalizer

	discoveryStore store.Storer[discoveryrule.DiscoveryRule]
	targetStore    store.Storer[target.Context]
}

func (r *reconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	ctx = ctrlconfig.InitContext(ctx, controllerName, req.NamespacedName)
	log := log.FromContext(ctx)
	log.Info("reconcile")

	key := store.KeyFromNSN(req.NamespacedName)

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
				log.Error("cannot remove finalizer", "error", err)
				cr.SetConditions(invv1alpha1.Failed(err.Error()))
				return ctrl.Result{Requeue: true}, errors.Wrap(r.Status().Update(ctx, cr), errUpdateStatus)
			}
			log.Info("Successfully deleted resource, with non existing client -> strange")
			return ctrl.Result{}, nil
		}
		// stop and delete the discovery rule
		dr.Stop(ctx)
		if err := r.discoveryStore.Delete(ctx, key); err != nil {
			log.Error("cannot delete discovery rule from store", "error", err)
			cr.SetConditions(invv1alpha1.Failed(err.Error()))
			return ctrl.Result{Requeue: true}, errors.Wrap(r.Status().Update(ctx, cr), errUpdateStatus)
		}
		// remove the finalizer
		if err := r.finalizer.RemoveFinalizer(ctx, cr); err != nil {
			log.Error("cannot remove finalizer", "error", err)
			cr.SetConditions(invv1alpha1.Failed(err.Error()))
			return ctrl.Result{Requeue: true}, errors.Wrap(r.Status().Update(ctx, cr), errUpdateStatus)
		}
		log.Info("Successfully deleted resource")
		return ctrl.Result{}, nil
	}

	if err := cr.Validate(); err != nil {
		log.Error("validation failed", "error", err)
		cr.SetConditions(invv1alpha1.Failed(err.Error()))
		return ctrl.Result{Requeue: true}, errors.Wrap(r.Status().Update(ctx, cr), errUpdateStatus)
	}

	if err := r.finalizer.AddFinalizer(ctx, cr); err != nil {
		log.Error("cannot add finalizer", "error", err)
		cr.SetConditions(invv1alpha1.Failed(err.Error()))
		return ctrl.Result{Requeue: true}, errors.Wrap(r.Status().Update(ctx, cr), errUpdateStatus)
	}
	// check if the discovery rule is running
	isDRRunning := false
	dr, err := r.discoveryStore.Get(ctx, key)
	if err == nil {
		isDRRunning = true
	}

	// gather the referencesm from the CR in a normalized format
	newDRConfig, err := r.getDRConfig(ctx, cr)
	if err != nil {
		log.Error("cannot get discovery rule config", "error", err)
		if isDRRunning {
			// we stop the discovery rule
			dr.Stop(ctx)
			if err := r.discoveryStore.Delete(ctx, key); err != nil { // we don't fail
				log.Error("cannot delete discovery rule from store", "error", err)
			}
		}
		cr.Status.StartTime = metav1.Time{}
		cr.SetConditions(invv1alpha1.Failed(err.Error()))
		return ctrl.Result{}, errors.Wrap(r.Status().Update(ctx, cr), errUpdateStatus) // do not requeue
	}

	if isDRRunning {
		currentDRConfig := dr.GetDiscoveryRulConfig()
		if !r.HasChanged(ctx, newDRConfig, currentDRConfig) {
			log.Info("refs -> no change")
			cr.SetConditions(invv1alpha1.Ready())
			return ctrl.Result{}, errors.Wrap(r.Status().Update(ctx, cr), errUpdateStatus)
		}
		log.Info("refs -> changed")
		// we stop the discovery rule
		dr.Stop(ctx)
		if err = r.discoveryStore.Delete(ctx, key); err != nil {
			// we dont fail
			log.Error("cannot delete discovery rule from store", "error", err)
		}
	}
	// create a new discoveryRule with the latest parameters
	dr = discoveryrule.New(r.Client, newDRConfig, r.targetStore)
	// new discovery initialization -> create or update (we deleted the DRConfig before)
	if err := r.discoveryStore.Create(ctx, key, dr); err != nil {
		log.Error("cannot add dr", "error", err)
		cr.SetConditions(invv1alpha1.Failed(err.Error()))
		// given this is a ummutable field this means the CR will have to be deleted/recreated
		return ctrl.Result{Requeue: true}, errors.Wrap(r.Status().Update(ctx, cr), errUpdateStatus)
	}

	go dr.Run(ctx)

	// update discovery rule start time
	cr.Status.StartTime = metav1.Now()
	cr.SetConditions(invv1alpha1.Ready())
	return ctrl.Result{}, errors.Wrap(r.Status().Update(ctx, cr), errUpdateStatus)
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
