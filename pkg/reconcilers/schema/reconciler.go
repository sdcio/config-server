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
	"net/url"
	"path"
	"reflect"

	"github.com/henderiw/apiserver-store/pkg/storebackend"
	"github.com/henderiw/logger/log"
	pkgerrors "github.com/pkg/errors"
	condv1alpha1 "github.com/sdcio/config-server/apis/condition/v1alpha1"
	invv1alpha1 "github.com/sdcio/config-server/apis/inv/v1alpha1"
	sdcerrors "github.com/sdcio/config-server/pkg/errors"
	"github.com/sdcio/config-server/pkg/git"
	"github.com/sdcio/config-server/pkg/git/auth/secret"
	"github.com/sdcio/config-server/pkg/reconcilers"
	"github.com/sdcio/config-server/pkg/reconcilers/ctrlconfig"
	"github.com/sdcio/config-server/pkg/reconcilers/eventhandler"
	"github.com/sdcio/config-server/pkg/reconcilers/resource"
	yangSchema "github.com/sdcio/config-server/pkg/schema"
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
	reconcilerName = "SchemaController"
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
	r.finalizer = resource.NewAPIFinalizer(mgr.GetClient(), finalizer, reconcilerName)
	// initializes the directory
	r.schemaBasePath = cfg.SchemaDir
	r.schemaLoader, err = yangSchema.NewSchemaLoader(path.Join(r.schemaBasePath, "tmp"), r.schemaBasePath, r.schemaclient, yangSchema.NewSchemaUploaderLocal(r.schemaclient))
	if err != nil {
		return nil, pkgerrors.Wrap(err, "cannot initialize schemaloader")
	}
	r.recorder = mgr.GetEventRecorderFor(reconcilerName)

	return nil, ctrl.NewControllerManagedBy(mgr).
		Named(reconcilerName).
		For(&invv1alpha1.Schema{}, builder.WithPredicates(predicate.GenerationChangedPredicate{})).
		Watches(&corev1.Secret{}, &eventhandler.SecretForSchemaEventHandler{Client: mgr.GetClient(), ControllerName: reconcilerName}).
		Complete(r)
}

type reconciler struct {
	client.Client
	finalizer *resource.APIFinalizer

	schemaLoader   *yangSchema.SchemaLoader
	schemaclient   ssclient.Client
	schemaBasePath string
	recorder       record.EventRecorder
}

func (r *reconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	ctx = ctrlconfig.InitContext(ctx, reconcilerName, req.NamespacedName)
	log := log.FromContext(ctx)
	log.Info("reconcile")

	schema := &invv1alpha1.Schema{}
	if err := r.Get(ctx, req.NamespacedName, schema); err != nil {
		// if the resource no longer exists the reconcile loop is done
		if !k8serrors.IsNotFound(err) {
			log.Error(errGetCr, "error", err)
			return ctrl.Result{}, pkgerrors.Wrap(resource.IgnoreNotFound(err), errGetCr)
		}
		return ctrl.Result{}, nil
	}
	schemaOrig := schema.DeepCopy()
	spec := &schema.Spec

	if !schema.GetDeletionTimestamp().IsZero() {
		// check if the schema exists; this is == nil check; in case of err it does not exist
		if _, err := r.schemaclient.GetSchemaDetails(ctx, &sdcpb.GetSchemaDetailsRequest{
			Schema: spec.GetSchema(),
		}); err == nil {
			if _, err := r.schemaclient.DeleteSchema(ctx, &sdcpb.DeleteSchemaRequest{
				Schema: spec.GetSchema(),
			}); err != nil {
				return r.handleError(ctx, schema, "cannot delete schema from schemaserver", err)
			}
		}

		// delete the reference from disk
		if err := r.schemaLoader.RemoveSchema(ctx, yangSchema.NewSchemaID(spec.Provider, spec.Version)); err != nil {
			return r.handleError(ctx, schemaOrig, "cannot delete reference", err)
		}
		// remove the finalizer
		if err := r.finalizer.RemoveFinalizer(ctx, schema); err != nil {
			return r.handleError(ctx, schemaOrig, "cannot remove finalizer", err)
		}
		// done deleting
		return ctrl.Result{}, nil
	}

	// We dont act as long the target is not ready (ready state is handled by the discovery controller)
	// Ready -> NotReady: happens only when the discovery fails => we keep the target as is do not delete the datastore/etc
	if err := r.finalizer.AddFinalizer(ctx, schema); err != nil {
		// we always retry when status fails -> optimistic concurrency
		return r.handleError(ctx, schemaOrig, "cannot add finalizer", err)
	}

	if schema.IsSchemaServerReady() {
		return r.handleError(ctx, schemaOrig, "schema server not ready", nil)
	}

	if !r.schemaLoader.SchemaExists(yangSchema.NewSchemaID(spec.Provider, spec.Version)) {
		// we set the loading condition to know loading started
		schema.SetConditions(invv1alpha1.Loading())
		if err := r.Status().Update(ctx, schema); err != nil {
			// we always retry when status fails -> optimistic concurrency
			return r.handleError(ctx, schemaOrig, "cannot update status", err)
		}
		r.recorder.Eventf(schema, corev1.EventTypeNormal, "schema", "loading")
		// we just insert the schema again
		schemaDef, err := schemaSpecToSchemaDefinition(ctx, schema, r.Client)
		if err != nil {
			return r.handleError(ctx, schemaOrig, "error converting from api type to internal", nil)
		}

		if err = r.schemaLoader.AddSchema(ctx, schemaDef); err != nil {
			return r.handleError(ctx, schemaOrig, "cannot load schema", err)
		}
	}
	// check if the schema exists
	rsp, err := r.schemaclient.GetSchemaDetails(ctx, &sdcpb.GetSchemaDetailsRequest{
		Schema: spec.GetSchema(),
	})
	if err != nil {
		return r.handleError(ctx, schemaOrig, "get schema detail returned with error", err)
	}
	if rsp == nil || rsp.Schema == nil {
		return r.handleError(ctx, schemaOrig, "get schema detail response w/o a response or schems", nil)
	}

	// switch rsp.Schema.Status {
	// case sdcpb.SchemaStatus_FAILED:
	// 	if _, err := r.schemaclient.CreateSchema(ctx, &sdcpb.CreateSchemaRequest{
	// 		Schema:    spec.GetSchema(),
	// 		File:      spec.GetNewSchemaBase(r.schemaBasePath).Models,
	// 		Directory: spec.GetNewSchemaBase(r.schemaBasePath).Includes,
	// 		Exclude:   spec.GetNewSchemaBase(r.schemaBasePath).Excludes,
	// 	}); err != nil {
	// 		return r.handleError(ctx, schemaOrig, "cannot create schema", err)
	// 	}
	// 	return r.handleSuccess(ctx, schemaOrig)
	// case sdcpb.SchemaStatus_RELOADING, sdcpb.SchemaStatus_INITIALIZING:
	// 	return r.handleError(ctx, schemaOrig, fmt.Sprintf("schema %s", rsp.Schema.Status), nil)
	// default: // OK case
	// 	return r.handleSuccess(ctx, schemaOrig)
	// }
	return r.handleSuccess(ctx, schemaOrig)
}

