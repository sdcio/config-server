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

package targetdatastore

import (
	"context"
	"fmt"
	"reflect"
	"strings"
	"time"

	"github.com/henderiw/apiserver-store/pkg/storebackend"
	"github.com/henderiw/logger/log"
	"github.com/pkg/errors"
	condv1alpha1 "github.com/sdcio/config-server/apis/condition/v1alpha1"
	invv1alpha1 "github.com/sdcio/config-server/apis/inv/v1alpha1"
	"github.com/sdcio/config-server/pkg/reconcilers"
	"github.com/sdcio/config-server/pkg/reconcilers/ctrlconfig"
	"github.com/sdcio/config-server/pkg/reconcilers/eventhandler"
	"github.com/sdcio/config-server/pkg/reconcilers/resource"
	sdcctx "github.com/sdcio/config-server/pkg/sdc/ctx"
	dsclient "github.com/sdcio/config-server/pkg/sdc/dataserver/client"
	sdctarget "github.com/sdcio/config-server/pkg/target"
	sdcpb "github.com/sdcio/sdc-protos/sdcpb"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/tools/record"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/event"
)

func init() {
	reconcilers.Register(crName, &reconciler{})
}

const (
	crName         = "targetdatastore"
	reconcilerName = "TargetDataStoreController"
	finalizer      = "targetdatastore.inv.sdcio.dev/finalizer"
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

	r.Client = mgr.GetClient()
	r.finalizer = resource.NewAPIFinalizer(mgr.GetClient(), finalizer, reconcilerName)
	r.targetStore = cfg.TargetStore
	r.dataServerStore = cfg.DataServerStore
	r.recorder = mgr.GetEventRecorderFor(reconcilerName)

	//targetDSWatcher := newTargetDataStoreWatcher(mgr.GetClient(), cfg.TargetStore)
	//go targetDSWatcher.Start(ctx)

	return nil, ctrl.NewControllerManagedBy(mgr).
		Named(reconcilerName).
		For(&invv1alpha1.Target{}).
		Watches(&invv1alpha1.TargetConnectionProfile{}, &eventhandler.TargetConnProfileForTargetEventHandler{Client: mgr.GetClient()}).
		Watches(&invv1alpha1.TargetSyncProfile{}, &eventhandler.TargetSyncProfileForTargetEventHandler{Client: mgr.GetClient()}).
		Watches(&corev1.Secret{}, &eventhandler.SecretForTargetEventHandler{Client: mgr.GetClient()}).
		Complete(r)
}

type reconciler struct {
	client.Client
	finalizer       *resource.APIFinalizer
	targetStore     storebackend.Storer[*sdctarget.Context]
	dataServerStore storebackend.Storer[sdcctx.DSContext]
	recorder        record.EventRecorder
}

