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
	"slices"
	"time"

	"github.com/henderiw/apiserver-store/pkg/storebackend"
	"github.com/henderiw/logger/log"
	"github.com/pkg/errors"
	condv1alpha1 "github.com/sdcio/config-server/apis/condition/v1alpha1"
	configv1alpha1 "github.com/sdcio/config-server/apis/config/v1alpha1"
	invv1alpha1 "github.com/sdcio/config-server/apis/inv/v1alpha1"
	configv1alpha1apply "github.com/sdcio/config-server/pkg/generated/applyconfiguration/config/v1alpha1"
	"github.com/sdcio/config-server/pkg/reconcilers"
	"github.com/sdcio/config-server/pkg/reconcilers/ctrlconfig"
	"github.com/sdcio/config-server/pkg/reconcilers/eventhandler"
	"github.com/sdcio/config-server/pkg/reconcilers/resource"
	targetmanager "github.com/sdcio/config-server/pkg/sdc/target/manager"
	sdcpb "github.com/sdcio/sdc-protos/sdcpb"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/tools/events"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/event"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	"sigs.k8s.io/controller-runtime/pkg/source"
)

func init() {
	reconcilers.Register(crName, &reconciler{})
}

const (
	crName                = "targetdatastore"
	fieldmanagerfinalizer = "TargetDataStoreController-finalizer"
	reconcilerName        = "TargetDataStoreController"
	finalizer             = "targetdatastore.inv.sdcio.dev/finalizer"
	// errors
	errGetCr        = "cannot get cr"
	errUpdateStatus = "cannot update status"

	// The runtime notifies every status change (Events channel); these
	// requeues only cover a dropped notification or slow external state.
	fallbackRequeue = 30 * time.Second
	schemaRequeue   = 10 * time.Second
	deleteRequeue   = 5 * time.Second
)

//+kubebuilder:rbac:groups=inv.sdcio.dev,resources=targets,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups=inv.sdcio.dev,resources=targets/status,verbs=get;update;patch

// SetupWithManager sets up the controller with the Manager.
//
// This controller owns the target instantiation and is the single writer of
// the Target Ready condition. The discovery controller owns TargetDiscoveryReady.
func (r *reconciler) SetupWithManager(ctx context.Context, mgr ctrl.Manager, c interface{}) (map[schema.GroupVersionKind]chan event.GenericEvent, error) {
	cfg, ok := c.(*ctrlconfig.ControllerConfig)
	if !ok {
		return nil, fmt.Errorf("cannot initialize, expecting controllerConfig, got: %s", reflect.TypeOf(c).Name())
	}

	if cfg.TargetManager == nil {
		return nil, fmt.Errorf("TargetManager is nil: set LOCAL_DATASERVER=true or disable TargetDataStoreController")
	}

	r.client = mgr.GetClient()
	r.finalizer = resource.NewAPIFinalizer(
		mgr.GetClient(),
		finalizer,
		fieldmanagerfinalizer,
		func(name, namespace string, finalizers ...string) runtime.ApplyConfiguration {
			ac := configv1alpha1apply.Target(name, namespace)
			if len(finalizers) > 0 {
				ac.WithFinalizers(finalizers...)
			}
			return ac
		},
	)
	r.targetMgr = cfg.TargetManager
	r.recorder = mgr.GetEventRecorder(reconcilerName)
	r.ready = resource.NewStatusWriter(mgr.GetClient(), reconcilerName,
		resource.WithEventRecorder(r.recorder),
		// takes over Ready from the retired TargetController field manager
		resource.WithForceOwnership(),
	)

	return nil, ctrl.NewControllerManagedBy(mgr).
		Named(reconcilerName).
		For(&configv1alpha1.Target{}).
		Watches(&invv1alpha1.TargetConnectionProfile{}, &eventhandler.TargetConnProfileForTargetEventHandler{Client: mgr.GetClient()}).
		Watches(&invv1alpha1.TargetSyncProfile{}, &eventhandler.TargetSyncProfileForTargetEventHandler{Client: mgr.GetClient()}).
		Watches(&corev1.Secret{}, &eventhandler.SecretForTargetEventHandler{Client: mgr.GetClient()}).
		Watches(&invv1alpha1.Schema{}, &eventhandler.SchemaForTargetEventHandler{Client: mgr.GetClient()}).
		// runtime status changes (datastore, recovery, ...) of each target
		WatchesRawSource(source.Channel(r.targetMgr.Events(), &handler.EnqueueRequestForObject{})).
		Complete(r)
}

