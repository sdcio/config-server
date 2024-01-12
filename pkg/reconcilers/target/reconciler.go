package target

import (
	"context"
	"fmt"
	"reflect"
	"strings"

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
	"sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/source"

	invv1alpha1 "github.com/iptecharch/config-server/apis/inv/v1alpha1"
	"github.com/iptecharch/config-server/pkg/reconcilers"
	"github.com/iptecharch/config-server/pkg/reconcilers/ctrlconfig"
	"github.com/iptecharch/config-server/pkg/reconcilers/resource"
	sdcctx "github.com/iptecharch/config-server/pkg/sdc/ctx"
	dsclient "github.com/iptecharch/config-server/pkg/sdc/dataserver/client"
	"github.com/iptecharch/config-server/pkg/store"
	"github.com/iptecharch/config-server/pkg/target"
)

func init() {
	reconcilers.Register("target", &reconciler{})
}

const (
	finalizer = "target.inv.sdcio.dev/finalizer"
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

	if err := invv1alpha1.AddToScheme(mgr.GetScheme()); err != nil {
		return nil, err
	}

	r.Client = mgr.GetClient()
	r.finalizer = resource.NewAPIFinalizer(mgr.GetClient(), finalizer)
	//r.configStore = cfg.ConfigStore
	r.targetStore = cfg.TargetStore
	r.dataServerStore = cfg.DataServerStore

	return nil, ctrl.NewControllerManagedBy(mgr).
		Named("TargetDataStoreController").
		For(&invv1alpha1.Target{}).
		Watches(&source.Kind{Type: &invv1alpha1.TargetConnectionProfile{}}, &targetConnProfileEventHandler{client: mgr.GetClient()}).
		Watches(&source.Kind{Type: &invv1alpha1.TargetSyncProfile{}}, &targetSyncProfileEventHandler{client: mgr.GetClient()}).
		Complete(r)
}

type reconciler struct {
	client.Client
	finalizer *resource.APIFinalizer

	//configStore     store.Storer[runtime.Object]
	targetStore     store.Storer[target.Context]
	dataServerStore store.Storer[sdcctx.DSContext]
}

func (r *reconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	log := log.FromContext(ctx).WithValues("req", req)
	log.Info("reconcile")

	key := store.KeyFromNSN(req.NamespacedName)

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
		// check if this is the last one -> if so stop the client to the dataserver
		targetCtx, err := r.targetStore.Get(ctx, key)
		if err != nil {
			// client does not exist
			if err := r.finalizer.RemoveFinalizer(ctx, cr); err != nil {
				log.Error(err, "cannot remove finalizer")
				cr.Status.UsedReferences = nil
				cr.SetConditions(invv1alpha1.DSFailed(err.Error()))
				return ctrl.Result{Requeue: true}, errors.Wrap(r.Status().Update(ctx, cr), errUpdateStatus)
			}
			log.Info("Successfully deleted resource, with non existing client -> strange")
			return ctrl.Result{}, nil
		}
		// delete the mapping in the dataserver cache, which keeps track of all targets per dataserver
		r.deleteTargetFromDataServer(ctx, store.ToKey(targetCtx.Client.GetAddress()), key)
		// delete the datastore
		if targetCtx.DataStore != nil {
			log.Info("deleting datastore", "key", key.String())
			rsp, err := targetCtx.Client.DeleteDataStore(ctx, &sdcpb.DeleteDataStoreRequest{Name: key.String()})
			if err != nil {
				log.Error(err, "cannot delete datastore")
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
			log.Error(err, "cannot remove finalizer")
			cr.Status.UsedReferences = nil
			cr.SetConditions(invv1alpha1.DSFailed(err.Error()))
			return ctrl.Result{Requeue: true}, errors.Wrap(r.Status().Update(ctx, cr), errUpdateStatus)
		}

		log.Info("Successfully deleted resource")
		return ctrl.Result{}, nil
	}

	// We dont act as long the target is not ready (rady state is handled by the discovery controller)
	// Ready -> NotReady: happens only when the discovery fails => we keep the target as is do not delete the datatore/etc
	log.Info("target ready condition", "status", cr.Status.GetCondition(invv1alpha1.ConditionTypeReady).Status)
	if cr.Status.GetCondition(invv1alpha1.ConditionTypeReady).Status == metav1.ConditionTrue {
		if err := r.finalizer.AddFinalizer(ctx, cr); err != nil {
			log.Error(err, "cannot add finalizer")
			cr.Status.UsedReferences = nil
			cr.SetConditions(invv1alpha1.DSFailed(err.Error()))
			return ctrl.Result{Requeue: true}, errors.Wrap(r.Status().Update(ctx, cr), errUpdateStatus)
		}

		// first check if the target has an assigned dataserver, if not allocate one
		// update the target store with the updated information
		currentTargetCtx, err := r.targetStore.Get(ctx, key)
		if err != nil {
			selectedDSctx, err := r.selectDataServerContext(ctx)
			if err != nil {
				log.Error(err, "cannot select a dataserver")
				cr.Status.UsedReferences = nil
				cr.SetConditions(invv1alpha1.DSFailed(err.Error()))
				return ctrl.Result{}, errors.Wrap(r.Status().Update(ctx, cr), errUpdateStatus)
			}
			// add the target to the DS
			r.addTargetToDataServer(ctx, store.ToKey(selectedDSctx.DSClient.GetAddress()), key)
			// create the target in the target store
			r.targetStore.Create(ctx, key, target.Context{
				Client: selectedDSctx.DSClient,
			})
		} else {
			// safety
			r.addTargetToDataServer(ctx, store.ToKey(currentTargetCtx.Client.GetAddress()), key)
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
	// target not ready so we can wait till the target goes to ready state
	if cr.GetCondition(invv1alpha1.ConditionTypeDSReady).Status == metav1.ConditionTrue {
		cr.Status.UsedReferences = nil
		cr.SetConditions(invv1alpha1.DSFailed("target not ready"))
		return ctrl.Result{}, errors.Wrap(r.Status().Update(ctx, cr), errUpdateStatus)
	}
	// if the Ready state is false and we dont have a true status in DSReady state
	// we dont update the target -> otherwise we get to many reconcile events and updates fail
	return ctrl.Result{}, nil
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
			log.Error(err, "cannot create dataserver client")
			return nil, err
		}
		if err := selectedDSctx.DSClient.Start(ctx); err != nil {
			log.Error(err, "cannot start dataserver client")
			return nil, err
		}
	}
	return selectedDSctx, nil
}

