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
	"time"

	"github.com/henderiw/apiserver-store/pkg/storebackend"
	"github.com/henderiw/logger/log"
	"github.com/pkg/errors"
	condv1alpha1 "github.com/sdcio/config-server/apis/condition/v1alpha1"
	invv1alpha1 "github.com/sdcio/config-server/apis/inv/v1alpha1"
	invv1alpha1apply "github.com/sdcio/config-server/pkg/generated/applyconfiguration/inv/v1alpha1"
	"github.com/sdcio/config-server/pkg/reconcilers"
	"github.com/sdcio/config-server/pkg/reconcilers/ctrlconfig"
	"github.com/sdcio/config-server/pkg/reconcilers/eventhandler"
	"github.com/sdcio/config-server/pkg/reconcilers/resource"
	targetmanager "github.com/sdcio/config-server/pkg/sdc/target/manager"
	sdcpb "github.com/sdcio/sdc-protos/sdcpb"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/equality"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/tools/events"
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

	if cfg.TargetManager == nil {
		return nil, fmt.Errorf("TargetManager is nil: set LOCAL_DATASERVER=true or disable TargetDataStoreController")
	}

	r.client = mgr.GetClient()
	r.finalizer = resource.NewAPIFinalizer(mgr.GetClient(), finalizer, reconcilerName)
	r.targetMgr = cfg.TargetManager
	r.recorder = mgr.GetEventRecorder(reconcilerName)

	return nil, ctrl.NewControllerManagedBy(mgr).
		Named(reconcilerName).
		For(&invv1alpha1.Target{}).
		Watches(&invv1alpha1.TargetConnectionProfile{}, &eventhandler.TargetConnProfileForTargetEventHandler{Client: mgr.GetClient()}).
		Watches(&invv1alpha1.TargetSyncProfile{}, &eventhandler.TargetSyncProfileForTargetEventHandler{Client: mgr.GetClient()}).
		Watches(&corev1.Secret{}, &eventhandler.SecretForTargetEventHandler{Client: mgr.GetClient()}).
		Watches(&invv1alpha1.Schema{}, &eventhandler.SchemaForTargetEventHandler{Client: mgr.GetClient()}).
		Complete(r)
}

type reconciler struct {
	client          client.Client
	finalizer       *resource.APIFinalizer
	targetMgr       *targetmanager.TargetManager
	recorder        events.EventRecorder
}