type reconciler struct {
	client    client.Client
	finalizer *resource.APIFinalizer
	targetMgr *targetmanager.TargetManager
	recorder  events.EventRecorder
	ready     *resource.StatusWriter // the only writer of Target Ready and usedReferences
}

func (r *reconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	ctx = ctrlconfig.InitContext(ctx, reconcilerName, req.NamespacedName)
	log := log.FromContext(ctx)
	log.Info("reconcile")

	targetKey := storebackend.KeyFromNSN(req.NamespacedName)

	target := &configv1alpha1.Target{}
	if err := r.client.Get(ctx, req.NamespacedName, target); err != nil {
		// if the resource no longer exists the reconcile loop is done
		if resource.IgnoreNotFound(err) != nil {
			log.Error(errGetCr, "error", err)
			return ctrl.Result{}, errors.Wrap(resource.IgnoreNotFound(err), errGetCr)
		}
		return ctrl.Result{}, nil
	}

	// one-time migration: drop the conditions folded into Ready.
	// The patch triggers a new reconcile on the cleaned object.
	if dropped, err := r.dropLegacyConditions(ctx, target); err != nil || dropped {
		return ctrl.Result{}, err
	}

	if !target.GetDeletionTimestamp().IsZero() {
		return r.reconcileDelete(ctx, target, targetKey)
	}

	if err := r.finalizer.AddFinalizer(ctx, target); err != nil {
		return ctrl.Result{}, errors.Wrap(err, "cannot add finalizer")
	}

	// --- pipeline: the first stage that blocks sets the Ready reason ---

	// 1) discovery
	// We dont act as long the target is not discoveredReady (ready state is handled by the discovery controller)
	discovery := target.GetCondition(configv1alpha1.ConditionTypeTargetDiscoveryReady)
	if !discovery.IsTrue() {
		r.targetMgr.ClearDesired(ctx, targetKey)
		return ctrl.Result{}, r.applyReady(ctx, target,
			configv1alpha1.TargetNotReady(configv1alpha1.TargetReasonDiscoveryNotReady, discovery.Message),
			nil)
	}

	// 2) schema
	schemaReady, schemaMsg, err := r.isSchemaReady(ctx, target)
	if err != nil {
		// the apiserver had issues retrieving the schema: report and back off
		_ = r.applyReady(ctx, target,
			configv1alpha1.TargetNotReady(configv1alpha1.TargetReasonSchemaNotReady, "cannot get schema ready state: "+err.Error()),
			nil)
		return ctrl.Result{}, errors.Wrap(err, "cannot get schema ready state")
	}
	if !schemaReady {
		return ctrl.Result{RequeueAfter: schemaRequeue}, r.applyReady(ctx, target,
			configv1alpha1.TargetNotReady(configv1alpha1.TargetReasonSchemaNotReady, schemaMsg),
			nil)
	}

	// 3) configuration (profiles, secrets): watched, so no requeue needed
	dsReq, usedRefs, err := r.getCreateDataStoreRequest(ctx, target)
	if err != nil {
		return ctrl.Result{}, r.applyReady(ctx, target,
			configv1alpha1.TargetNotReady(configv1alpha1.TargetReasonInvalidConfiguration, err.Error()),
			target.Status.UsedReferences)
	}

	// 4) runtime: dataserver -> datastore -> recovery -> running
	hash := targetmanager.ComputeCreateDSHash(dsReq, usedRefs)
	// token BEFORE desired: the first recovery attempt must already see it
	r.targetMgr.SetRecoveryRetryToken(ctx, targetKey,
		target.GetAnnotations()[configv1alpha1.AnnotationRecoveryRetry])
	r.targetMgr.ApplyDesired(ctx, targetKey, dsReq, usedRefs, hash)

	// This is the runtime status *before* it processed the new desired state;
	// the runtime notifies us when it moves on.
	st := r.targetMgr.GetOrCreate(targetKey).Status()
	cond, requeue, datastoreUp := readyFromRuntime(st)

	refs := target.Status.UsedReferences
	if datastoreUp {
		refs = usedRefs
	}
	return ctrl.Result{RequeueAfter: requeue}, r.applyReady(ctx, target, cond, refs)
}