func (r *reconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	ctx = ctrlconfig.InitContext(ctx, reconcilerName, req.NamespacedName)
	log := log.FromContext(ctx)
	log.Info("reconcile")

	targetKey := storebackend.KeyFromNSN(req.NamespacedName)

	target := &invv1alpha1.Target{}
	if err := r.Get(ctx, req.NamespacedName, target); err != nil {
		// if the resource no longer exists the reconcile loop is done
		if resource.IgnoreNotFound(err) != nil {
			log.Error(errGetCr, "error", err)
			return ctrl.Result{}, errors.Wrap(resource.IgnoreNotFound(err), errGetCr)
		}
		return ctrl.Result{}, nil
	}

	targetOrig := target.DeepCopy()

	if !target.GetDeletionTimestamp().IsZero() {
		if len(target.GetFinalizers()) > 1 {
			// this should be the last finalizer to be removed as this deletes the target from the taregtStore
			log.Debug("requeue delete, not all finalizers removed", "finalizers", target.GetFinalizers())
			return ctrl.Result{Requeue: true},
				errors.Wrap(r.handleError(ctx, targetOrig, "deleting target, not all finalizers removed", nil, true), errUpdateStatus)
		}
		log.Debug("deleting targetstore...")
		// check if this is the last one -> if so stop the client to the dataserver
		tctx, err := r.targetStore.Get(ctx, targetKey)
		if err != nil {
			// client does not exist
			if err := r.finalizer.RemoveFinalizer(ctx, target); err != nil {
				return ctrl.Result{Requeue: true},
					errors.Wrap(r.handleError(ctx, targetOrig, "cannot delete finalizer", err, true), errUpdateStatus)
			}
			return ctrl.Result{}, nil
		}
		// delete the mapping in the dataserver cache, which keeps track of all targets per dataserver
		r.deleteTargetFromDataServer(ctx, targetKey)
		log.Debug("deleted target from dataserver...")
		// delete the datastore
		if tctx.IsReady() {
			log.Debug("deleting datastore", "key", targetKey.String())
			if err := tctx.DeleteDS(ctx); err != nil {
				//log.Error("cannot delete datastore", "error", err)
				return ctrl.Result{Requeue: true},
					errors.Wrap(r.handleError(ctx, targetOrig, "cannot delete datastore", err, true), errUpdateStatus)
			}
			log.Debug("delete datastore succeeded")
		}

		// delete the target from the target store
		r.targetStore.Delete(ctx, targetKey)
		// remove the finalizer
		if err := r.finalizer.RemoveFinalizer(ctx, target); err != nil {
			return ctrl.Result{Requeue: true},
				errors.Wrap(r.handleError(ctx, targetOrig, "cannot delete finalizer", err, true), errUpdateStatus)
		}

		log.Debug("Successfully deleted resource")
		return ctrl.Result{}, nil
	}

	if err := r.finalizer.AddFinalizer(ctx, target); err != nil {
		return ctrl.Result{Requeue: true},
			errors.Wrap(r.handleError(ctx, targetOrig, "cannot add finalizer", err, true), errUpdateStatus)
	}

	// We dont act as long the target is not ready (rady state is handled by the discovery controller)
	// Ready -> NotReady: happens only when the discovery fails => we keep the target as is do not delete the datatore/etc
	log.Debug("target discovery ready condition", "status", target.Status.GetCondition(invv1alpha1.ConditionTypeDiscoveryReady).Status)
	if target.Status.GetCondition(invv1alpha1.ConditionTypeDiscoveryReady).Status != metav1.ConditionTrue {
		// target not ready so we can wait till the target goes to ready state
		return ctrl.Result{}, // requeue will happen automatically when discovery is done
			errors.Wrap(r.handleError(ctx, targetOrig, "discovery not ready", nil, true), errUpdateStatus)
	}

	isSchemaReady, _, err := r.isSchemaReady(ctx, target)
	if err != nil {
		// this means the apiserver had issues retrieving the schema
		return ctrl.Result{Requeue: true},
			errors.Wrap(r.handleError(ctx, targetOrig, "cannot get schema ready state", err, true), errUpdateStatus)
	}
	if !isSchemaReady {
		return ctrl.Result{RequeueAfter: 10 * time.Second},
			errors.Wrap(r.handleError(ctx, targetOrig, "schema not ready", err, true), errUpdateStatus)
	}

	// first check if the target has an assigned dataserver, if not allocate one
	// update the target store with the updated information
	curtctx, err := r.targetStore.Get(ctx, targetKey)
	if err != nil || !curtctx.IsReady() {
		// select a dataserver
		selectedDSctx, serr := r.selectDataServerContext(ctx)
		if serr != nil {
			log.Debug("cannot select a dataserver", "error", err)
			return ctrl.Result{},
				errors.Wrap(r.handleError(ctx, targetOrig, "schema not ready", err, true), errUpdateStatus)
		}
		// add the target to the DS
		r.addTargetToDataServer(ctx, storebackend.ToKey(selectedDSctx.DSClient.GetAddress()), targetKey)
		tctx := sdctarget.New(targetKey, r.Client, selectedDSctx.DSClient)
		// either update or create based on the previous error
		if err != nil {
			if err := r.targetStore.Create(ctx, targetKey, tctx); err != nil {
				return ctrl.Result{Requeue: true},
					errors.Wrap(r.handleError(ctx, targetOrig, "cannot create dataserver", err, true), errUpdateStatus)
			}
		} else {
			if err := r.targetStore.Update(ctx, targetKey, tctx); err != nil {
				return ctrl.Result{Requeue: true},
					errors.Wrap(r.handleError(ctx, targetOrig, "cannot update dataserver", err, true), errUpdateStatus)
			}
		}
	} else {
		// safety
		r.addTargetToDataServer(ctx, storebackend.ToKey(curtctx.GetAddress()), targetKey)
	}

	// Now that the target store is up to date and we have an assigned dataserver
	// we will create/update the datastore for the target
	// Target is ready
	_, usedRefs, err := r.updateDataStoreTargetReady(ctx, target)
	if err != nil {
		return ctrl.Result{Requeue: true},
			errors.Wrap(r.handleError(ctx, targetOrig, "cannot update target ready in dataserver", err, true), errUpdateStatus)
	}

	ready, err := r.getTargetStatus(ctx, target)
	if err != nil {
		return ctrl.Result{RequeueAfter: 5 * time.Second},
			errors.Wrap(r.handleError(ctx, targetOrig, "cannot get target ready status", err, false), errUpdateStatus)
	}
	if !ready {
		return ctrl.Result{RequeueAfter: 5 * time.Second},
			errors.Wrap(r.handleError(ctx, targetOrig, "target not ready", err, false), errUpdateStatus)
	}
	return ctrl.Result{}, errors.Wrap(r.handleSuccess(ctx, targetOrig, usedRefs), errUpdateStatus)
}

