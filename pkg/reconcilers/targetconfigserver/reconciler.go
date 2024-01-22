package targetconfigserver

import (
	"context"
	"fmt"
	"reflect"

	"github.com/pkg/errors"
	"k8s.io/apimachinery/pkg/apis/meta/internalversion"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/fields"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/event"
	"sigs.k8s.io/controller-runtime/pkg/log"

	configv1alpha1 "github.com/iptecharch/config-server/apis/config/v1alpha1"
	invv1alpha1 "github.com/iptecharch/config-server/apis/inv/v1alpha1"
	"github.com/iptecharch/config-server/pkg/configserver"
	"github.com/iptecharch/config-server/pkg/reconcilers"
	"github.com/iptecharch/config-server/pkg/reconcilers/ctrlconfig"
	"github.com/iptecharch/config-server/pkg/reconcilers/resource"
	"github.com/iptecharch/config-server/pkg/store"
	"github.com/iptecharch/config-server/pkg/store/memory"
	genericapirequest "k8s.io/apiserver/pkg/endpoints/request"
)

func init() {
	reconcilers.Register("targetconfigserver", &reconciler{})
}

const (
	finalizer = "targetconfigserver.inv.sdcio.dev/finalizer"
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

	if err := invv1alpha1.AddToScheme(mgr.GetScheme()); err != nil {
		return nil, err
	}

	r.Client = mgr.GetClient()
	r.finalizer = resource.NewAPIFinalizer(mgr.GetClient(), finalizer)
	r.configProvider = cfg.ConfigProvider
	r.targetStore = memory.NewStore[bool]() // keeps track of the target status locally

	return nil, ctrl.NewControllerManagedBy(mgr).
		Named("TargetConfigServerController").
		For(&invv1alpha1.Target{}).
		Complete(r)
}

type reconciler struct {
	client.Client
	finalizer *resource.APIFinalizer

	configProvider configserver.ResourceProvider
	targetStore    store.Storer[bool] // keeps track of the target status locally
}

func (r *reconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	log := log.FromContext(ctx).WithValues("req", req)
	log.Info("reconcile")

	crKey := store.KeyFromNSN(req.NamespacedName)

	cr := &invv1alpha1.Target{}
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
		// list the
		configList, err := r.listTargetConfigs(ctx, cr)
		if err != nil {
			return ctrl.Result{Requeue: true}, err
		}
		for _, config := range configList.Items {
			condition := config.GetCondition(configv1alpha1.ConditionTypeTargetReady)
			if condition.Status != metav1.ConditionFalse && condition.Reason != string(configv1alpha1.ConditionReasonTargetDeleted) {
				// update the status if not already set
				// resource version does not need to be updated
				config.SetConditions(configv1alpha1.TargetDeleted())
				r.configProvider.UpdateStore(ctx, store.KeyFromNSN(types.NamespacedName{
					Name:      config.GetName(),
					Namespace: config.GetNamespace(),
				}), &config)
			}
		}
		if err := r.finalizer.RemoveFinalizer(ctx, cr); err != nil {
			log.Error(err, "cannot remove finalizer")
			return ctrl.Result{Requeue: true}, err
		}
		r.targetStore.Delete(ctx, crKey) // err is always nil
		log.Info("Successfully deleted resource")
		return ctrl.Result{}, nil
	}

	if err := r.finalizer.AddFinalizer(ctx, cr); err != nil {
		log.Error(err, "cannot add finalizer")
		return ctrl.Result{Requeue: true}, err
	}

	// check if the target transitioned wrt ready state
	transition := false
	wasReady, err := r.targetStore.Get(ctx, crKey)
	if err != nil {
		transition = true
	}
	if cr.IsReady() != wasReady {
		transition = true
	}
	r.targetStore.Update(ctx, crKey, cr.IsReady())

	if transition {
		// is this too much, how to find if the target status changed?
		configList, err := r.listTargetConfigs(ctx, cr)
		if err != nil {
			return ctrl.Result{Requeue: true}, err
		}
		for _, config := range configList.Items {
			// target is ready
			if cr.IsReady() {
				// todo sort based on time
				config.SetConditions(configv1alpha1.TargetReady())

				r.configProvider.UpdateTarget(ctx, store.KeyFromNSN(types.NamespacedName{
					Name:      config.GetName(),
					Namespace: config.GetNamespace(),
				}), crKey, &config)

			} else {
				condition := config.GetCondition(configv1alpha1.ConditionTypeTargetReady)
				if condition.Status != metav1.ConditionFalse && condition.Reason != string(configv1alpha1.ConditionReasonNotReady) {
					// update the status if not already set
					// resource version does not need to be updated, so we do a shortcut
					config.SetConditions(configv1alpha1.TargetNotReady(cr.NotReadyReason()))
					r.configProvider.UpdateStore(ctx, store.KeyFromNSN(types.NamespacedName{
						Name:      config.GetName(),
						Namespace: config.GetNamespace(),
					}), &config)
				}
			}
		}
	}

	// if the Ready state is false and we dont have a true status in DSReady state
	// we dont update the target -> otherwise we get to many reconcile events and updates fail
	return ctrl.Result{}, nil
}

func (r *reconciler) listTargetConfigs(ctx context.Context, cr *invv1alpha1.Target) (*configv1alpha1.ConfigList, error) {
	ctx = genericapirequest.WithNamespace(ctx, cr.GetNamespace())

	obj, err := r.configProvider.List(ctx, &internalversion.ListOptions{
		FieldSelector: fields.OneTermEqualSelector("metadata.namespace", cr.GetNamespace()),
		LabelSelector: labels.SelectorFromSet(map[string]string{
			configv1alpha1.TargetNameKey: cr.GetName(),
		}),
	})
	if err != nil {
		return nil, err
	}
	configList, ok := obj.(*configv1alpha1.ConfigList)
	if !ok {
		return nil, fmt.Errorf("listTargetConfigs, unexpected object, wanted %s, got : %s",
			reflect.TypeOf(configv1alpha1.ConfigList{}).Name(),
			reflect.TypeOf(obj).Name(),
		)
	}
	return configList, nil

}
