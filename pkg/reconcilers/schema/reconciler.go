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
	"errors"
	"fmt"
	"path/filepath"
	"reflect"

	"github.com/henderiw/apiserver-store/pkg/storebackend"
	"github.com/henderiw/logger/log"
	pkgerrors "github.com/pkg/errors"
	condv1alpha1 "github.com/sdcio/config-server/apis/condition/v1alpha1"
	invv1alpha1 "github.com/sdcio/config-server/apis/inv/v1alpha1"
	sdcerror "github.com/sdcio/config-server/pkg/error"
	"github.com/sdcio/config-server/pkg/git/auth/secret"
	"github.com/sdcio/config-server/pkg/reconcilers"
	"github.com/sdcio/config-server/pkg/reconcilers/ctrlconfig"
	"github.com/sdcio/config-server/pkg/reconcilers/eventhandler"
	"github.com/sdcio/config-server/pkg/reconcilers/resource"
	schemaloader "github.com/sdcio/config-server/pkg/schema"
	sdcctx "github.com/sdcio/config-server/pkg/sdc/ctx"
	ssclient "github.com/sdcio/config-server/pkg/sdc/schemaserver/client"
	sdcpb "github.com/sdcio/sdc-protos/sdcpb"
	corev1 "k8s.io/api/core/v1"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/tools/record"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/builder"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/event"
	"sigs.k8s.io/controller-runtime/pkg/predicate"
)

func init() {
	reconcilers.Register(crName, &reconciler{})
}

const (
	crName         = "schema"
	controllerName = "SchemaController"
	finalizer      = "schema.inv.sdcio.dev/finalizer"
	// errors
	errGetCr           = "cannot get cr"
	errUpdateDataStore = "cannot update datastore"
	errUpdateStatus    = "cannot update status"
)

// SetupWithManager sets up the controller with the Manager.
func (r *reconciler) SetupWithManager(ctx context.Context, mgr ctrl.Manager, c interface{}) (map[schema.GroupVersionKind]chan event.GenericEvent, error) {
	var err error
	cfg, ok := c.(*ctrlconfig.ControllerConfig)
	if !ok {
		return nil, fmt.Errorf("cannot initialize, expecting controllerConfig, got: %s", reflect.TypeOf(c).Name())
	}

	err = cfg.SchemaServerStore.List(ctx, func(ctx context.Context, key storebackend.Key, dsCtx sdcctx.SSContext) {
		r.schemaclient = dsCtx.SSClient
	})
	if err != nil {
		return nil, err
	}
	if r.schemaclient == nil {
		return nil, fmt.Errorf("cannot get schema client")
	}

	r.Client = mgr.GetClient()
	r.finalizer = resource.NewAPIFinalizer(mgr.GetClient(), finalizer)
	// initializes the directory
	r.schemaBasePath = cfg.SchemaDir
	r.schemaLoader, err = schemaloader.NewLoader(
		filepath.Join(r.schemaBasePath, "tmp"),
		r.schemaBasePath,
		secret.NewCredentialResolver(mgr.GetClient(), []secret.Resolver{
			secret.NewBasicAuthResolver(),
		}),
	)
	if err != nil {
		return nil, pkgerrors.Wrap(err, "cannot initialize schemaloader")
	}
	r.recorder = mgr.GetEventRecorderFor(controllerName)

	return nil, ctrl.NewControllerManagedBy(mgr).
		Named(controllerName).
		For(&invv1alpha1.Schema{}, builder.WithPredicates(predicate.GenerationChangedPredicate{})).
		Watches(&corev1.Secret{}, &eventhandler.SecretForSchemaEventHandler{Client: mgr.GetClient(), ControllerName: controllerName}).
		Complete(r)
}

type reconciler struct {
	client.Client
	finalizer *resource.APIFinalizer

	schemaLoader   *schemaloader.Loader
	schemaclient   ssclient.Client
	schemaBasePath string
	recorder       record.EventRecorder
}