func (r *reconciler) handleSuccess(ctx context.Context, target *invv1alpha1.Target, usedRefs *invv1alpha1.TargetStatusUsedReferences) error {
	log := log.FromContext(ctx)
	log.Debug("handleSuccess", "key", target.GetNamespacedName(), "status old", target.DeepCopy().Status)
	// take a snapshot of the current object
	patch := client.MergeFrom(target.DeepCopy())
	// update status
	target.SetConditions(invv1alpha1.DatastoreReady())
	target.Status.UsedReferences = usedRefs
	//target.SetOverallStatus()
	r.recorder.Eventf(target, corev1.EventTypeNormal, invv1alpha1.TargetKind, "datastore ready")

	log.Debug("handleSuccess", "key", target.GetNamespacedName(), "status new", target.Status)

	return r.Client.Status().Patch(ctx, target, patch, &client.SubResourcePatchOptions{
		PatchOptions: client.PatchOptions{
			FieldManager: reconcilerName,
		},
	})
}

func (r *reconciler) handleError(ctx context.Context, target *invv1alpha1.Target, msg string, err error, resetUsedRefs bool) error {
	log := log.FromContext(ctx)
	// take a snapshot of the current object
	patch := client.MergeFrom(target.DeepCopy())

	if err != nil {
		msg = fmt.Sprintf("%s err %s", msg, err.Error())
	}
	target.SetConditions(invv1alpha1.DatastoreFailed(msg))
	//target.SetOverallStatus()
	if resetUsedRefs {
		target.Status.UsedReferences = nil
	}
	log.Error(msg, "error", err)
	r.recorder.Eventf(target, corev1.EventTypeWarning, invv1alpha1.TargetKind, msg)

	return r.Client.Status().Patch(ctx, target, patch, &client.SubResourcePatchOptions{
		PatchOptions: client.PatchOptions{
			FieldManager: reconcilerName,
		},
	})
}

func (r *reconciler) deleteTargetFromDataServer(ctx context.Context, targetKey storebackend.Key) {
	log := log.FromContext(ctx)
	dsKeys := []storebackend.Key{}
	r.dataServerStore.List(ctx, func(ctx context.Context, dsKey storebackend.Key, dsctx sdcctx.DSContext) {
		if dsctx.Targets.Has(targetKey.String()) {
			dsKeys = append(dsKeys, dsKey)
		}
	})
	for _, dsKey := range dsKeys {
		dsctx, err := r.dataServerStore.Get(ctx, dsKey)
		if err != nil {
			log.Debug("deleteTargetFromDataServer dataserver key not found", "dsKey", dsKey, "targetKey", targetKey, "error", err.Error())
			return
		}
		dsctx.Targets = dsctx.Targets.Delete(targetKey.String())
		if err := r.dataServerStore.Update(ctx, dsKey, dsctx); err != nil {
			log.Debug("deleteTargetFromDataServer dataserver update failed", "dsKey", dsKey, "targetKey", targetKey, "error", err.Error())
		}
	}
}