func (r *reconciler) reconcileDelete(ctx context.Context, target *configv1alpha1.Target, targetKey storebackend.Key) (ctrl.Result, error) {
	log := log.FromContext(ctx)

	if len(target.GetFinalizers()) > 1 {
		// we must be the last finalizer: the runtime and datastore have to
		// outlive the other finalizers (e.g. config cleanup)
		log.Debug("requeue delete, not all finalizers removed", "finalizers", target.GetFinalizers())
		return ctrl.Result{RequeueAfter: deleteRequeue}, r.applyReady(ctx, target,
			configv1alpha1.TargetNotReady(configv1alpha1.TargetReasonDeleting, "waiting for other finalizers"),
			target.Status.UsedReferences)
	}

	// Finalization is synchronous and error-returning; any failure keeps the
	// finalizer and retries (sdcio/config-server#516):
	//
	// 1) stop the runtime and wait until its run loop has exited, so nothing
	//    can (re)create the datastore or rewrite the Deviation afterwards
	if err := r.targetMgr.Delete(ctx, targetKey); err != nil {
		return r.retryDelete(ctx, target, "cannot stop target runtime", err)
	}
	// 2) delete the datastore; NotFound counts as deleted. Works without an
	//    in-memory runtime (e.g. after a config-server restart).
	if err := r.targetMgr.DeleteDatastore(ctx, targetKey); err != nil {
		return r.retryDelete(ctx, target, "cannot delete datastore", err)
	}

	// 3) Deviation
	if err := r.deleteDeviation(ctx, target); err != nil {
		return r.retryDelete(ctx, target, "cannot delete deviation", err)
	}

	if err := r.finalizer.RemoveFinalizer(ctx, target); err != nil {
		return ctrl.Result{}, errors.Wrap(err, "cannot remove finalizer")
	}

	log.Debug("Successfully deleted resource")
	return ctrl.Result{}, nil
}

// retryDelete keeps the finalizer, surfaces the failure on Ready and retries
// at a fixed interval (not exponential backoff: the dataserver may be down
// for a while and deletion should resume promptly when it is back).
func (r *reconciler) retryDelete(ctx context.Context, target *configv1alpha1.Target, msg string, err error) (ctrl.Result, error) {
	log.FromContext(ctx).Warn("target finalization blocked, retrying", "msg", msg, "err", err, "retryIn", deleteRequeue)
	return ctrl.Result{RequeueAfter: deleteRequeue}, r.applyReady(ctx, target,
		configv1alpha1.TargetNotReady(configv1alpha1.TargetReasonDeleting, msg+": "+err.Error()),
		target.Status.UsedReferences)
}

