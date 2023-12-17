package schema

import (
	"context"
	"fmt"
	"path/filepath"
	"reflect"

	"github.com/pkg/errors"
	"k8s.io/apimachinery/pkg/runtime/schema"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/event"
	"sigs.k8s.io/controller-runtime/pkg/log"

	invv1alpha1 "github.com/iptecharch/config-server/apis/inv/v1alpha1"
	"github.com/iptecharch/config-server/pkg/reconcilers"
	"github.com/iptecharch/config-server/pkg/reconcilers/context/dsctx"
	"github.com/iptecharch/config-server/pkg/reconcilers/ctrlconfig"
	"github.com/iptecharch/config-server/pkg/reconcilers/resource"
	schemaloader "github.com/iptecharch/config-server/pkg/schema"
	ssclient "github.com/iptecharch/config-server/pkg/sdc/schemaserver/client"
	"github.com/iptecharch/config-server/pkg/store"
	sdcpb "github.com/iptecharch/sdc-protos/sdcpb"
)

func init() {
	reconcilers.Register("schema", &reconciler{})
}

const (
	finalizer = "schema.inv.sdcio.dev/finalizer"
	// errors
	errGetCr           = "cannot get cr"
	errUpdateDataStore = "cannot update datastore"
	errUpdateStatus    = "cannot update status"
)

type adder interface {
	Add(item interface{})
}

//+kubebuilder:rbac:groups=inv.sdcio.dev,resources=schemas,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups=inv.sdcio.dev,resources=schemas/status,verbs=get;update;patch

// SetupWithManager sets up the controller with the Manager.
func (r *reconciler) SetupWithManager(ctx context.Context, mgr ctrl.Manager, c interface{}) (map[schema.GroupVersionKind]chan event.GenericEvent, error) {
	var err error
	cfg, ok := c.(*ctrlconfig.ControllerConfig)
	if !ok {
		return nil, fmt.Errorf("cannot initialize, expecting controllerConfig, got: %s", reflect.TypeOf(c).Name())
	}

	if err := invv1alpha1.AddToScheme(mgr.GetScheme()); err != nil {
		return nil, err
	}

	cfg.DataServerStore.List(ctx, func(ctx context.Context, key store.Key, dsCtx dsctx.Context) {
		r.schemaclient = dsCtx.SSClient
	})
	if r.schemaclient == nil {
		return nil, fmt.Errorf("cannot get schema client")
	}

	r.Client = mgr.GetClient()
	r.finalizer = resource.NewAPIFinalizer(mgr.GetClient(), finalizer)
	// initiazes the directory
	r.schemaBasePath = cfg.SchemaDir
	r.schemaLoader, err = schemaloader.NewLoader(filepath.Join(r.schemaBasePath, "tmp"), r.schemaBasePath)
	if err != nil {
		return nil, errors.Wrap(err, "cannot initialize schemaloader")
	}

	return nil, ctrl.NewControllerManagedBy(mgr).
		Named("SchemaController").
		For(&invv1alpha1.Schema{}).
		Complete(r)
}

type reconciler struct {
	client.Client
	finalizer *resource.APIFinalizer

	schemaLoader   *schemaloader.Loader
	schemaclient   ssclient.Client
	schemaBasePath string
}