func (r *reconciler) addTargetToDataServer(ctx context.Context, dsKey storebackend.Key, targetKey storebackend.Key) {
	log := log.FromContext(ctx)
	if _, err := r.dataServerStore.Get(ctx, dsKey); err != nil {
		log.Debug("AddTarget2DataServer dataserver key not found", "dsKey", dsKey, "targetKey", targetKey, "error", err.Error())
		return
	}

	if err := r.dataServerStore.UpdateWithFn(ctx, func(ctx context.Context, key storebackend.Key, dsctx sdcctx.DSContext) sdcctx.DSContext {
		dsctx.Targets = dsctx.Targets.Insert(targetKey.String())
		return dsctx
	}); err != nil {
		log.Debug("AddTarget2DataServer dataserver update failed", "dsKey", dsKey, "targetKey", targetKey, "error", err.Error())
	}
}

// selectDataServerContext selects a dataserver for a particalur target based on
// least amount of targets assigned to a dataserver
func (r *reconciler) selectDataServerContext(ctx context.Context) (*sdcctx.DSContext, error) {
	log := log.FromContext(ctx)
	var err error
	selectedDSctx := &sdcctx.DSContext{}
	minTargets := 9999
	r.dataServerStore.List(ctx, func(ctx context.Context, k storebackend.Key, dsctx sdcctx.DSContext) {
		if dsctx.Targets.Len() == 0 || dsctx.Targets.Len() < minTargets {
			selectedDSctx = &dsctx
			minTargets = dsctx.Targets.Len()
		}
	})
	// create and start client if it does not exist
	if selectedDSctx.DSClient == nil {
		log.Debug("selectedDSctx", "selectedDSctx", selectedDSctx)

		selectedDSctx.DSClient, err = dsclient.New(selectedDSctx.Config)
		if err != nil {
			// happens when address or config is not set properly
			log.Error("cannot create dataserver client", "error", err)
			return nil, err
		}
		if err := selectedDSctx.DSClient.Start(ctx); err != nil {
			log.Error("cannot start dataserver client", "error", err)
			return nil, err
		}
	}
	return selectedDSctx, nil
}

// getTargetStatus
func (r *reconciler) getTargetStatus(ctx context.Context, cr *invv1alpha1.Target) (bool, error) {
	targetKey := storebackend.KeyFromNSN(types.NamespacedName{Namespace: cr.GetNamespace(), Name: cr.GetName()})
	log := log.FromContext(ctx).With("targetkey", targetKey.String())

	// this should always succeed
	tctx, err := r.targetStore.Get(ctx, targetKey)
	if err != nil {
		log.Error("cannot get datastore from store", "error", err)
		return false, err
	}
	// timeout to wait for target connection in the data-server since it is just created
	time.Sleep(1 * time.Second)
	resp, err := tctx.GetDataStore(ctx, &sdcpb.GetDataStoreRequest{Name: targetKey.String()})
	if err != nil {
		log.Error("cannot get target from the datastore", "key", targetKey.String(), "error", err)
		return false, err
	}
	log.Info("getTargetStatus", "status", resp.Target.Status.String(), "details", resp.Target.StatusDetails)
	if resp.Target.Status != sdcpb.TargetStatus_CONNECTED {
		if err := r.targetStore.UpdateWithFn(ctx, func(ctx context.Context, key storebackend.Key, tctx *sdctarget.Context) *sdctarget.Context {
			tctx.SetNotReady(ctx)
			return tctx
		}); err != nil {
			return true, err
		}
		return false, nil
	}

	if err := r.targetStore.UpdateWithFn(ctx, func(ctx context.Context, key storebackend.Key, tctx *sdctarget.Context) *sdctarget.Context {
		tctx.SetReady(ctx)
		return tctx
	}); err != nil {
		return true, err
	}
	return true, nil
}

