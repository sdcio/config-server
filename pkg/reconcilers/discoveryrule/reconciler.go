package discoveryrule

import (
	"context"
	"fmt"

	invv1alpha1 "github.com/iptecharch/config-server/apis/inv/v1alpha1"
	"github.com/iptecharch/config-server/pkg/discovery/discoveryrule"
	"github.com/iptecharch/config-server/pkg/reconcilers"
	discoveryeventhandler "github.com/iptecharch/config-server/pkg/reconcilers/discoveryeventhandlers"
	"github.com/iptecharch/config-server/pkg/reconcilers/resource"
	"github.com/iptecharch/config-server/pkg/store"
	memstore "github.com/iptecharch/config-server/pkg/store/memory"
	errors "github.com/pkg/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/event"
	"sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/source"
)

func init() {
	reconcilers.Register("discoveryrule", &reconciler{})
}

const (
	finalizer = "discoveryruleip.inv.sdcio.dev/finalizer"
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
	/*
		cfg, ok := c.(*ctrlconfig.ControllerConfig)
		if !ok {
			return nil, fmt.Errorf("cannot initialize, expecting controllerConfig, got: %s", reflect.TypeOf(c).Name())
		}
	*/

	if err := invv1alpha1.AddToScheme(mgr.GetScheme()); err != nil {
		return nil, err
	}

	r.Client = mgr.GetClient()
	r.finalizer = resource.NewAPIFinalizer(mgr.GetClient(), finalizer)
	r.discoveryStore = memstore.NewStore[discoveryrule.DiscoveryRule]()

	return nil, ctrl.NewControllerManagedBy(mgr).
		Named("DiscoveryRuleIPController").
		For(&invv1alpha1.DiscoveryRule{}).
		//Owns(&invv1alpha1.DiscoveryRule{}).
		Watches(&source.Kind{
			Type: &invv1alpha1.TargetConnectionProfile{}},
			&discoveryeventhandler.TargetConnProfileEventHandler{
				Client:  mgr.GetClient(),
				ObjList: &invv1alpha1.DiscoveryRuleList{},
			}).
		Watches(&source.Kind{Type: &invv1alpha1.TargetSyncProfile{}},
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
}

func (r *reconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	log := log.FromContext(ctx).WithValues("req", req)
	log.Info("reconcile")

	key := store.GetNSNKey(req.NamespacedName)

	cr := &invv1alpha1.DiscoveryRule{}
	if err := r.Get(ctx, req.NamespacedName, cr); err != nil {
		// if the resource no longer exists the reconcile loop is done
		if resource.IgnoreNotFound(err) != nil {
			log.Error(err, errGetCr)
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
				log.Error(err, "cannot remove finalizer")
				cr.SetConditions(invv1alpha1.Failed(err.Error()))
				return ctrl.Result{Requeue: true}, errors.Wrap(r.Status().Update(ctx, cr), errUpdateStatus)
			}
			log.Info("Successfully deleted resource, with non existing client -> strange")
			return ctrl.Result{}, nil
		}
		// stop and delete the discovery rule
		dr.Stop(ctx)
		if err := r.discoveryStore.Delete(ctx, key); err != nil {
			log.Error(err, "cannot delete discovery rule from store")
			cr.SetConditions(invv1alpha1.Failed(err.Error()))
			return ctrl.Result{Requeue: true}, errors.Wrap(r.Status().Update(ctx, cr), errUpdateStatus)
		}
		// remove the finalizer
		if err := r.finalizer.RemoveFinalizer(ctx, cr); err != nil {
			log.Error(err, "cannot remove finalizer")
			cr.SetConditions(invv1alpha1.Failed(err.Error()))
			return ctrl.Result{Requeue: true}, errors.Wrap(r.Status().Update(ctx, cr), errUpdateStatus)
		}
		log.Info("Successfully deleted resource")
		return ctrl.Result{}, nil
	}

	if err := cr.Validate(); err != nil {
		log.Error(err, "validation failed")
		cr.SetConditions(invv1alpha1.Failed(err.Error()))
		return ctrl.Result{Requeue: true}, errors.Wrap(r.Status().Update(ctx, cr), errUpdateStatus)
	}

	if err := r.finalizer.AddFinalizer(ctx, cr); err != nil {
		log.Error(err, "cannot add finalizer")
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
		log.Error(err, "cannot get discovery rule config")
		if isDRRunning {
			// we stop the discovery rule
			dr.Stop(ctx)
			if err := r.discoveryStore.Delete(ctx, key); err != nil { // we don't fail
				log.Error(err, "cannot delete discovery rule from store")
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
			log.Error(err, "cannot delete discovery rule from store")
		}
	}
	// create a new discoveryRule with the latest parameters
	dr = discoveryrule.New(r.Client, newDRConfig)
	// new discovery initialization -> create or update (we deleted the DRConfig before)
	if err := r.discoveryStore.Create(ctx, key, dr); err != nil {
		log.Error(err, "cannot add dr ")
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
