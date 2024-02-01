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

	"github.com/henderiw/logger/log"
	invv1alpha1 "github.com/iptecharch/config-server/apis/inv/v1alpha1"
	"github.com/iptecharch/config-server/pkg/lease"
	"github.com/iptecharch/config-server/pkg/reconcilers"
	"github.com/iptecharch/config-server/pkg/reconcilers/ctrlconfig"
	"github.com/iptecharch/config-server/pkg/reconcilers/resource"
	sdcctx "github.com/iptecharch/config-server/pkg/sdc/ctx"
	dsclient "github.com/iptecharch/config-server/pkg/sdc/dataserver/client"
	"github.com/iptecharch/config-server/pkg/store"
	"github.com/iptecharch/config-server/pkg/target"
	sdcpb "github.com/iptecharch/sdc-protos/sdcpb"
	"github.com/pkg/errors"
	"google.golang.org/protobuf/encoding/prototext"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
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
	//r.configStore = cfg.ConfigStore
	r.targetStore = cfg.TargetStore
	r.dataServerStore = cfg.DataServerStore

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
	finalizer *resource.APIFinalizer

	targetStore     store.Storer[target.Context]
	dataServerStore store.Storer[sdcctx.DSContext]
}

func (r *reconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	ctx = ctrlconfig.InitContext(ctx, controllerName, req.NamespacedName)
	log := log.FromContext(ctx)
	log.Info("reconcile")

	targetKey := store.KeyFromNSN(req.NamespacedName)

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

	if !cr.GetDeletionTimestamp().IsZero() {
		// check if this is the last one -> if so stop the client to the dataserver
		targetCtx, err := r.targetStore.Get(ctx, targetKey)
		if err != nil {
			// client does not exist
			if err := r.finalizer.RemoveFinalizer(ctx, cr); err != nil {
				log.Error("cannot remove finalizer", "error", err)
				cr.Status.UsedReferences = nil
				cr.SetConditions(invv1alpha1.DSFailed(err.Error()))
				return ctrl.Result{Requeue: true}, errors.Wrap(r.Status().Update(ctx, cr), errUpdateStatus)
			}
			log.Info("Successfully deleted resource, with non existing client -> strange")
			return ctrl.Result{}, nil
		}
		// delete the mapping in the dataserver cache, which keeps track of all targets per dataserver
		r.deleteTargetFromDataServer(ctx, store.ToKey(targetCtx.Client.GetAddress()), targetKey)
		// delete the datastore
		if targetCtx.DataStore != nil {
			log.Info("deleting datastore", "key", targetKey.String())
			rsp, err := targetCtx.Client.DeleteDataStore(ctx, &sdcpb.DeleteDataStoreRequest{Name: targetKey.String()})
			if err != nil {
				log.Error("cannot delete datastore", "error", err)
				cr.Status.UsedReferences = nil
				cr.SetConditions(invv1alpha1.DSFailed(err.Error()))
				return ctrl.Result{Requeue: true}, errors.Wrap(r.Status().Update(ctx, cr), errUpdateStatus)
			}
			log.Info("delete datastore succeeded", "resp", prototext.Format(rsp))
		}

		// delete the target from the target store
		r.targetStore.Delete(ctx, store.KeyFromNSN(req.NamespacedName))
		// remove the finalizer
		if err := r.finalizer.RemoveFinalizer(ctx, cr); err != nil {
			log.Error("cannot remove finalizer", "error", err)
			cr.Status.UsedReferences = nil
			cr.SetConditions(invv1alpha1.DSFailed(err.Error()))
			return ctrl.Result{Requeue: true}, errors.Wrap(r.Status().Update(ctx, cr), errUpdateStatus)
		}

		log.Info("Successfully deleted resource")
		return ctrl.Result{}, nil
	}

	if err := r.finalizer.AddFinalizer(ctx, cr); err != nil {
		log.Error("cannot add finalizer", "error", err)
		cr.Status.UsedReferences = nil
		cr.SetConditions(invv1alpha1.DSFailed(err.Error()))
		return ctrl.Result{Requeue: true}, errors.Wrap(r.Status().Update(ctx, cr), errUpdateStatus)
	}

	l := r.getLease(ctx, targetKey)
	if err := l.AcquireLease(ctx, "TargetDataStoreController"); err != nil {
		log.Info("cannot acquire lease", "targetKey", targetKey.String(), "error", err.Error())
		return ctrl.Result{Requeue: true, RequeueAfter: lease.RequeueInterval}, nil
	}

	// We dont act as long the target is not ready (rady state is handled by the discovery controller)
	// Ready -> NotReady: happens only when the discovery fails => we keep the target as is do not delete the datatore/etc
	log.Info("target ready condition", "status", cr.Status.GetCondition(invv1alpha1.ConditionTypeReady).Status)
	cr.SetConditions(invv1alpha1.DSFailed("target not ready"))
	if cr.Status.GetCondition(invv1alpha1.ConditionTypeReady).Status != metav1.ConditionTrue {
		// target not ready so we can wait till the target goes to ready state
		cr.Status.UsedReferences = nil
		cr.SetConditions(invv1alpha1.DSFailed("target not ready"))
		return ctrl.Result{}, errors.Wrap(r.Status().Update(ctx, cr), errUpdateStatus)
	}

	// first check if the target has an assigned dataserver, if not allocate one
	// update the target store with the updated information
	currentTargetCtx, err := r.targetStore.Get(ctx, targetKey)
	if err != nil || currentTargetCtx.Client == nil {
		selectedDSctx, err := r.selectDataServerContext(ctx)
		if err != nil {
			log.Error("cannot select a dataserver", "error", err)
			cr.Status.UsedReferences = nil
			cr.SetConditions(invv1alpha1.DSFailed(err.Error()))
			return ctrl.Result{}, errors.Wrap(r.Status().Update(ctx, cr), errUpdateStatus)
		}
		// add the target to the DS
		r.addTargetToDataServer(ctx, store.ToKey(selectedDSctx.DSClient.GetAddress()), targetKey)
		// create the target in the target store
		if currentTargetCtx.Client == nil {
			currentTargetCtx.Client = selectedDSctx.DSClient
			if err := r.targetStore.Update(ctx, targetKey, currentTargetCtx); err != nil {
				cr.Status.UsedReferences = nil
				cr.SetConditions(invv1alpha1.DSFailed(err.Error()))
				return ctrl.Result{}, errors.Wrap(r.Status().Update(ctx, cr), errUpdateStatus)
			}
		} else {
			if err := r.targetStore.Create(ctx, targetKey, target.Context{
				Client: selectedDSctx.DSClient,
			}); err != nil {
				cr.Status.UsedReferences = nil
				cr.SetConditions(invv1alpha1.DSFailed(err.Error()))
				return ctrl.Result{}, errors.Wrap(r.Status().Update(ctx, cr), errUpdateStatus)
			}
		}
	} else {
		// safety
		r.addTargetToDataServer(ctx, store.ToKey(currentTargetCtx.Client.GetAddress()), targetKey)
	}

	isSchemaReady, schemaMsg, err := r.isSchemaReady(ctx, cr)
	if err != nil {
		log.Error("cannot get schema ready state", "error", err)
		cr.Status.UsedReferences = nil
		cr.SetConditions(invv1alpha1.DSFailed(err.Error()))
		return ctrl.Result{}, errors.Wrap(r.Status().Update(ctx, cr), errUpdateStatus)
	}
	log.Info("schema ready state", "ready", isSchemaReady, "msg", schemaMsg)
	if !isSchemaReady {
		cr.Status.UsedReferences = nil
		cr.SetConditions(invv1alpha1.DSSchemaNotReady(schemaMsg))
		return ctrl.Result{}, errors.Wrap(r.Status().Update(ctx, cr), errUpdateStatus)
	}

	// Now that the target store is up to date and we have an assigned dataserver
	// we will create/update the datastore for the target
	// Target is ready
	changed, usedRefs, err := r.updateDataStoreTargetReady(ctx, cr)
	if err != nil {
		cr.Status.UsedReferences = nil
		cr.SetConditions(invv1alpha1.DSFailed(err.Error()))
		return ctrl.Result{}, errors.Wrap(r.Status().Update(ctx, cr), errUpdateStatus)
	}
	// robustness avoid to update status when there is no change
	// avoid retriggering reconcile
	if changed {
		cr.Status.UsedReferences = usedRefs
		cr.SetConditions(invv1alpha1.DSReady())
		return ctrl.Result{}, errors.Wrap(r.Status().Update(ctx, cr), errUpdateStatus)
	}
	cr.Status.UsedReferences = usedRefs
	cr.SetConditions(invv1alpha1.DSReady())
	return ctrl.Result{}, errors.Wrap(r.Status().Update(ctx, cr), errUpdateStatus)
	//return ctrl.Result{}, nil

}