func (r *reconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	ctx = ctrlconfig.InitContext(ctx, controllerName, req.NamespacedName)
	log := log.FromContext(ctx)
	log.Info("reconcile")

	cr := &invv1alpha1.Schema{}
	if err := r.Get(ctx, req.NamespacedName, cr); err != nil {
		// if the resource no longer exists the reconcile loop is done
		if !k8serrors.IsNotFound(err) {
			log.Error(errGetCr, "error", err)
			return ctrl.Result{}, pkgerrors.Wrap(resource.IgnoreNotFound(err), errGetCr)
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
				return r.handleErrorWithStatus(ctx, cr, "cannot delete schema from schemaserver", err)
			}
		}

		// delete the reference from disk
		if err := r.schemaLoader.DelRef(ctx, spec.GetKey()); err != nil {
			return r.handleErrorWithStatus(ctx, cr, "cannot delete reference", err)
		}
		// remove the finalizer
		if err := r.finalizer.RemoveFinalizer(ctx, cr); err != nil {
			return r.handleErrorWithStatus(ctx, cr, "cannot remove finalizer", err)
		}
		// done deleting
		return ctrl.Result{}, nil
	}

	// We dont act as long the target is not ready (ready state is handled by the discovery controller)
	// Ready -> NotReady: happens only when the discovery fails => we keep the target as is do not delete the datastore/etc
	if err := r.finalizer.AddFinalizer(ctx, cr); err != nil {
		// we always retry when status fails -> optimistic concurrency
		return r.handleErrorWithStatus(ctx, cr, "cannot add finalizer", err)
	}

	// we just insert the schema again
	r.schemaLoader.AddRef(ctx, spec)
	_, dirExists, err := r.schemaLoader.GetRef(ctx, spec.GetKey())
	if err != nil {
		return r.handleErrorWithStatus(ctx, cr, "cannot get schema reference", err)
	}

	if !dirExists {
		// we set the loading condition to know loading started
		cr.SetConditions(invv1alpha1.Loading())
		if err := r.Status().Update(ctx, cr); err != nil {
			// we always retry when status fails -> optimistic concurrency
			return r.handleError(ctx, cr, "cannot update status", err)
		}
		r.recorder.Eventf(cr, corev1.EventTypeNormal,
			"schema", "loading")
		if err := r.schemaLoader.Load(ctx, spec.GetKey(), types.NamespacedName{
			Namespace: cr.Namespace,
			Name:      cr.Spec.Credentials,
		}); err != nil {
			return r.handleErrorWithStatus(ctx, cr, "cannot load schema", err)
		}
	}
	// check if the schema exists
	if _, err := r.schemaclient.GetSchemaDetails(ctx, &sdcpb.GetSchemaDetailsRequest{
		Schema: spec.GetSchema(),
	}); err != nil {
		// schema does not exists in schema-server -> create it
		if _, err := r.schemaclient.CreateSchema(ctx, &sdcpb.CreateSchemaRequest{
			Schema:    spec.GetSchema(),
			File:      spec.GetNewSchemaBase(r.schemaBasePath).Models,
			Directory: spec.GetNewSchemaBase(r.schemaBasePath).Includes,
			Exclude:   spec.GetNewSchemaBase(r.schemaBasePath).Excludes,
		}); err != nil {
			return r.handleErrorWithStatus(ctx, cr, "cannot create schema", err)
		}
	} else {
		// if schema exists, reload it from disk
		// TODO: Does this actually reload schema from disk?
		_, err := r.schemaclient.ReloadSchema(ctx, &sdcpb.ReloadSchemaRequest{Schema: spec.GetSchema()})
		if err != nil {
			return r.handleErrorWithStatus(ctx, cr, "cannot reload schema", err)
		}
	}

	// schema ready
	cr.SetConditions(condv1alpha1.Ready())
	r.recorder.Eventf(cr, corev1.EventTypeNormal,
		"schema", "ready")
	err = r.Status().Update(ctx, cr)
	if err != nil {
		return ctrl.Result{}, pkgerrors.Wrap(err, errUpdateStatus)
	}
	return ctrl.Result{}, nil
}

func (r *reconciler) handleErrorWithStatus(ctx context.Context, cr *invv1alpha1.Schema, msg string, err error) (ctrl.Result, error) {
	res, err := r.handleError(ctx, cr, msg, err)
	sErr := r.Status().Update(ctx, cr)
	if sErr != nil {
		return ctrl.Result{}, pkgerrors.Wrap(sErr, errUpdateStatus)
	}
	return res, err
}

func (r *reconciler) handleError(ctx context.Context, cr *invv1alpha1.Schema, msg string, err error) (ctrl.Result, error) {
	log := log.FromContext(ctx)
	if err == nil {
		cr.SetConditions(condv1alpha1.Failed(msg))
		log.Error(msg)
		r.recorder.Eventf(cr, corev1.EventTypeWarning, crName, msg)
		return ctrl.Result{}, err // recoverable error
	} else {
		cr.SetConditions(condv1alpha1.Failed(err.Error()))
		log.Error(msg, "error", err)
		r.recorder.Eventf(cr, corev1.EventTypeWarning, crName, fmt.Sprintf("%s, err: %s", msg, err.Error()))
		var recoverableError *sdcerror.RecoverableError
		var unrecoverableError *sdcerror.UnrecoverableError
		switch {
		case errors.As(err, &recoverableError):
			return ctrl.Result{}, recoverableError // recoverable error
		case errors.As(err, &unrecoverableError):
			return ctrl.Result{Requeue: false}, nil // unrecoverable error - setting an error here would result in ignoring a request to not requeue
		default:
			return ctrl.Result{}, err // recoverable error
		}
	}
}