func (r *reconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	log := log.FromContext(ctx).WithValues("req", req)
	log.Info("reconcile")

	cr := &invv1alpha1.Schema{}
	if err := r.Get(ctx, req.NamespacedName, cr); err != nil {
		// if the resource no longer exists the reconcile loop is done
		if resource.IgnoreNotFound(err) != nil {
			log.Error(err, errGetCr)
			return ctrl.Result{}, errors.Wrap(resource.IgnoreNotFound(err), errGetCr)
		}
		return ctrl.Result{}, nil
	}
	cr = cr.DeepCopy()
	spec := &cr.Spec

	if !cr.GetDeletionTimestamp().IsZero() {
		// check if the schema exists; this is == nil check; in case of err it does not exist
		if _, err := r.schemaclient.GetSchemaDetails(ctx, &sdcpb.GetSchemaDetailsRequest{
			Schema: spec.GetSchema(),
		}); err == nil {
			if _, err := r.schemaclient.DeleteSchema(ctx, &sdcpb.DeleteSchemaRequest{
				Schema: spec.GetSchema(),
			}); err == nil {
				log.Error(err, "cannot delete schema from schemaserver")
				cr.SetConditions(invv1alpha1.Failed(err.Error()))
				return ctrl.Result{Requeue: true}, errors.Wrap(r.Status().Update(ctx, cr), errUpdateStatus)
			}
		}

		// delete the reference from disk
		if err := r.schemaLoader.DelRef(ctx, spec.GetKey()); err != nil {
			log.Error(err, "cannot delete schema from disk")
			cr.SetConditions(invv1alpha1.Failed(err.Error()))
			return ctrl.Result{Requeue: true}, errors.Wrap(r.Status().Update(ctx, cr), errUpdateStatus)
		}
		// remove the finalizer
		if err := r.finalizer.RemoveFinalizer(ctx, cr); err != nil {
			log.Error(err, "cannot remove finalizer")
			cr.SetConditions(invv1alpha1.DSFailed(err.Error()))
			return ctrl.Result{Requeue: true}, errors.Wrap(r.Status().Update(ctx, cr), errUpdateStatus)
		}

		log.Info("Successfully deleted resource")
		return ctrl.Result{}, nil
	}

	// We dont act as long the target is not ready (rady state is handled by the discovery controller)
	// Ready -> NotReady: happens only when the discovery fails => we keep the target as is do not delete the datatore/etc
	if err := r.finalizer.AddFinalizer(ctx, cr); err != nil {
		log.Error(err, "cannot add finalizer")
		cr.SetConditions(invv1alpha1.Failed(err.Error()))
		return ctrl.Result{Requeue: true}, errors.Wrap(r.Status().Update(ctx, cr), errUpdateStatus)
	}

	// we just insert the schema again
	r.schemaLoader.AddRef(ctx, spec)
	_, dirExists, err := r.schemaLoader.GetRef(ctx, spec.GetKey())
	if err != nil {
		log.Error(err, "cannot get schema")
		cr.SetConditions(invv1alpha1.Failed(err.Error()))
		return ctrl.Result{Requeue: true}, errors.Wrap(r.Status().Update(ctx, cr), errUpdateStatus)
	}

	if !dirExists {
		if err := r.schemaLoader.Load(ctx, spec.GetKey()); err != nil {
			log.Error(err, "cannot load schema")
			cr.SetConditions(invv1alpha1.Failed(err.Error()))
			return ctrl.Result{Requeue: true}, errors.Wrap(r.Status().Update(ctx, cr), errUpdateStatus)
		}
	}
	// check if the schema exists
	if _, err := r.schemaclient.GetSchemaDetails(ctx, &sdcpb.GetSchemaDetailsRequest{
		Schema: spec.GetSchema(),
	}); err != nil {
		log.Info("schema", "schema", spec.GetSchema())
		log.Info("create schema", "Models", spec.GetNewSchemaBase(r.schemaBasePath).Models)
		log.Info("create schema", "Includes", spec.GetNewSchemaBase(r.schemaBasePath).Includes)
		log.Info("create schema", "Excludes", spec.GetNewSchemaBase(r.schemaBasePath).Excludes)
		// schema does not exists
		if _, err := r.schemaclient.CreateSchema(ctx, &sdcpb.CreateSchemaRequest{
			Schema:    spec.GetSchema(),
			File:      spec.GetNewSchemaBase(r.schemaBasePath).Models,
			Directory: spec.GetNewSchemaBase(r.schemaBasePath).Includes,
			Exclude:   spec.GetNewSchemaBase(r.schemaBasePath).Excludes,
		}); err != nil {
			log.Error(err, "cannot load schema")
			cr.SetConditions(invv1alpha1.Failed(err.Error()))
			return ctrl.Result{Requeue: true}, errors.Wrap(r.Status().Update(ctx, cr), errUpdateStatus)
		}
	}

	// schema ready
	cr.SetConditions(invv1alpha1.Ready())
	return ctrl.Result{}, errors.Wrap(r.Status().Update(ctx, cr), errUpdateStatus)

}