// updateDataStore will do 1 of 3 things
// 1. create a datastore if none exists
// 2. delete/update the datastore if changes were detected
// 3. do nothing if no changes were detected.
func (r *reconciler) updateDataStoreTargetReady(ctx context.Context, target *invv1alpha1.Target) (bool, *invv1alpha1.TargetStatusUsedReferences, error) {
	targetKey := storebackend.KeyFromNSN(types.NamespacedName{Namespace: target.GetNamespace(), Name: target.GetName()})
	log := log.FromContext(ctx).With("targetkey", targetKey.String())
	changed := false
	req, usedRefs, err := r.getCreateDataStoreRequest(ctx, target)
	if err != nil {
		log.Error("cannot create datastore request from CR/Profiles", "error", err)
		return changed, nil, err
	}
	// this should always succeed
	tctx, err := r.targetStore.Get(ctx, targetKey)
	if err != nil {
		log.Error("cannot get datastore from store", "error", err)
		return changed, nil, err
	}
	// get the datastore from the dataserver
	getRsp, err := tctx.GetDataStore(ctx, &sdcpb.GetDataStoreRequest{Name: targetKey.String()})
	if err != nil {
		// datastore does not exist or dataserver is unhealthy
		if !strings.Contains(err.Error(), "unknown datastore") {
			tctx.SetNotReady(ctx)
			log.Error("cannot get datastore from dataserver", "error", err)
			return changed, nil, err
		}
		changed = true
		log.Debug("datastore does not exist -> create")
		r.recorder.Eventf(target, corev1.EventTypeNormal,
			"datastore", "create")
		if err := tctx.CreateDS(ctx, req); err != nil {
			return changed, nil, err
		}
		if err := r.targetStore.Update(ctx, targetKey, tctx); err != nil {
			log.Error("cannot update datastore in store", "error", err)
			return changed, nil, err
		}
		return changed, usedRefs, nil
	}
	// datastore exists -> validate changes and if so delete the datastore
	if !r.hasDataStoreChanged(ctx, req, getRsp, target, usedRefs) {
		log.Debug("datastore exist -> no change")
		return changed, usedRefs, nil
	}
	changed = true
	log.Debug("datastore exist -> changed")
	r.recorder.Eventf(target, corev1.EventTypeNormal,
		"datastore", "delete")
	if err := tctx.DeleteDS(ctx); err != nil {
		return changed, nil, err
	}
	r.recorder.Eventf(target, corev1.EventTypeNormal,
		"datastore", "create")
	if err := tctx.CreateDS(ctx, req); err != nil {
		return changed, nil, err
	}
	if err := r.targetStore.Update(ctx, targetKey, tctx); err != nil {
		log.Error("cannot update datastore in store", "error", err)
		return changed, nil, err
	}
	return changed, usedRefs, nil
}

