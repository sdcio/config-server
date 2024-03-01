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
	invv1alpha1 "github.com/sdcio/config-server/apis/inv/v1alpha1"
	"github.com/sdcio/config-server/pkg/lease"
	"github.com/sdcio/config-server/pkg/reconcilers"
	"github.com/sdcio/config-server/pkg/reconcilers/ctrlconfig"
	"github.com/sdcio/config-server/pkg/reconcilers/resource"
	sdcctx "github.com/sdcio/config-server/pkg/sdc/ctx"
	dsclient "github.com/sdcio/config-server/pkg/sdc/dataserver/client"
	"github.com/sdcio/config-server/pkg/target"
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
	reconcilers.Register("targetdatastore", &reconciler{})
}

const (
	controllerName = "TargetDataStoreController"
	finalizer      = "targetdatastore.inv.sdcio.dev/finalizer"
	// errors
	errGetCr           = "cannot get cr"
	errUpdateDataStore = "cannot update datastore"
	errUpdateStatus    = "cannot update status"
)

type adder interface {
	Add(item interface{})
}

//+kubebuilder:rbac:groups=inv.sdcio.dev,resources=targets,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups=inv.sdcio.dev,resources=targets/status,verbs=get;update;patch

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
	r.targetStore = cfg.TargetStore
	r.dataServerStore = cfg.DataServerStore
	r.recorder = mgr.GetEventRecorderFor(controllerName)

	targetDSWatcher := newTargetDataStoreWatcher(mgr.GetClient(), cfg.TargetStore)
	go targetDSWatcher.Start(ctx)

	return nil, ctrl.NewControllerManagedBy(mgr).
		Named(controllerName).
		For(&invv1alpha1.Target{}).
		Watches(&invv1alpha1.TargetConnectionProfile{}, &targetConnProfileEventHandler{client: mgr.GetClient()}).
		Watches(&invv1alpha1.TargetSyncProfile{}, &targetSyncProfileEventHandler{client: mgr.GetClient()}).
		Watches(&corev1.Secret{}, &secretEventHandler{client: mgr.GetClient()}).
		Complete(r)
}

type reconciler struct {
	client.Client
	finalizer       *resource.APIFinalizer
	targetStore     storebackend.Storer[*target.Context]
	dataServerStore storebackend.Storer[sdcctx.DSContext]
	recorder        record.EventRecorder
}