// readyFromRuntime maps the runtime phase onto the Ready condition.
// datastoreUp reports whether the datastore exists with the desired refs.
func readyFromRuntime(st targetmanager.RuntimeStatus) (cond condv1alpha1.Condition, requeue time.Duration, datastoreUp bool) {
	withErr := func(msg string) string {
		if st.LastError == "" {
			return msg
		}
		return msg + ": " + st.LastError
	}

	switch st.Phase {
	case targetmanager.PhaseRunning:
		return configv1alpha1.TargetReady(st.RecoveryMsg), 0, true

	case targetmanager.PhaseRecovering:
		return configv1alpha1.TargetNotReady(configv1alpha1.TargetReasonRecovering,
			withErr("recovering configs")), fallbackRequeue, true

	case targetmanager.PhaseRecoveryFailed:
		// terminal: only a human (annotation / target change) moves this on
		return configv1alpha1.TargetNotReady(configv1alpha1.TargetReasonRecoveryFailed,
			fmt.Sprintf("config recovery failed, manual intervention required: %s; fix the configs, then set annotation %s to a new value to retry",
				st.LastError, configv1alpha1.AnnotationRecoveryRetry)), 0, true

	case targetmanager.PhaseDegraded:
		return configv1alpha1.TargetNotReady(configv1alpha1.TargetReasonDatastoreFailed,
			withErr("datastore failed")), fallbackRequeue, false

	case targetmanager.PhaseEnsuringDatastore:
		return configv1alpha1.TargetNotReady(configv1alpha1.TargetReasonDatastoreNotReady,
			withErr("ensuring datastore")), fallbackRequeue, false

	case targetmanager.PhaseWaitingForDS:
		return configv1alpha1.TargetNotReady(configv1alpha1.TargetReasonDataServerNotReady,
			withErr("waiting for dataserver")), fallbackRequeue, false

	case targetmanager.PhasePending:
		return configv1alpha1.TargetNotReady(configv1alpha1.TargetReasonInitializing,
			"initializing target runtime"), fallbackRequeue, false

	case targetmanager.PhaseDeleting:
		return configv1alpha1.TargetNotReady(configv1alpha1.TargetReasonDeleting,
			"target runtime stopping"), fallbackRequeue, false

	default:
		return configv1alpha1.TargetNotReady(condv1alpha1.ConditionReasonUnknown,
			fmt.Sprintf("unknown runtime phase %q", st.Phase)), fallbackRequeue, false
	}
}

// applyReady writes the Ready condition and usedReferences (r.ready is the
// only writer). usedRefs == nil clears them.
//
// TargetReady/TargetNotReady bound the message deterministically, so a
// repeated long error is the same status and the apiserver drops the write.
// An event is recorded when status or reason changes, not on message churn.
func (r *reconciler) applyReady(ctx context.Context, target *configv1alpha1.Target, newCond condv1alpha1.Condition, usedRefs *configv1alpha1.TargetStatusUsedReferences) error {
	log.FromContext(ctx).Debug("applyReady", "ready", newCond.IsTrue(), "reason", newCond.Reason, "msg", newCond.Message)
	return errors.Wrap(r.ready.Write(ctx, target, resource.StatusUpdate{
		Conditions: []condv1alpha1.Condition{newCond},
		Build: func(conds []condv1alpha1.Condition, rv string) runtime.ApplyConfiguration {
			ac := configv1alpha1apply.Target(target.Name, target.Namespace).
				WithStatus(configv1alpha1apply.TargetStatus().
					WithConditions(conds...).
					WithUsedReferences(usedRefsToApply(usedRefs)),
				)
			if rv != "" {
				ac.WithResourceVersion(rv)
			}
			return ac
		},
	}), errUpdateStatus)
}