func (r *reconciler) hasDataStoreChanged(
	ctx context.Context,
	req *sdcpb.CreateDataStoreRequest,
	rsp *sdcpb.GetDataStoreResponse,
	target *invv1alpha1.Target,
	usedRefs *invv1alpha1.TargetStatusUsedReferences,
) bool {
	log := log.FromContext(ctx)
	log.Debug("hasDataStoreChanged",
		"name", fmt.Sprintf("%s/%s", req.Name, rsp.Name),
		"schema Name", fmt.Sprintf("%s/%s", req.Schema.Name, rsp.Schema.Name),
		"schema Vendor", fmt.Sprintf("%s/%s", req.Schema.Vendor, rsp.Schema.Vendor),
		"schema Version", fmt.Sprintf("%s/%s", req.Schema.Version, rsp.Schema.Version),
		"target Type", fmt.Sprintf("%s/%s", req.Target.Type, rsp.Target.Type),
		"target Address", fmt.Sprintf("%s/%s", req.Target.Address, rsp.Target.Address),
	)
	if req.Name != rsp.Name {
		return true
	}

	if req.Schema.Name != rsp.Schema.Name ||
		req.Schema.Vendor != rsp.Schema.Vendor ||
		req.Schema.Version != rsp.Schema.Version {
		return true
	}
	if req.Target.Type != rsp.Target.Type ||
		req.Target.Address != rsp.Target.Address {
		return true
	}

	if target.Status.UsedReferences == nil {
		log.Debug("hasDataStoreChanged", "UsedReferences", "nil")
		return true
	}

	log.Debug("hasDataStoreChanged",
		"ConnectionProfileResourceVersion", fmt.Sprintf("%s/%s", target.Status.UsedReferences.ConnectionProfileResourceVersion, usedRefs.ConnectionProfileResourceVersion),
		"SyncProfileResourceVersion", fmt.Sprintf("%s/%s", target.Status.UsedReferences.SyncProfileResourceVersion, usedRefs.SyncProfileResourceVersion),
		"SecretResourceVersion", fmt.Sprintf("%s/%s", target.Status.UsedReferences.SecretResourceVersion, usedRefs.SecretResourceVersion),
	)

	if target.Status.UsedReferences.ConnectionProfileResourceVersion != usedRefs.ConnectionProfileResourceVersion ||
		target.Status.UsedReferences.SyncProfileResourceVersion != usedRefs.SyncProfileResourceVersion ||
		target.Status.UsedReferences.SecretResourceVersion != usedRefs.SecretResourceVersion {
		// TODO TLS
		return true
	}

	dsReaadyCondition := target.Status.GetCondition(invv1alpha1.ConditionTypeDatastoreReady)
	if dsReaadyCondition.Status == metav1.ConditionFalse {
		log.Debug("hasDataStoreChanged", "DS Ready condition", dsReaadyCondition)
		return true
	}

	return false
}

func (r *reconciler) getConnProfile(ctx context.Context, key types.NamespacedName) (*invv1alpha1.TargetConnectionProfile, error) {
	profile := &invv1alpha1.TargetConnectionProfile{}
	if err := r.Get(ctx, key, profile); err != nil {
		if resource.IgnoreNotFound(err) != nil {
			return nil, err
		}
		return invv1alpha1.DefaultTargetConnectionProfile(), nil
	}
	return profile, nil
}

func (r *reconciler) getSyncProfile(ctx context.Context, key types.NamespacedName) (*invv1alpha1.TargetSyncProfile, error) {
	profile := &invv1alpha1.TargetSyncProfile{}
	if err := r.Get(ctx, key, profile); err != nil {
		if resource.IgnoreNotFound(err) != nil {
			return nil, err
		}
		return invv1alpha1.DefaultTargetSyncProfile(), nil
	}
	return profile, nil
}

func (r *reconciler) getSecret(ctx context.Context, key types.NamespacedName) (*corev1.Secret, error) {
	secret := &corev1.Secret{}
	if err := r.Get(ctx, key, secret); err != nil {
		return nil, err
	}
	return secret, nil
}

func (r *reconciler) isSchemaReady(ctx context.Context, target *invv1alpha1.Target) (bool, string, error) {
	//log := log.FromContext(ctx)
	schemaList := &invv1alpha1.SchemaList{}
	opts := []client.ListOption{
		client.InNamespace(target.Namespace),
	}

	if err := r.List(ctx, schemaList, opts...); err != nil {
		return false, "", err
	}

	for _, schema := range schemaList.Items {
		if target.Status.DiscoveryInfo == nil {
			return false, "target has no discovery info", nil
		}
		if schema.Spec.Provider == target.Status.DiscoveryInfo.Provider &&
			schema.Spec.Version == target.Status.DiscoveryInfo.Version {
			schemaCondition := schema.GetCondition(condv1alpha1.ConditionTypeReady)
			return schemaCondition.IsTrue(), schemaCondition.Message, nil
		}
	}
	return false, "schema referenced by target not found", nil

}