func (r *reconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	ctx = ctrlconfig.InitContext(ctx, controllerName, req.NamespacedName)
	log := log.FromContext(ctx)
	log.Info("reconcile")

	targetKey := storebackend.KeyFromNSN(req.NamespacedName)

	cr := &invv1alpha1.Target{}
	if err := r.Get(ctx, req.NamespacedName, cr); err != nil {
		// if the resource no longer exists the reconcile loop is done
		if resource.IgnoreNotFound(err) != nil {
			log.Error(errGetCr, "error", err)
			return ctrl.Result{}, errors.Wrap(resource.IgnoreNotFound(err), errGetCr)
		}
		return ctrl.Result{}, nil
	}

	cr = cr.DeepCopy()

	l := lease.New(r.Client, targetKey.NamespacedName)
	if err := l.AcquireLease(ctx, "TargetDataStoreController"); err != nil {
		log.Debug("cannot acquire lease", "targetKey", targetKey.String(), "error", err.Error())
		r.recorder.Eventf(cr, corev1.EventTypeWarning,
			"lease", "error %s", err.Error())
		return ctrl.Result{Requeue: true, RequeueAfter: lease.RequeueInterval}, nil
	}
	r.recorder.Eventf(cr, corev1.EventTypeWarning,
		"lease", "acquired")

	if !cr.GetDeletionTimestamp().IsZero() {
		cr.SetConditions(invv1alpha1.DatastoreFailed("target deleting"))
		cr.SetOverallStatus()
		if len(cr.GetFinalizers()) > 1 {
			// this should be the last finalizer to be removed as this deletes the target from the taregtStore
			log.Debug("requeue delete, not all finalizers removed", "finalizers", cr.GetFinalizers())
			return ctrl.Result{Requeue: true}, errors.Wrap(r.Status().Update(ctx, cr), errUpdateStatus)
		}
		log.Debug("deleting targetstore...")
		// check if this is the last one -> if so stop the client to the dataserver
		tctx, err := r.targetStore.Get(ctx, targetKey)
		if err != nil {
			// client does not exist
			if err := r.finalizer.RemoveFinalizer(ctx, cr); err != nil {
				log.Debug("cannot remove finalizer", "error", err)
				cr.Status.UsedReferences = nil
				cr.SetConditions(invv1alpha1.DatastoreFailed(err.Error()))
				cr.SetOverallStatus()
				r.recorder.Eventf(cr, corev1.EventTypeWarning,
					"Error", "error %s", err.Error())
				return ctrl.Result{Requeue: true}, errors.Wrap(r.Status().Update(ctx, cr), errUpdateStatus)
			}
			log.Debug("Successfully deleted resource, with non existing client -> strange")
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
				cr.Status.UsedReferences = nil
				cr.SetConditions(invv1alpha1.DatastoreFailed(err.Error()))
				cr.SetOverallStatus()
				r.recorder.Eventf(cr, corev1.EventTypeWarning,
					"Error", "error %s", err.Error())
				return ctrl.Result{Requeue: true}, errors.Wrap(r.Status().Update(ctx, cr), errUpdateStatus)
			}
			log.Debug("delete datastore succeeded")
		}

		// delete the target from the target store
		r.targetStore.Delete(ctx, targetKey)
		// remove the finalizer
		if err := r.finalizer.RemoveFinalizer(ctx, cr); err != nil {
			//log.Error("cannot remove finalizer", "error", err)
			cr.Status.UsedReferences = nil
			cr.SetConditions(invv1alpha1.DatastoreFailed(err.Error()))
			cr.SetOverallStatus()
			r.recorder.Eventf(cr, corev1.EventTypeWarning,
				"Error", "error %s", err.Error())
			return ctrl.Result{Requeue: true}, errors.Wrap(r.Status().Update(ctx, cr), errUpdateStatus)
		}

		log.Debug("Successfully deleted resource")
		return ctrl.Result{}, nil
	}

	if err := r.finalizer.AddFinalizer(ctx, cr); err != nil {
		log.Debug("cannot add finalizer", "error", err)
		cr.Status.UsedReferences = nil
		cr.SetConditions(invv1alpha1.DatastoreFailed(err.Error()))
		cr.SetOverallStatus()
		r.recorder.Eventf(cr, corev1.EventTypeWarning,
			"Error", "error %s", err.Error())
		return ctrl.Result{Requeue: true}, errors.Wrap(r.Status().Update(ctx, cr), errUpdateStatus)
	}

	// We dont act as long the target is not ready (rady state is handled by the discovery controller)
	// Ready -> NotReady: happens only when the discovery fails => we keep the target as is do not delete the datatore/etc
	log.Debug("target discovery ready condition", "status", cr.Status.GetCondition(invv1alpha1.ConditionTypeDiscoveryReady).Status)
	if cr.Status.GetCondition(invv1alpha1.ConditionTypeDiscoveryReady).Status != metav1.ConditionTrue {
		// target not ready so we can wait till the target goes to ready state
		cr.Status.UsedReferences = nil
		cr.SetConditions(invv1alpha1.DatastoreFailed("target discovery not ready"))
		cr.SetOverallStatus()
		r.recorder.Eventf(cr, corev1.EventTypeWarning,
			"datastore", "discovery not ready")
		return ctrl.Result{}, errors.Wrap(r.Status().Update(ctx, cr), errUpdateStatus)
	}

	// first check if the target has an assigned dataserver, if not allocate one
	// update the target store with the updated information
	curtctx, err := r.targetStore.Get(ctx, targetKey)
	if err != nil || !curtctx.IsReady() {
		// select a dataserver
		selectedDSctx, serr := r.selectDataServerContext(ctx)
		if serr != nil {
			log.Debug("cannot select a dataserver", "error", err)
			cr.Status.UsedReferences = nil
			cr.SetConditions(invv1alpha1.DatastoreFailed(err.Error()))
			cr.SetOverallStatus()
			r.recorder.Eventf(cr, corev1.EventTypeWarning,
				"Error", "error %s", err.Error())
			return ctrl.Result{}, errors.Wrap(r.Status().Update(ctx, cr), errUpdateStatus)
		}
		// add the target to the DS
		r.addTargetToDataServer(ctx, storebackend.ToKey(selectedDSctx.DSClient.GetAddress()), targetKey)
		tctx := target.New(targetKey, r.Client, selectedDSctx.DSClient)
		// either update or create based on the previous error
		if err != nil {
			r.recorder.Eventf(cr, corev1.EventTypeNormal,
				"datastore", "create")
			if err := r.targetStore.Create(ctx, targetKey, tctx); err != nil {
				log.Error("create targetStore failed", "error", err.Error())
				cr.Status.UsedReferences = nil
				cr.SetConditions(invv1alpha1.DatastoreFailed(err.Error()))
				cr.SetOverallStatus()
				r.recorder.Eventf(cr, corev1.EventTypeWarning,
					"Error", "error %s", err.Error())
				return ctrl.Result{Requeue: true}, errors.Wrap(r.Status().Update(ctx, cr), errUpdateStatus)
			}
		} else {
			r.recorder.Eventf(cr, corev1.EventTypeNormal,
				"datastore", "update")
			if err := r.targetStore.Update(ctx, targetKey, tctx); err != nil {
				log.Error("update targetStore failed", "error", err.Error())
				cr.Status.UsedReferences = nil
				cr.SetConditions(invv1alpha1.DatastoreFailed(err.Error()))
				cr.SetOverallStatus()
				r.recorder.Eventf(cr, corev1.EventTypeWarning,
					"Error", "error %s", err.Error())
				return ctrl.Result{Requeue: true}, errors.Wrap(r.Status().Update(ctx, cr), errUpdateStatus)
			}
		}
	} else {
		// safety
		r.addTargetToDataServer(ctx, storebackend.ToKey(curtctx.GetAddress()), targetKey)
	}

	isSchemaReady, schemaMsg, err := r.isSchemaReady(ctx, cr)
	if err != nil {
		log.Error("cannot get schema ready state", "error", err)
		cr.Status.UsedReferences = nil
		cr.SetConditions(invv1alpha1.DatastoreFailed(err.Error()))
		cr.SetOverallStatus()
		r.recorder.Eventf(cr, corev1.EventTypeWarning,
			"datastore", "schema not ready")
		return ctrl.Result{Requeue: true}, errors.Wrap(r.Status().Update(ctx, cr), errUpdateStatus)
	}
	if !isSchemaReady {
		log.Info("schema ready state", "ready", isSchemaReady, "msg", schemaMsg)
		cr.Status.UsedReferences = nil
		cr.SetConditions(invv1alpha1.DatastoreSchemaNotReady(schemaMsg))
		cr.SetOverallStatus()
		r.recorder.Eventf(cr, corev1.EventTypeWarning,
			"datastore", "schema not ready")
		return ctrl.Result{RequeueAfter: 10 * time.Second}, errors.Wrap(r.Status().Update(ctx, cr), errUpdateStatus)
	}
	// Now that the target store is up to date and we have an assigned dataserver
	// we will create/update the datastore for the target
	// Target is ready
	changed, usedRefs, err := r.updateDataStoreTargetReady(ctx, cr)
	if err != nil {
		cr.Status.UsedReferences = nil
		cr.SetConditions(invv1alpha1.DatastoreFailed(err.Error()))
		cr.SetOverallStatus()
		r.recorder.Eventf(cr, corev1.EventTypeWarning,
			"Error", "error %s", err.Error())
		return ctrl.Result{Requeue: true}, errors.Wrap(r.Status().Update(ctx, cr), errUpdateStatus)
	}
	// robustness avoid to update status when there is no change
	// avoid retriggering reconcile
	if changed {
		cr.Status.UsedReferences = usedRefs
		cr.SetConditions(invv1alpha1.DatastoreReady())
		cr.SetOverallStatus()
		r.recorder.Eventf(cr, corev1.EventTypeNormal,
			"datastore", "ready")
		return ctrl.Result{}, errors.Wrap(r.Status().Update(ctx, cr), errUpdateStatus)
	}
	cr.Status.UsedReferences = usedRefs
	cr.SetConditions(invv1alpha1.DatastoreReady())
	cr.SetOverallStatus()
	r.recorder.Eventf(cr, corev1.EventTypeNormal,
		"datastore", "ready")
	return ctrl.Result{}, errors.Wrap(r.Status().Update(ctx, cr), errUpdateStatus)

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
	dsctx, err := r.dataServerStore.Get(ctx, dsKey)
	if err != nil {
		log.Debug("AddTarget2DataServer dataserver key not found", "dsKey", dsKey, "targetKey", targetKey, "error", err.Error())
		return
	}
	dsctx.Targets = dsctx.Targets.Insert(targetKey.String())
	if err := r.dataServerStore.Update(ctx, dsKey, dsctx); err != nil {
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

// updateDataStore will do 1 of 3 things
// 1. create a datastore if none exists
// 2. delete/update the datastore if changes were detected
// 3. do nothing if no changes were detected.
func (r *reconciler) updateDataStoreTargetReady(ctx context.Context, cr *invv1alpha1.Target) (bool, *invv1alpha1.TargetStatusUsedReferences, error) {
	targetKey := storebackend.KeyFromNSN(types.NamespacedName{Namespace: cr.GetNamespace(), Name: cr.GetName()})
	log := log.FromContext(ctx).With("targetkey", targetKey.String())
	changed := false
	req, usedRefs, err := r.getCreateDataStoreRequest(ctx, cr)
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
		// datastore does not exist
		if !strings.Contains(err.Error(), "unknown datastore") {
			log.Error("cannot get datastore from dataserver", "error", err)
			return changed, nil, err
		}
		changed = true
		log.Debug("datastore does not exist -> create")
		r.recorder.Eventf(cr, corev1.EventTypeNormal,
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
	if !r.hasDataStoreChanged(ctx, req, getRsp, cr, usedRefs) {
		log.Debug("datastore exist -> no change")
		return changed, usedRefs, nil
	}
	changed = true
	log.Debug("datastore exist -> changed")
	r.recorder.Eventf(cr, corev1.EventTypeNormal,
		"datastore", "delete")
	if err := tctx.DeleteDS(ctx); err != nil {
		return changed, nil, err
	}
	r.recorder.Eventf(cr, corev1.EventTypeNormal,
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
	cr *invv1alpha1.Target,
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

	if cr.Status.UsedReferences == nil {
		log.Debug("hasDataStoreChanged", "UsedReferences", "nil")
		return true
	}

	log.Debug("hasDataStoreChanged",
		"ConnectionProfileResourceVersion", fmt.Sprintf("%s/%s", cr.Status.UsedReferences.ConnectionProfileResourceVersion, usedRefs.ConnectionProfileResourceVersion),
		"SyncProfileResourceVersion", fmt.Sprintf("%s/%s", cr.Status.UsedReferences.SyncProfileResourceVersion, usedRefs.SyncProfileResourceVersion),
		"SecretResourceVersion", fmt.Sprintf("%s/%s", cr.Status.UsedReferences.SecretResourceVersion, usedRefs.SecretResourceVersion),
	)

	if cr.Status.UsedReferences.ConnectionProfileResourceVersion != usedRefs.ConnectionProfileResourceVersion ||
		cr.Status.UsedReferences.SyncProfileResourceVersion != usedRefs.SyncProfileResourceVersion ||
		cr.Status.UsedReferences.SecretResourceVersion != usedRefs.SecretResourceVersion {
		// TODO TLS
		return true
	}

	dsReaadyCondition := cr.Status.GetCondition(invv1alpha1.ConditionTypeDatastoreReady)
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

func (r *reconciler) isSchemaReady(ctx context.Context, cr *invv1alpha1.Target) (bool, string, error) {
	//log := log.FromContext(ctx)
	schemaList := &invv1alpha1.SchemaList{}
	opts := []client.ListOption{
		client.InNamespace(cr.Namespace),
	}

	if err := r.List(ctx, schemaList, opts...); err != nil {
		return false, "", err
	}

	for _, schema := range schemaList.Items {
		if schema.Spec.Provider == cr.Status.DiscoveryInfo.Provider &&
			schema.Spec.Version == cr.Status.DiscoveryInfo.Version {
			schemaCondition := schemaList.Items[0].GetCondition(invv1alpha1.ConditionTypeReady)
			return schemaCondition.Status == metav1.ConditionTrue, schemaCondition.Message, nil
		}
	}
	return false, "schema referenced by target not found", nil

}

func (r *reconciler) getCreateDataStoreRequest(ctx context.Context, cr *invv1alpha1.Target) (*sdcpb.CreateDataStoreRequest, *invv1alpha1.TargetStatusUsedReferences, error) {
	usedReferences := &invv1alpha1.TargetStatusUsedReferences{}

	syncProfile, err := r.getSyncProfile(ctx, types.NamespacedName{Namespace: cr.GetNamespace(), Name: *cr.Spec.SyncProfile})
	if err != nil {
		return nil, nil, err
	}
	usedReferences.SyncProfileResourceVersion = syncProfile.GetResourceVersion()
	connProfile, err := r.getConnProfile(ctx, types.NamespacedName{Namespace: cr.GetNamespace(), Name: cr.Spec.ConnectionProfile})
	if err != nil {
		return nil, nil, err
	}
	usedReferences.ConnectionProfileResourceVersion = connProfile.ResourceVersion
	secret, err := r.getSecret(ctx, types.NamespacedName{Namespace: cr.GetNamespace(), Name: cr.Spec.Credentials})
	if err != nil {
		return nil, nil, err
	}
	usedReferences.SecretResourceVersion = secret.ResourceVersion

	var tls *sdcpb.TLS
	tls = &sdcpb.TLS{
		SkipVerify: connProfile.Spec.SkipVerify,
	}
	if cr.Spec.TLSSecret != nil {
		tls = &sdcpb.TLS{
			SkipVerify: connProfile.Spec.SkipVerify,
		}
		tlsSecret, err := r.getSecret(ctx, types.NamespacedName{Namespace: cr.GetNamespace(), Name: *cr.Spec.TLSSecret})
		if err != nil {
			return nil, nil, err
		}
		tls.Ca = string(tlsSecret.Data["ca"])
		tls.Cert = string(tlsSecret.Data["cert"])
		tls.Key = string(tlsSecret.Data["key"])
		usedReferences.TLSSecretResourceVersion = tlsSecret.ResourceVersion
	}

	// check if the discovery info is complete
	if cr.Status.DiscoveryInfo == nil ||
		cr.Status.DiscoveryInfo.Provider == "" ||
		cr.Status.DiscoveryInfo.Version == "" {
		return nil, nil, fmt.Errorf("target not discovered, discovery incomplete, got: %v", cr.Status.DiscoveryInfo)
	}

	commitCandidate := sdcpb.CommitCandidate_COMMIT_CANDIDATE
	if connProfile.Spec.CommitCandidate == invv1alpha1.CommitCandidate_Running {
		commitCandidate = sdcpb.CommitCandidate_COMMIT_RUNNING
	}

	return &sdcpb.CreateDataStoreRequest{
		Name: storebackend.KeyFromNSN(types.NamespacedName{Namespace: cr.Namespace, Name: cr.Name}).String(),
		Target: &sdcpb.Target{
			Type:    string(connProfile.Spec.Protocol),
			Address: cr.Spec.Address,
			Credentials: &sdcpb.Credentials{
				Username: string(secret.Data["username"]),
				Password: string(secret.Data["password"]),
			},
			Tls:                tls,
			IncludeNs:          connProfile.Spec.IncludeNS,
			OperationWithNs:    connProfile.Spec.OperationWithNS,
			UseOperationRemove: connProfile.Spec.UseOperationRemove,
			CommitCandidate:    commitCandidate,
		},
		Sync: invv1alpha1.GetSyncProfile(syncProfile),
		Schema: &sdcpb.Schema{
			Name:    "",
			Vendor:  cr.Status.DiscoveryInfo.Provider,
			Version: cr.Status.DiscoveryInfo.Version,
		},
	}, usedReferences, nil
}