// dropLegacyConditions removes condition types that were folded into Ready.
// SSA cannot remove entries owned by other (retired) field managers, so this
// uses a plain status patch. Returns true if it patched.
func (r *reconciler) dropLegacyConditions(ctx context.Context, target *configv1alpha1.Target) (bool, error) {
	kept := make([]condv1alpha1.Condition, 0, len(target.Status.Conditions))
	for _, c := range target.Status.Conditions {
		if slices.Contains(configv1alpha1.LegacyTargetConditionTypes, condv1alpha1.ConditionType(c.Type)) {
			continue
		}
		kept = append(kept, c)
	}
	if len(kept) == len(target.Status.Conditions) {
		return false, nil
	}

	log.FromContext(ctx).Info("removing legacy target conditions",
		"removed", len(target.Status.Conditions)-len(kept))

	orig := target.DeepCopy()
	target.Status.Conditions = kept
	if err := r.client.Status().Patch(ctx, target,
		client.MergeFromWithOptions(orig, client.MergeFromWithOptimisticLock{})); err != nil {
		return false, errors.Wrap(err, "cannot remove legacy conditions")
	}
	return true, nil
}

func (r *reconciler) deleteDeviation(ctx context.Context, target *configv1alpha1.Target) error {
	deviation := &configv1alpha1.Deviation{}
	deviation.Name = target.Name
	deviation.Namespace = target.Namespace
	if err := r.client.Delete(ctx, deviation); err != nil {
		if resource.IgnoreNotFound(err) != nil {
			return errors.Wrap(err, "cannot delete deviation")
		}
	}
	return nil
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

func (r *reconciler) isSchemaReady(ctx context.Context, target *configv1alpha1.Target) (bool, string, error) {
	if target.Status.DiscoveryInfo == nil {
		return false, "target has no discovery info", nil
	}

	schemaList := &invv1alpha1.SchemaList{}
	opts := []client.ListOption{
		client.InNamespace(target.Namespace),
	}
	if err := r.client.List(ctx, schemaList, opts...); err != nil {
		return false, "", err
	}

	for _, schema := range schemaList.Items {
		if schema.Spec.Provider == target.Status.DiscoveryInfo.Provider &&
			schema.Spec.Version == target.Status.DiscoveryInfo.Version {
			schemaCondition := schema.GetCondition(condv1alpha1.ConditionTypeReady)
			msg := schemaCondition.Message
			if !schemaCondition.IsTrue() && msg == "" {
				msg = fmt.Sprintf("schema %s/%s not ready", schema.Spec.Provider, schema.Spec.Version)
			}
			return schemaCondition.IsTrue(), msg, nil
		}
	}
	return false, fmt.Sprintf("schema %s/%s referenced by target not found",
		target.Status.DiscoveryInfo.Provider, target.Status.DiscoveryInfo.Version), nil
}

func (r *reconciler) getCreateDataStoreRequest(ctx context.Context, target *configv1alpha1.Target) (*sdcpb.CreateDataStoreRequest, *configv1alpha1.TargetStatusUsedReferences, error) {
	usedReferences := &configv1alpha1.TargetStatusUsedReferences{}

	if target.Spec.SyncProfile == nil {
		return nil, nil, fmt.Errorf("target %s has no syncProfile", target.GetName())
	}
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
			Tls:           tls,
			Port:          uint32(connProfile.Spec.Port),
			DeviceProfile: toProtoDeviceProfile(connProfile.DeviceProfile()),
		},
		Schema: &sdcpb.Schema{
			Name:    "",
			Vendor:  target.Status.DiscoveryInfo.Provider,
			Version: target.Status.DiscoveryInfo.Version,
		},
	}

	if connProfile.Spec.Protocol == invv1alpha1.Protocol_GNMI {
		targetName := ""
		if connProfile.Spec.TargetName != nil {
			targetName = *connProfile.Spec.TargetName
		}
		req.Target.ProtocolOptions = &sdcpb.Target_GnmiOpts{
			GnmiOpts: &sdcpb.GnmiOptions{
				Encoding:   string(connProfile.Encoding()),
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

func usedRefsToApply(refs *configv1alpha1.TargetStatusUsedReferences) *configv1alpha1apply.TargetStatusUsedReferencesApplyConfiguration {
	if refs == nil {
		return nil
	}
	a := configv1alpha1apply.TargetStatusUsedReferences()
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