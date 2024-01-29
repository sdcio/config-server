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
	"github.com/henderiw/logger/log"

	invv1alpha1 "github.com/iptecharch/config-server/apis/inv/v1alpha1"
	"github.com/iptecharch/config-server/pkg/reconcilers"
	"github.com/iptecharch/config-server/pkg/reconcilers/ctrlconfig"
	"github.com/iptecharch/config-server/pkg/reconcilers/resource"
	schemaloader "github.com/iptecharch/config-server/pkg/schema"
	sdcctx "github.com/iptecharch/config-server/pkg/sdc/ctx"
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

//+kubebuilder:rbac:groups=inv.sdcio.dev,resources=schemas,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups=inv.sdcio.dev,resources=schemas/status,verbs=get;update;patch

// SetupWithManager sets up the controller with the Manager.
func (r *reconciler) SetupWithManager(ctx context.Context, mgr ctrl.Manager, c interface{}) (map[schema.GroupVersionKind]chan event.GenericEvent, error) {
	var err error
	cfg, ok := c.(*ctrlconfig.ControllerConfig)
	if !ok {
		return nil, fmt.Errorf("cannot initialize, expecting controllerConfig, got: %s", reflect.TypeOf(c).Name())
	}

	/*
	if err := invv1alpha1.AddToScheme(mgr.GetScheme()); err != nil {
		return nil, err
	}
	*/

	cfg.SchemaServerStore.List(ctx, func(ctx context.Context, key store.Key, dsCtx sdcctx.SSContext) {
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
	log := log.FromContext(ctx).With("req", req)
	log.Info("reconcile")

	cr := &invv1alpha1.Schema{}
	if err := r.Get(ctx, req.NamespacedName, cr); err != nil {
		// if the resource no longer exists the reconcile loop is done
		if resource.IgnoreNotFound(err) != nil {
			log.Error(errGetCr, "error", err)
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
			}); err != nil {
				log.Error("cannot delete schema from schemaserver", "error", err)
				cr.SetConditions(invv1alpha1.Failed(err.Error()))
				return ctrl.Result{Requeue: true}, errors.Wrap(r.Status().Update(ctx, cr), errUpdateStatus)
			}
		}

		// delete the reference from disk
		if err := r.schemaLoader.DelRef(ctx, spec.GetKey()); err != nil {
			log.Error("cannot delete schema from disk", "error", err)
			cr.SetConditions(invv1alpha1.Failed(err.Error()))
			return ctrl.Result{Requeue: true}, errors.Wrap(r.Status().Update(ctx, cr), errUpdateStatus)
		}
		// remove the finalizer
		if err := r.finalizer.RemoveFinalizer(ctx, cr); err != nil {
			log.Error("cannot remove finalizer", "error", err)
			cr.SetConditions(invv1alpha1.DSFailed(err.Error()))
			return ctrl.Result{Requeue: true}, errors.Wrap(r.Status().Update(ctx, cr), errUpdateStatus)
		}

		log.Info("Successfully deleted resource")
		return ctrl.Result{}, nil
	}

	// We dont act as long the target is not ready (rady state is handled by the discovery controller)
	// Ready -> NotReady: happens only when the discovery fails => we keep the target as is do not delete the datatore/etc
	if err := r.finalizer.AddFinalizer(ctx, cr); err != nil {
		log.Error("cannot add finalizer", "error", err)
		cr.SetConditions(invv1alpha1.Failed(err.Error()))
		return ctrl.Result{Requeue: true}, errors.Wrap(r.Status().Update(ctx, cr), errUpdateStatus)
	}

	// we just insert the schema again
	log.Info("schema", "key", spec.GetKey())
	r.schemaLoader.AddRef(ctx, spec)
	_, dirExists, err := r.schemaLoader.GetRef(ctx, spec.GetKey())
	if err != nil {
		log.Error("cannot get schema", "error", err)
		cr.SetConditions(invv1alpha1.Failed(err.Error()))
		return ctrl.Result{Requeue: true}, errors.Wrap(r.Status().Update(ctx, cr), errUpdateStatus)
	}

	if !dirExists {
		// we set the loading condition to know loading started
		cr.SetConditions(invv1alpha1.Loading())
		if err := r.Status().Update(ctx, cr); err != nil {
			return ctrl.Result{Requeue: true}, err
		}

		if err := r.schemaLoader.Load(ctx, spec.GetKey()); err != nil {
			log.Error("cannot load schema", "error", err)
			cr.SetConditions(invv1alpha1.Failed(err.Error()))
			return ctrl.Result{Requeue: true}, errors.Wrap(r.Status().Update(ctx, cr), errUpdateStatus)
		}
	}
	// check if the schema exists
	if _, err := r.schemaclient.GetSchemaDetails(ctx, &sdcpb.GetSchemaDetailsRequest{
		Schema: spec.GetSchema(),
	}); err != nil {
		// TODO act upon NotFound error, etc

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
			log.Error("cannot load schema", "error", err)
			cr.SetConditions(invv1alpha1.Failed(err.Error()))
			return ctrl.Result{Requeue: true}, errors.Wrap(r.Status().Update(ctx, cr), errUpdateStatus)
		}
	}

	// schema ready
	cr.SetConditions(invv1alpha1.Ready())
	return ctrl.Result{}, errors.Wrap(r.Status().Update(ctx, cr), errUpdateStatus)

}