func (r *reconciler) deleteTargetFromDataServer(ctx context.Context, dsKey store.Key, targetKey store.Key) {
	log := log.FromContext(ctx)
	dsctx, err := r.dataServerStore.Get(ctx, dsKey)
	if err != nil {
		log.Info("DeleteTarget2DataServer dataserver key not found", "dsKey", dsKey, "targetKey", targetKey, "error", err.Error())
		return
	}
	dsctx.Targets = dsctx.Targets.Delete(targetKey.String())
	if err := r.dataServerStore.Update(ctx, dsKey, dsctx); err != nil {
		log.Info("DeleteTarget2DataServer dataserver update failed", "dsKey", dsKey, "targetKey", targetKey, "error", err.Error())
	}
}

func (r *reconciler) addTargetToDataServer(ctx context.Context, dsKey store.Key, targetKey store.Key) {
	log := log.FromContext(ctx)
	dsctx, err := r.dataServerStore.Get(ctx, dsKey)
	if err != nil {
		log.Info("AddTarget2DataServer dataserver key not found", "dsKey", dsKey, "targetKey", targetKey, "error", err.Error())
		return
	}
	dsctx.Targets = dsctx.Targets.Insert(targetKey.String())
	if err := r.dataServerStore.Update(ctx, dsKey, dsctx); err != nil {
		log.Info("AddTarget2DataServer dataserver update failed", "dsKey", dsKey, "targetKey", targetKey, "error", err.Error())
	}
}