func (r *reconciler) updateDataStoreTargetNotReady(ctx context.Context, cr *invv1alpha1.Target) error {
	key := store.KeyFromNSN(types.NamespacedName{Namespace: cr.GetNamespace(), Name: cr.GetName()})
	log := log.FromContext(ctx).WithValues("targetkey", key.String())

	// this should always succeed
	targetCtx, err := r.targetStore.Get(ctx, key)
	if err != nil {
		log.Error(err, "cannot get datastore from store")
		return err
	}
	// get the datastore from the dataserver
	if _, err := targetCtx.Client.GetDataStore(ctx, &sdcpb.GetDataStoreRequest{Name: key.String()}); err != nil {
		if !strings.Contains(err.Error(), "unknown datastore") {
			log.Error(err, "cannot get datastore from dataserver")
			return err
		}
		log.Info("datastore does not exist")
		return nil
	} else {
		// datastore exists - > delete the datastore
		rsp, err := targetCtx.Client.DeleteDataStore(ctx, &sdcpb.DeleteDataStoreRequest{Name: key.String()})
		if err != nil {
			log.Error(err, "cannot delete datstore in dataserver")
			return err
		}
		log.Info("delete datastore succeeded", "resp", prototext.Format(rsp))
	}
	return nil
}

// updateDataStore will do 1 of 3 things
// 1. create a datastore if none exists
// 2. delete/update the datastore if changes were detected
// 3. do nothing if no changes were detected.
func (r *reconciler) updateDataStoreTargetReady(ctx context.Context, cr *invv1alpha1.Target) (bool, *invv1alpha1.TargetStatusUsedReferences, error) {
	key := store.KeyFromNSN(types.NamespacedName{Namespace: cr.GetNamespace(), Name: cr.GetName()})
	log := log.FromContext(ctx).WithValues("targetkey", key.String())
	changed := false
	req, usedRefs, err := r.getCreateDataStoreRequest(ctx, cr)
	if err != nil {
		log.Error(err, "cannot create datastore request from CR/Profiles")
		return changed, nil, err
	}
	// this should always succeed
	targetCtx, err := r.targetStore.Get(ctx, key)
	if err != nil {
		log.Error(err, "cannot get datastore from store")
		return changed, nil, err
	}
	// get the datastore from the dataserver
	getRsp, err := targetCtx.Client.GetDataStore(ctx, &sdcpb.GetDataStoreRequest{Name: key.String()})
	if err != nil {
		if !strings.Contains(err.Error(), "unknown datastore") {
			log.Error(err, "cannot get datastore from dataserver")
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
		rsp, err := targetCtx.Client.DeleteDataStore(ctx, &sdcpb.DeleteDataStoreRequest{Name: key.String()})
		if err != nil {
			log.Error(err, "cannot delete datstore in dataserver")
			return changed, nil, err
		}
		log.Info("delete datastore succeeded", "resp", prototext.Format(rsp))
	}
	// datastore does not exist -> create datastore
	log.Info("create datastore", "req name", req.Name, "schema", req.GetSchema(), "target", req.GetTarget(), "sync", req.GetSync())
	changed = true
	rsp, err := targetCtx.Client.CreateDataStore(ctx, req)
	if err != nil {
		log.Error(err, "cannot create datastore in dataserver")
		return changed, nil, err
	}
	targetCtx.DataStore = req
	if err := r.targetStore.Update(ctx, key, targetCtx); err != nil {
		log.Error(err, "cannot update datastore in store")
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