func (r *reconciler) getCreateDataStoreRequest(ctx context.Context, target *invv1alpha1.Target) (*sdcpb.CreateDataStoreRequest, *invv1alpha1.TargetStatusUsedReferences, error) {
	usedReferences := &invv1alpha1.TargetStatusUsedReferences{}

	syncProfile, err := r.getSyncProfile(ctx, types.NamespacedName{Namespace: target.GetNamespace(), Name: *target.Spec.SyncProfile})
	if err != nil {
		return nil, nil, err
	}
	usedReferences.SyncProfileResourceVersion = syncProfile.GetResourceVersion()
	connProfile, err := r.getConnProfile(ctx, types.NamespacedName{Namespace: target.GetNamespace(), Name: target.Spec.ConnectionProfile})
	if err != nil {
		return nil, nil, err
	}
	usedReferences.ConnectionProfileResourceVersion = connProfile.ResourceVersion
	secret, err := r.getSecret(ctx, types.NamespacedName{Namespace: target.GetNamespace(), Name: target.Spec.Credentials})
	if err != nil {
		return nil, nil, err
	}
	usedReferences.SecretResourceVersion = secret.ResourceVersion

	// if insecure is set we expect tls == nil
	// if either SkipVerify is true or a TLS secret is provided TLS is enabled
	// and we need to provide the tls conext with the relevant info
	// skipVery bool and secret information if the TLS secret is set.
	var tls *sdcpb.TLS
	if connProfile.IsInsecure() {
		tls = &sdcpb.TLS{
			SkipVerify: connProfile.SkipVerify(),
		}
		if target.Spec.TLSSecret != nil {
			tlsSecret, err := r.getSecret(ctx, types.NamespacedName{Namespace: target.GetNamespace(), Name: *target.Spec.TLSSecret})
			if err != nil {
				return nil, nil, err
			}
			tls.Ca = string(tlsSecret.Data["ca"])
			tls.Cert = string(tlsSecret.Data["cert"])
			tls.Key = string(tlsSecret.Data["key"])
			usedReferences.TLSSecretResourceVersion = tlsSecret.ResourceVersion
		}
	}

	// check if the discovery info is complete
	if target.Status.DiscoveryInfo == nil ||
		target.Status.DiscoveryInfo.Provider == "" ||
		target.Status.DiscoveryInfo.Version == "" {
		return nil, nil, fmt.Errorf("target not discovered, discovery incomplete, got: %v", target.Status.DiscoveryInfo)
	}

	req := &sdcpb.CreateDataStoreRequest{
		Name: storebackend.KeyFromNSN(types.NamespacedName{Namespace: target.Namespace, Name: target.Name}).String(),
		Target: &sdcpb.Target{
			Type:    string(connProfile.Spec.Protocol),
			Address: target.Spec.Address,
			Credentials: &sdcpb.Credentials{
				Username: string(secret.Data["username"]),
				Password: string(secret.Data["password"]),
			},
			Tls:  tls,
			Port: uint32(connProfile.Spec.Port),
		},
		Schema: &sdcpb.Schema{
			Name:    "",
			Vendor:  target.Status.DiscoveryInfo.Provider,
			Version: target.Status.DiscoveryInfo.Version,
		},
	}

	if connProfile.Spec.Protocol == invv1alpha1.Protocol_GNMI {
		req.Target.ProtocolOptions = &sdcpb.Target_GnmiOpts{
			GnmiOpts: &sdcpb.GnmiOptions{
				Encoding: string(connProfile.Encoding()),
			},
		}
	}
	if connProfile.Spec.Protocol == invv1alpha1.Protocol_NETCONF {
		commitCandidate := sdcpb.CommitCandidate_COMMIT_CANDIDATE
		if connProfile.CommitCandidate() == invv1alpha1.CommitCandidate_Running {
			commitCandidate = sdcpb.CommitCandidate_COMMIT_RUNNING
		}
		req.Target.ProtocolOptions = &sdcpb.Target_NetconfOpts{
			NetconfOpts: &sdcpb.NetconfOptions{
				IncludeNs:          connProfile.IncludeNS(),
				OperationWithNs:    connProfile.OperationWithNS(),
				UseOperationRemove: connProfile.UseOperationRemove(),
				CommitCandidate:    commitCandidate,
			},
		}
	}

	req.Sync = invv1alpha1.GetSyncProfile(syncProfile, req.Target)

	return req, usedReferences, nil
}