// selectDataServerContext selects a dataserver for a particalur target based on
// least amount of targets assigned to a dataserver
func (r *reconciler) selectDataServerContext(ctx context.Context) (*sdcctx.DSContext, error) {
	log := log.FromContext(ctx)
	var err error
	selectedDSctx := &sdcctx.DSContext{}
	minTargets := 9999
	r.dataServerStore.List(ctx, func(ctx context.Context, k store.Key, dsctx sdcctx.DSContext) {
		if dsctx.Targets.Len() == 0 || dsctx.Targets.Len() < minTargets {
			selectedDSctx = &dsctx
			minTargets = dsctx.Targets.Len()
		}
	})
	// create and start client if it does not exist
	if selectedDSctx.DSClient == nil {
		log.Info("selectedDSctx", "selectedDSctx", selectedDSctx)

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
	key := store.KeyFromNSN(types.NamespacedName{Namespace: cr.GetNamespace(), Name: cr.GetName()})
	log := log.FromContext(ctx).With("targetkey", key.String())
	changed := false
	req, usedRefs, err := r.getCreateDataStoreRequest(ctx, cr)
	if err != nil {
		log.Error("cannot create datastore request from CR/Profiles", "error", err)
		return changed, nil, err
	}
	// this should always succeed
	targetCtx, err := r.targetStore.Get(ctx, key)
	if err != nil {
		log.Error("cannot get datastore from store", "error", err)
		return changed, nil, err
	}
	// get the datastore from the dataserver
	getRsp, err := targetCtx.Client.GetDataStore(ctx, &sdcpb.GetDataStoreRequest{Name: key.String()})
	if err != nil {
		if !strings.Contains(err.Error(), "unknown datastore") {
			log.Error("cannot get datastore from dataserver", "error", err)
			return changed, nil, err
		}
		log.Info("datastore does not exist")
		// datastore does not exist
	} else {
		// datastore exists -> validate changes and if so delete the datastore
		if !r.hasDataStoreChanged(ctx, req, getRsp, cr, usedRefs) {
			log.Info("datastore exist -> no change")
			return changed, usedRefs, nil
		}
		changed = true
		log.Info("datastore exist -> changed")
		targetCtx.Ready = false
		targetCtx.DataStore = nil
		r.targetStore.Update(ctx, key, targetCtx)
		rsp, err := targetCtx.Client.DeleteDataStore(ctx, &sdcpb.DeleteDataStoreRequest{Name: key.String()})
		if err != nil {
			log.Error("cannot delete datstore in dataserver", "error", err)
			return changed, nil, err
		}
		log.Info("delete datastore succeeded", "resp", prototext.Format(rsp))
	}
	// datastore does not exist -> create datastore
	log.Info("create datastore", "req name", req.Name, "schema", req.GetSchema(), "target", req.GetTarget(), "sync", req.GetSync())
	changed = true
	rsp, err := targetCtx.Client.CreateDataStore(ctx, req)
	if err != nil {
		log.Error("cannot create datastore in dataserver", "error", err)
		return changed, nil, err
	}
	targetCtx.DataStore = req
	targetCtx.Ready = true
	if err := r.targetStore.Update(ctx, key, targetCtx); err != nil {
		log.Error("cannot update datastore in store", "error", err)
		return changed, nil, err
	}
	log.Info("create datastore succeeded", "resp", prototext.Format(rsp))
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
	log.Info("hasDataStoreChanged",
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
		log.Info("hasDataStoreChanged", "UsedReferences", "nil")
		return true
	}

	log.Info("hasDataStoreChanged",
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

	if cr.Status.GetCondition(invv1alpha1.ConditionTypeDSReady).Status == metav1.ConditionFalse {
		log.Info("hasDataStoreChanged", "DS Ready condition", "false")
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

	name, vendor := invv1alpha1.GetVendorType(cr.Status.DiscoveryInfo.Provider)

	return &sdcpb.CreateDataStoreRequest{
		Name: store.KeyFromNSN(types.NamespacedName{Namespace: cr.Namespace, Name: cr.Name}).String(),
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
		},
		Sync: invv1alpha1.GetSyncProfile(syncProfile),
		Schema: &sdcpb.Schema{
			//Name: cr.Status.DiscoveryInfo.Provider,
			Name:    name,
			Vendor:  vendor,
			Version: cr.Status.DiscoveryInfo.Version,
		},
	}, usedReferences, nil
}

func (r *reconciler) getLease(ctx context.Context, targetKey store.Key) lease.Lease {
	tctx, err := r.targetStore.Get(ctx, targetKey)
	if err != nil {
		lease := lease.New(r.Client, targetKey.NamespacedName)
		r.targetStore.Create(ctx, targetKey, target.Context{Lease: lease})
		return lease
	}
	if tctx.Lease == nil {
		lease := lease.New(r.Client, targetKey.NamespacedName)
		tctx.Lease = lease
		r.targetStore.Update(ctx, targetKey, target.Context{Lease: lease})
		return lease
	}
	return tctx.Lease
}
