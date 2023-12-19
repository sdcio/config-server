package discoveryrule
/*
import (
	"context"
	"fmt"
	"time"

	invv1alpha1 "github.com/iptecharch/config-server/apis/inv/v1alpha1"
	"github.com/iptecharch/config-server/pkg/discovery/discoveryrule"
	"github.com/iptecharch/config-server/pkg/reconcilers"
	"github.com/iptecharch/config-server/pkg/reconcilers/resource"
	"github.com/iptecharch/config-server/pkg/store"
	memstore "github.com/iptecharch/config-server/pkg/store/memory"
	"github.com/pkg/errors"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
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
	finalizer = "discoveryrule.inv.sdcio.dev/finalizer"
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

	if err := invv1alpha1.AddToScheme(mgr.GetScheme()); err != nil {
		return nil, err
	}

	r.Client = mgr.GetClient()
	r.finalizer = resource.NewAPIFinalizer(mgr.GetClient(), finalizer)
	r.discoveryStore = memstore.NewStore[discoveryrule.DiscoveryRule]()

	return nil, ctrl.NewControllerManagedBy(mgr).
		Named("DiscoveryRuleController").
		For(&invv1alpha1.DiscoveryRule{}).
		//Owns(&invv1alpha1.DiscoveryRule{}).
		Watches(&source.Kind{Type: &invv1alpha1.TargetConnectionProfile{}}, &targetConnProfileEventHandler{client: mgr.GetClient()}).
		Watches(&source.Kind{Type: &invv1alpha1.TargetSyncProfile{}}, &targetSyncProfileEventHandler{client: mgr.GetClient()}).
		Watches(&source.Kind{Type: &invv1alpha1.DiscoveryRuleIPRange{}}, &drIPRangeEventHandler{client: mgr.GetClient()}).
		Watches(&source.Kind{Type: &invv1alpha1.DiscoveryRuleStatic{}}, &drStaticEventHandler{client: mgr.GetClient()}).
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

	// gather the referencesm from the CR
	drGVK, drCtx, err := r.getReferences(ctx, cr)
	if err != nil {
		log.Error(err, "cannot get reference context")
		if isDRRunning {
			// we stop the discovery rule
			dr.Stop(ctx)
			if err := r.discoveryStore.Delete(ctx, key); err != nil {
				// we dont fail
				log.Error(err, "cannot delete discovery rule from store")
			}
		}
		cr.Status.StartTime = metav1.Time{}
		cr.Status.UsedReferences = nil
		cr.SetConditions(invv1alpha1.Failed(err.Error()))
		return ctrl.Result{RequeueAfter: 1 * time.Second}, errors.Wrap(r.Status().Update(ctx, cr), errUpdateStatus)
	}
	// based on the gvk check if the gvk is supported
	drInit, ok := discoveryrule.DiscoveryRules[*drGVK]
	if !ok {
		log.Info("cannot initialize discovery rule, gvk not registered", "gvk", *drGVK)
		// we stop the discovery rule
		dr.Stop(ctx)
		if err := r.discoveryStore.Delete(ctx, key); err != nil {
			// we dont fail
			log.Error(err, "cannot delete discovery rule from store")
		}
		cr.Status.StartTime = metav1.Time{}
		cr.Status.UsedReferences = nil
		cr.SetConditions(invv1alpha1.Failed("cannot initialize discovery rule, gvk not registered"))
		return ctrl.Result{}, errors.Wrap(r.Status().Update(ctx, cr), errUpdateStatus)
	}

	if isDRRunning {
		drKey := types.NamespacedName{Namespace: cr.GetNamespace(), Name: cr.Spec.DiscoveryRuleRef.Name}
		drRuleResourceVersion, err := dr.Get(ctx, drKey)
		if err != nil {
			// we stop the discovery rule
			dr.Stop(ctx)
			if err := r.discoveryStore.Delete(ctx, key); err != nil {
				// we dont fail
				log.Error(err, "cannot delete discovery rule from store")
			}
			cr.Status.StartTime = metav1.Time{}
			cr.Status.UsedReferences = nil
			cr.SetConditions(invv1alpha1.Failed(err.Error()))
			return ctrl.Result{}, errors.Wrap(r.Status().Update(ctx, cr), errUpdateStatus)
		}
		if !r.HasReferencesChanged(ctx, cr, drRuleResourceVersion, drCtx) {
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
	// new discovery initialization
	dr = drInit(r.Client)
	// this fetches the discovery rule based on a the configured discovery rule reference
	// each kind of discovery rule is abstracted
	drKey := types.NamespacedName{Namespace: cr.GetNamespace(), Name: cr.Spec.DiscoveryRuleRef.Name}
	drRuleResourceVersion, err := dr.Get(ctx, drKey)
	if err != nil {
		log.Error(err, "cannot get discovery rule", "key", drKey, "nsn", drKey.String())
		cr.SetConditions(invv1alpha1.Failed(fmt.Sprintf("cannot get discovery rule, gvk %v with nsn %s not available", *drGVK, drKey.String())))
		// given this is a ummutable field this means the CR will have to be deleted/recreated
		return ctrl.Result{}, errors.Wrap(r.Status().Update(ctx, cr), errUpdateStatus)
	}

	if err := r.discoveryStore.Create(ctx, key, dr); err != nil {
		log.Error(err, "cannot add dr ")
		cr.SetConditions(invv1alpha1.Failed(fmt.Sprintf("cannot initialize discovery rule, gvk %v not registered", *drGVK)))
		// given this is a ummutable field this means the CR will have to be deleted/recreated
		return ctrl.Result{Requeue: true}, errors.Wrap(r.Status().Update(ctx, cr), errUpdateStatus)
	}
	
	// TODO in the case of a Static Discovery Rule this can be synchronous
	go dr.Run(ctx, drCtx)

	// update discovery rule start time
	cr.Status.StartTime = metav1.Now()

	//for _, profile := range drCtx
	cr.Status.UsedReferences = &invv1alpha1.DiscoveryRuleStatusUsedReferences{
		SecretResourceVersion:           drCtx.SecretResourceVersion,
		TLSSecretResourceVersion:        "", // TODO
		Profiles:                        drCtx.GetProfilesResourceVersion(),
		DiscoveryRuleRefResourceVersion: drRuleResourceVersion,
	}
	cr.SetConditions(invv1alpha1.Ready())
	return ctrl.Result{}, errors.Wrap(r.Status().Update(ctx, cr), errUpdateStatus)
}

func (r *reconciler) getReferences(ctx context.Context, cr *invv1alpha1.DiscoveryRule) (*schema.GroupVersionKind, *invv1alpha1.DiscoveryRuleContext, error) {
	var err error
	gvk, err := r.getDRGVK(ctx, cr)
	if err != nil {
		err = errors.Wrap(err, "cannot get gvk")
	}
	drCtx, err := r.getDiscoveryContext(ctx, cr)
	if err != nil {
		err = errors.Wrap(err, "cannot get discoveryContext")
	}
	return gvk, drCtx, err
}

func (r *reconciler) getDiscoveryContext(ctx context.Context, cr *invv1alpha1.DiscoveryRule) (*invv1alpha1.DiscoveryRuleContext, error) {
	profiles := make([]invv1alpha1.DiscoveryRuleContextProfile, 0, len(cr.Spec.Profiles))
	for _, profile := range cr.Spec.Profiles {
		connProfile, err := r.getConnProfile(ctx, types.NamespacedName{
			Namespace: cr.GetNamespace(),
			Name:      profile.ConnectionProfile,
		})
		if err != nil {
			return nil, err
		}
		syncProfile, err := r.getSyncProfile(ctx, types.NamespacedName{
			Namespace: cr.GetNamespace(),
			Name:      profile.SyncProfile,
		})
		if err != nil {
			return nil, err
		}
		profiles = append(profiles, invv1alpha1.DiscoveryRuleContextProfile{
			ConnectionProfile: connProfile,
			SyncProfile:       syncProfile,
		})
	}

	secret, err := r.getSecret(ctx, types.NamespacedName{
		Namespace: cr.GetNamespace(),
		Name:      cr.Spec.Secret,
	})
	if err != nil {
		return nil, err
	}

	return &invv1alpha1.DiscoveryRuleContext{
		Client:                r.Client,
		DiscoveryRule:         cr,
		Profiles:              profiles,
		SecretResourceVersion: secret.ResourceVersion,
	}, nil
}

func (r *reconciler) getSecret(ctx context.Context, key types.NamespacedName) (*corev1.Secret, error) {
	obj := &corev1.Secret{}
	if err := r.Get(ctx, key, obj); err != nil {
		return nil, err
	}
	return obj, nil
}

func (r *reconciler) getConnProfile(ctx context.Context, key types.NamespacedName) (*invv1alpha1.TargetConnectionProfile, error) {
	obj := &invv1alpha1.TargetConnectionProfile{}
	if err := r.Get(ctx, key, obj); err != nil {
		return nil, err
	}
	return obj, nil
}

func (r *reconciler) getSyncProfile(ctx context.Context, key types.NamespacedName) (*invv1alpha1.TargetSyncProfile, error) {
	obj := &invv1alpha1.TargetSyncProfile{}
	if err := r.Get(ctx, key, obj); err != nil {
		return nil, err
	}
	return obj, nil
}

func (r *reconciler) getDRGVK(ctx context.Context, cr *invv1alpha1.DiscoveryRule) (*schema.GroupVersionKind, error) {
	gv, err := schema.ParseGroupVersion(cr.Spec.DiscoveryRuleRef.APIVersion)
	if err != nil {
		return nil, err
	}
	if cr.Spec.DiscoveryRuleRef.Kind == "" {
		return nil, fmt.Errorf("kind cannot be emoty")
	}
	return &schema.GroupVersionKind{
		Group:   gv.Group,
		Version: gv.Version,
		Kind:    cr.Spec.DiscoveryRuleRef.Kind,
	}, nil
}

func (r *reconciler) HasReferencesChanged(ctx context.Context, cr *invv1alpha1.DiscoveryRule, drRuleResourceVersion string, drCtx *invv1alpha1.DiscoveryRuleContext) bool {
	log := log.FromContext(ctx)
	log.Info("HasReferencesChanged", "refs", cr.Status.UsedReferences)
	if cr.Status.UsedReferences == nil {
		return true
	}
	log.Info("HasReferencesChanged",
		"drRuleResourceVersion", fmt.Sprintf("%s/%s", cr.Status.UsedReferences.DiscoveryRuleRefResourceVersion, drRuleResourceVersion),
		"SecretResourceVersion", fmt.Sprintf("%s/%s", cr.Status.UsedReferences.SecretResourceVersion, drCtx.SecretResourceVersion),
	)
	if cr.Status.UsedReferences.DiscoveryRuleRefResourceVersion != drRuleResourceVersion ||
		cr.Status.UsedReferences.SecretResourceVersion != drCtx.SecretResourceVersion {
		//cr.Status.UsedReferences.TLSSecretResourceVersion != "" {}
		return true
	}

	log.Info("HasReferencesChanged", "profile length", fmt.Sprintf("%d/%d", len(cr.Status.UsedReferences.Profiles), len(drCtx.Profiles)))
	if len(cr.Status.UsedReferences.Profiles) != len(drCtx.Profiles) {
		return true
	}

	for idx := range cr.Status.UsedReferences.Profiles {
		log.Info("HasReferencesChanged",
			"ConnectionProfileResourceVersion",
			fmt.Sprintf("%s/%s",
				cr.Status.UsedReferences.Profiles[idx].ConnectionProfileResourceVersion,
				drCtx.Profiles[idx].ConnectionProfile.ResourceVersion,
			),
			"SyncProfileResourceVersion",
			fmt.Sprintf("%s/%s",
				cr.Status.UsedReferences.Profiles[idx].SyncProfileResourceVersion,
				drCtx.Profiles[idx].SyncProfile.ResourceVersion,
			),
		)
		if cr.Status.UsedReferences.Profiles[idx].ConnectionProfileResourceVersion != drCtx.Profiles[idx].ConnectionProfile.ResourceVersion ||
			cr.Status.UsedReferences.Profiles[idx].SyncProfileResourceVersion != drCtx.Profiles[idx].SyncProfile.ResourceVersion {
			return true
		}
	}

	return false
}
*/