func schemaSpecToSchemaDefinition(ctx context.Context, schema *invv1alpha1.Schema, client client.Reader) (*yangSchema.SchemaDefinition, error) {
	sid := yangSchema.NewSchemaID(schema.Spec.Provider, schema.Spec.Version)
	sd := yangSchema.NewSchemaDefinition(sid)

	for _, r := range schema.Spec.Repositories {
		repoUrl, err := url.Parse(r.RepositoryURL)
		if err != nil {
			return nil, err
		}

		dirs := make([]*yangSchema.SrcDstPath, 0, len(r.Dirs))

		for _, d := range r.Dirs {
			dirs = append(dirs, yangSchema.NewSrcDstPath(d.Src, d.Dst))
		}

		refKind, err := convertRefKind(r.Kind)
		if err != nil {
			return nil, err
		}

		rs := yangSchema.NewRepositorySpec(repoUrl, refKind, r.Ref, yangSchema.NewSchemaLoadSpec(r.Schema.Models, r.Schema.Includes, r.Schema.Excludes), dirs)

		// set proxy for the repo
		if r.Proxy != nil && r.Proxy.URL != "" {
			purl, err := url.Parse(r.Proxy.URL)
			if err != nil {
				return nil, err
			}
			rs.SetProxy(purl)
		}

		if r.Credentials != "" {
			cred, err := secret.NewCredentialResolver(client, []secret.Resolver{
				secret.NewBasicAuthResolver(),
			}).ResolveCredential(ctx, types.NamespacedName{Namespace: schema.Namespace, Name: r.Credentials})
			if err != nil {
				return nil, err
			}
			rs.SetCredentials(cred.ToAuthMethod())
		}

		sd.AddRepositorySpec(rs)
	}

	return sd, nil
}

func convertRefKind(btc invv1alpha1.BranchTagKind) (git.GitRefKind, error) {
	switch btc {
	case invv1alpha1.BranchTagKindBranch:
		return git.GitRefKindBranch, nil
	case invv1alpha1.BranchTagKindTag:
		return git.GitRefKindTag, nil
	case invv1alpha1.BranchTagKindHash:
		return git.GitRefKindHash, nil
	}
	return "", fmt.Errorf("unknown BranchTagKind")
}

func (r *reconciler) handleSuccess(ctx context.Context, schema *invv1alpha1.Schema) (ctrl.Result, error) {
	log := log.FromContext(ctx)
	log.Debug("handleSuccess", "key", schema.GetNamespacedName(), "status old", schema.DeepCopy().Status)
	// take a snapshot of the current object
	patch := client.MergeFrom(schema.DeepCopy())
	// update status
	schema.SetConditions(condv1alpha1.Ready())
	r.recorder.Eventf(schema, corev1.EventTypeNormal, invv1alpha1.SchemaKind, "ready")

	log.Debug("handleSuccess", "key", schema.GetNamespacedName(), "status new", schema.Status)

	return ctrl.Result{}, pkgerrors.Wrap(r.Client.Status().Patch(ctx, schema, patch, &client.SubResourcePatchOptions{
		PatchOptions: client.PatchOptions{
			FieldManager: reconcilerName,
		},
	}), errUpdateStatus)
}

func (r *reconciler) handleError(ctx context.Context, schema *invv1alpha1.Schema, msg string, err error) (ctrl.Result, error) {
	log := log.FromContext(ctx)

	// take a snapshot of the current object
	patch := client.MergeFrom(schema.DeepCopy())

	if err != nil {
		msg = fmt.Sprintf("%s err %s", msg, err.Error())
	}

	schema.SetConditions(condv1alpha1.Failed(msg))
	log.Error(msg)
	r.recorder.Eventf(schema, corev1.EventTypeWarning, crName, msg)

	var unrecoverableError *sdcerrors.UnrecoverableError
	result := ctrl.Result{}
	if errors.As(err, &unrecoverableError) {
		result = ctrl.Result{Requeue: false} // unrecoverable error - setting an error here would result in ignoring a request to not requeue
	}

	return result, pkgerrors.Wrap(r.Client.Status().Patch(ctx, schema, patch, &client.SubResourcePatchOptions{
		PatchOptions: client.PatchOptions{
			FieldManager: reconcilerName,
		},
	}), errUpdateStatus)
}