func (r *reconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	ctx = ctrlconfig.InitContext(ctx, reconcilerName, req.NamespacedName)
	log := log.FromContext(ctx)
	log.Info("reconcile")

	targetKey := storebackend.KeyFromNSN(req.NamespacedName)

	target := &invv1alpha1.Target{}
	if err := r.client.Get(ctx, req.NamespacedName, target); err != nil {
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
			// this should be the last finalizer to be removed as this deletes the target from the targetStore
			log.Debug("requeue delete, not all finalizers removed", "finalizers", target.GetFinalizers())
			return ctrl.Result{Requeue: true},
				errors.Wrap(r.handleError(ctx, targetOrig, "deleting target, not all finalizers removed", nil, true), errUpdateStatus)
		}
		log.Debug("deleting targetstore...")
		// check if this is the last one -> if so stop the client to the dataserver
		
		if r.targetMgr != nil {
			r.targetMgr.ClearDesired(ctx, targetKey)
			r.targetMgr.Delete(ctx, targetKey)
		}

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

	// We dont act as long the target is not discoveredReady (ready state is handled by the discovery controller)
	// Ready -> NotReady: happens only when the discovery fails => we keep the target as is do not delete the datatore/etc
	log.Info("target discovery ready condition", "status", target.Status.GetCondition(invv1alpha1.ConditionTypeDiscoveryReady).Status)
	if target.Status.GetCondition(invv1alpha1.ConditionTypeDiscoveryReady).Status != metav1.ConditionTrue {
		if r.targetMgr != nil {
			r.targetMgr.ClearDesired(ctx, targetKey)
		}
		// target not ready so we can wait till the target goes to ready state
		return ctrl.Result{},
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

	// Now that the target is up and the schema s available we can create the datastore
	dsReq, usedRefs, err := r.getCreateDataStoreRequest(ctx, target)
	if err != nil {
		msg := fmt.Sprintf("cannot create datastore request from CR/Profiles: %s", err)
		_ = r.handleError(ctx, targetOrig, msg, nil, false)
		return ctrl.Result{}, nil
	}

	// Compute desired hash (must be stable)
	hash := targetmanager.ComputeCreateDSHash(dsReq, usedRefs) 

	// Drive runtime
	r.targetMgr.ApplyDesired(ctx, targetKey, dsReq, usedRefs, hash)
	
	// Read runtime status
	rt := r.targetMgr.GetOrCreate(targetKey)
	st := rt.Status()

	switch st.Phase {
		case targetmanager.PhaseRunning:
			return ctrl.Result{}, errors.Wrap(r.handleSuccess(ctx, targetOrig, usedRefs), errUpdateStatus)

		case targetmanager.PhaseWaitingForDS, targetmanager.PhaseEnsuringDatastore, targetmanager.PhasePending:
			// not ready yet; update status as failed/degraded but requeue soon
			msg := fmt.Sprintf("runtime phase=%s dsReady=%t dsStoreReady=%t err=%s",
				st.Phase, st.DSReady, st.DSStoreReady, st.LastError)

			_ = r.handleError(ctx, targetOrig, msg, nil, false)
			return ctrl.Result{RequeueAfter: 5 * time.Second}, nil

		case targetmanager.PhaseDegraded:
			msg := fmt.Sprintf("runtime degraded: %s", st.LastError)
			_ = r.handleError(ctx, targetOrig, msg, nil, false)
			return ctrl.Result{RequeueAfter: 5 * time.Second}, nil

		default:
			_ = r.handleError(ctx, targetOrig, "runtime unknown phase", nil, false)
			return ctrl.Result{RequeueAfter: 5 * time.Second}, nil
		}
}


func (r *reconciler) handleSuccess(ctx context.Context, target *invv1alpha1.Target, usedRefs *invv1alpha1.TargetStatusUsedReferences) error {
	log := log.FromContext(ctx)
	//log.Info("handleSuccess", "key", target.GetNamespacedName(), "status old", target.DeepCopy().Status)
	// take a snapshot of the current object
	newCond := invv1alpha1.DatastoreReady()
	oldCond := target.GetCondition(invv1alpha1.ConditionTypeDatastoreReady)

	// we don't update the resource if no condition changed
	if newCond.Equal(oldCond) &&
		equality.Semantic.DeepEqual(usedRefs, target.Status.UsedReferences) {
		log.Info("handleSuccess -> no change")
		return nil
	}
	log.Info("handleSuccess",
		"condition change", !newCond.Equal(oldCond),
		"usedRefs change", !equality.Semantic.DeepEqual(usedRefs, target.Status.UsedReferences),
	)

	r.recorder.Eventf(target, nil, corev1.EventTypeNormal, invv1alpha1.TargetKind, "datastore ready", "")

	applyConfig := invv1alpha1apply.Target(target.Name, target.Namespace).
		WithStatus(invv1alpha1apply.TargetStatus().
			WithConditions(newCond).
			WithUsedReferences(usedRefsToApply(usedRefs)),
		)

	return r.client.Status().Apply(ctx, applyConfig, &client.SubResourceApplyOptions{
		ApplyOptions: client.ApplyOptions{
			FieldManager: reconcilerName,
		},
	})
}

func (r *reconciler) handleError(ctx context.Context, target *invv1alpha1.Target, msg string, err error, resetUsedRefs bool) error {
	log := log.FromContext(ctx)

	if err != nil {
		msg = fmt.Sprintf("%s err %s", msg, err.Error())
	}

	newCond := invv1alpha1.DatastoreFailed(msg)
	oldCond := target.GetCondition(invv1alpha1.ConditionTypeDatastoreReady)

	var newUsedRefs *invv1alpha1.TargetStatusUsedReferences
	if !resetUsedRefs {
		newUsedRefs = target.Status.UsedReferences
	}

	if newCond.Equal(oldCond) &&
		equality.Semantic.DeepEqual(newUsedRefs, target.Status.UsedReferences) {
		log.Info("handleError -> no change")
		return nil
	}
	
	log.Info("handleError",
		"condition change", !newCond.Equal(oldCond),
		"usedRefs change", !equality.Semantic.DeepEqual(newUsedRefs, target.Status.UsedReferences),
	)
	log.Error(msg, "error", err)
	r.recorder.Eventf(target, nil, corev1.EventTypeWarning, invv1alpha1.TargetKind, msg, "")

	applyConfig := invv1alpha1apply.Target(target.Name, target.Namespace).
		WithStatus(invv1alpha1apply.TargetStatus().
			WithConditions(newCond).
			WithUsedReferences(usedRefsToApply(newUsedRefs)),
		)

	return r.client.Status().Apply(ctx, applyConfig, &client.SubResourceApplyOptions{
		ApplyOptions: client.ApplyOptions{
			FieldManager: reconcilerName,
		},
	})
}

func (r *reconciler) getConnProfile(ctx context.Context, key types.NamespacedName) (*invv1alpha1.TargetConnectionProfile, error) {
	profile := &invv1alpha1.TargetConnectionProfile{}
	if err := r.client.Get(ctx, key, profile); err != nil {
		if resource.IgnoreNotFound(err) != nil {
			return nil, err
		}
		return invv1alpha1.DefaultTargetConnectionProfile(), nil
	}
	return profile, nil
}

func (r *reconciler) getSyncProfile(ctx context.Context, key types.NamespacedName) (*invv1alpha1.TargetSyncProfile, error) {
	profile := &invv1alpha1.TargetSyncProfile{}
	if err := r.client.Get(ctx, key, profile); err != nil {
		if resource.IgnoreNotFound(err) != nil {
			return nil, err
		}
		return invv1alpha1.DefaultTargetSyncProfile(), nil
	}
	return profile, nil
}

func (r *reconciler) getSecret(ctx context.Context, key types.NamespacedName) (*corev1.Secret, error) {
	secret := &corev1.Secret{}
	if err := r.client.Get(ctx, key, secret); err != nil {
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

	if err := r.client.List(ctx, schemaList, opts...); err != nil {
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
	if !connProfile.IsInsecure() {
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
		DatastoreName: storebackend.KeyFromNSN(types.NamespacedName{Namespace: target.Namespace, Name: target.Name}).String(),
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
		targetName:= ""
		if connProfile.Spec.TargetName != nil {
			targetName = *connProfile.Spec.TargetName
		}
		req.Target.ProtocolOptions = &sdcpb.Target_GnmiOpts{
			GnmiOpts: &sdcpb.GnmiOptions{
				Encoding: string(connProfile.Encoding()),
				TargetName: targetName,
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


func usedRefsToApply(refs *invv1alpha1.TargetStatusUsedReferences) *invv1alpha1apply.TargetStatusUsedReferencesApplyConfiguration {
	if refs == nil {
		return nil
	}
	a := invv1alpha1apply.TargetStatusUsedReferences()
	if refs.SecretResourceVersion != "" {
		a.WithSecretResourceVersion(refs.SecretResourceVersion)
	}
	if refs.TLSSecretResourceVersion != "" {
		a.WithTLSSecretResourceVersion(refs.TLSSecretResourceVersion)
	}
	if refs.ConnectionProfileResourceVersion != "" {
		a.WithConnectionProfileResourceVersion(refs.ConnectionProfileResourceVersion)
	}
	if refs.SyncProfileResourceVersion != "" {
		a.WithSyncProfileResourceVersion(refs.SyncProfileResourceVersion)
	}
	return a
}