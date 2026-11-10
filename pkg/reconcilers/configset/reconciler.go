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

package configset

import (
	"context"
	merrors "errors"
	"fmt"
	"maps"
	"sort"
	"strings"

	"github.com/henderiw/logger/log"
	"github.com/pkg/errors"
	condv1alpha1 "github.com/sdcio/config-server/apis/condition/v1alpha1"
	"github.com/sdcio/config-server/apis/config"
	configv1alpha1 "github.com/sdcio/config-server/apis/config/v1alpha1"
	configv1alpha1apply "github.com/sdcio/config-server/pkg/generated/applyconfiguration/config/v1alpha1"
	"github.com/sdcio/config-server/pkg/reconcilers"
	"github.com/sdcio/config-server/pkg/reconcilers/ctrlconfig"
	"github.com/sdcio/config-server/pkg/reconcilers/eventhandler"
	"github.com/sdcio/config-server/pkg/reconcilers/resource"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/tools/events"
	"k8s.io/utils/ptr"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/event"
)

func init() {
	reconcilers.Register(crName, &reconciler{})
}

const (
	crName                = "configset"
	fieldmanagerfinalizer = "ConfigSetController-finalizer"
	reconcilerName        = "ConfigSetController"
	finalizer             = "configset.config.sdcio.dev/finalizer"
	// errors
	errGetCr           = "cannot get cr"
	errUpdateDataStore = "cannot update datastore"
	errUpdateStatus    = "cannot update status"
)

// SetupWithManager sets up the controller with the Manager.
func (r *reconciler) SetupWithManager(ctx context.Context, mgr ctrl.Manager, c interface{}) (map[schema.GroupVersionKind]chan event.GenericEvent, error) {
	r.client = mgr.GetClient()
	r.finalizer = resource.NewAPIFinalizer(
		mgr.GetClient(),
		finalizer,
		fieldmanagerfinalizer,
		func(name, namespace string, finalizers ...string) runtime.ApplyConfiguration {
			ac := configv1alpha1apply.ConfigSet(name, namespace)
			if len(finalizers) > 0 {
				ac.WithFinalizers(finalizers...)
			}
			return ac
		},
	)
	r.recorder = mgr.GetEventRecorder(reconcilerName)

	return nil, ctrl.NewControllerManagedBy(mgr).
		Named(reconcilerName).
		Owns(&configv1alpha1.Config{}).
		For(&configv1alpha1.ConfigSet{}).
		Watches(&configv1alpha1.Target{}, &eventhandler.TargetForConfigSet{Client: mgr.GetClient(), ControllerName: reconcilerName}).
		Complete(r)
}

type reconciler struct {
	client    client.Client
	finalizer *resource.APIFinalizer
	recorder  events.EventRecorder
}

func (r *reconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	ctx = ctrlconfig.InitContext(ctx, reconcilerName, req.NamespacedName)
	log := log.FromContext(ctx)
	log.Info("reconcile")

	configSet := &configv1alpha1.ConfigSet{}
	if err := r.client.Get(ctx, req.NamespacedName, configSet); err != nil {
		// if the resource no longer exists the reconcile loop is done
		if resource.IgnoreNotFound(err) != nil {
			log.Error(errGetCr, "error", err)
			return ctrl.Result{}, errors.Wrap(resource.IgnoreNotFound(err), errGetCr)
		}
		return ctrl.Result{}, nil
	}
	configSetOrig := configSet.DeepCopy()

	if !configSet.GetDeletionTimestamp().IsZero() {
		// list the configs per target
		existingChildConfigs := r.getOrphanConfigsFromConfigSet(ctx, configSet)

		var errs error
		for nsn, existingChildConfig := range existingChildConfigs {
			if err := r.client.Delete(ctx, existingChildConfig); err != nil {
				errs = merrors.Join(errs, fmt.Errorf("cannot delete child config %s, err: %v", nsn, err))
			}
		}
		if errs != nil {
			return ctrl.Result{Requeue: true},
				errors.Wrap(r.handleError(ctx, configSetOrig, configSet, "cannot delete child configs", errs), errUpdateStatus)
		}

		// it is possible that the child resource cannot be deleted - due to dependency
		// we should only remove the finalizer when all resources get deleted.
		if len(existingChildConfigs) != 0 {
			log.Info("not all resources deleted, will retry once upon child config removal")
			return ctrl.Result{}, nil
		}

		if err := r.finalizer.RemoveFinalizer(ctx, configSet); err != nil {
			return ctrl.Result{Requeue: true},
				errors.Wrap(r.handleError(ctx, configSetOrig, configSet, "cannot delete finalizer", err), errUpdateStatus)
		}
		return ctrl.Result{}, nil
	}

	if err := r.finalizer.AddFinalizer(ctx, configSet); err != nil {
		return ctrl.Result{Requeue: true},
			errors.Wrap(r.handleError(ctx, configSetOrig, configSet, "cannot add finalizer", err), errUpdateStatus)
	}

	targets, err := r.unrollDownstreamTargets(ctx, configSet)
	if err != nil {
		return ctrl.Result{Requeue: true},
			errors.Wrap(r.handleError(ctx, configSetOrig, configSet, "cannot unroll downstream targets", err), errUpdateStatus)
	}

	if err := r.ensureConfigs(ctx, configSet, targets); err != nil {
		return ctrl.Result{Requeue: true},
			errors.Wrap(r.handleError(ctx, configSetOrig, configSet, "cannot ensure configs", err), errUpdateStatus)
	}

	// Child Config changes (Owns) re-trigger this reconcile, so pending/failed
	// states do not need an explicit requeue.
	switch state, msg := determineOverallStatus(configSet); state {
	case stateFailed:
		return ctrl.Result{},
			errors.Wrap(r.handleError(ctx, configSetOrig, configSet, msg, nil), errUpdateStatus)
	case statePending:
		return ctrl.Result{},
			errors.Wrap(r.handlePending(ctx, configSetOrig, configSet, msg), errUpdateStatus)
	}
	return ctrl.Result{}, errors.Wrap(r.handleSuccess(ctx, configSetOrig, configSet), errUpdateStatus)
}

// unrollDownstreamTargets list the targets
func (r *reconciler) unrollDownstreamTargets(ctx context.Context, configSet *configv1alpha1.ConfigSet) ([]types.NamespacedName, error) {
	selector, err := metav1.LabelSelectorAsSelector(configSet.Spec.Target.TargetSelector)
	if err != nil {
		return nil, fmt.Errorf("parsing selector failed: err: %s", err.Error())
	}
	opts := []client.ListOption{
		client.InNamespace(configSet.Namespace),
		client.MatchingLabelsSelector{Selector: selector},
	}

	targetList := &configv1alpha1.TargetList{}
	if err := r.client.List(ctx, targetList, opts...); err != nil {
		return nil, err
	}
	targets := make([]types.NamespacedName, 0, len(targetList.Items))
	for _, target := range targetList.Items {
		// only add targets that are not in deleting state
		if target.GetDeletionTimestamp().IsZero() {
			targets = append(targets, types.NamespacedName{Name: target.Name, Namespace: configSet.Namespace})
		}
	}
	sort.Slice(targets, func(i, j int) bool {
		return targets[i].Name < targets[j].Name
	})
	return targets, nil
}

func (r *reconciler) ensureConfigs(ctx context.Context, configSet *configv1alpha1.ConfigSet, targets []types.NamespacedName) error {
	log := log.FromContext(ctx)

	// get the existing configs to see if the config is present; if the configset's target is no
	// longer applicable we will delete the config for this particular target
	existingConfigs := r.getOrphanConfigsFromConfigSet(ctx, configSet)

	// TODO run in parallel and/or try 1 first to see if the validation works or not
	targetsStatus := make([]configv1alpha1.ConfigSetTargetStatus, len(targets))
	for i, target := range targets {
		targetsStatus[i] = configv1alpha1.ConfigSetTargetStatus{Name: target.Name}

		desired := buildConfig(ctx, configSet, target)
		nsnKey := types.NamespacedName{Namespace: desired.Namespace, Name: desired.Name}

		// remove from the existing map; whatever remains afterwards is deleted
		oldConfig, ok := existingConfigs[nsnKey]
		delete(existingConfigs, nsnKey)

		if !ok { // config does not exist -> create it
			if err := r.client.Create(ctx, desired); err != nil {
				targetsStatus[i].Condition = condv1alpha1.Failed(err.Error())
				log.Error("cannot create config", "name", nsnKey.Name, "error", err.Error())
				continue
			}
			targetsStatus[i].Condition = configv1alpha1.ConfigNotReady(configv1alpha1.ConfigReasonPending, "creating")
			continue
		}

		// config exists -> update spec/labels/annotations if they drifted
		newConfig := oldConfig.DeepCopy()
		newConfig.Spec = desired.Spec
		newConfig.Labels = mergeStringMap(newConfig.Labels, desired.Labels)
		newConfig.Annotations = mergeStringMap(newConfig.Annotations, desired.Annotations)

		newHash, err := newConfig.CalculateHash()
		if err != nil {
			targetsStatus[i].Condition = condv1alpha1.Failed(err.Error())
			log.Error("cannot calculate hash", "name", nsnKey.Name, "error", err.Error())
			continue
		}
		oldHash, err := oldConfig.CalculateHash()
		if err != nil {
			targetsStatus[i].Condition = condv1alpha1.Failed(err.Error())
			log.Error("cannot calculate hash", "name", nsnKey.Name, "error", err.Error())
			continue
		}

		if oldHash == newHash {
			targetsStatus[i].Condition = readyOf(oldConfig)
			continue
		}
		if err := r.client.Update(ctx, newConfig); err != nil {
			targetsStatus[i].Condition = condv1alpha1.Failed(err.Error())
			log.Error("cannot update config", "name", nsnKey.Name, "error", err.Error())
			continue
		}
		targetsStatus[i].Condition = configv1alpha1.ConfigNotReady(configv1alpha1.ConfigReasonPending, "updating")
	}

	// These configs no longer match a target
	for _, existingConfig := range existingConfigs {
		log.Info("existing config delete", "existingConfig", existingConfig.Name)
		if err := r.client.Delete(ctx, existingConfig); err != nil {
			log.Error("delete existing intent failed", "error", err)
		}
	}
	configSet.Status.Targets = targetsStatus

	return nil
}

// readyOf returns the child's Ready condition. GetCondition returns
// {Type: Ready, Status: False} with an empty Reason when the condition is
// absent; a freshly created child is waiting for its first transaction, so
// report it as Pending, the same as the API does (ConfigNotReady).
func readyOf(cfg *configv1alpha1.Config) condv1alpha1.Condition {
	c := cfg.GetCondition(condv1alpha1.ConditionTypeReady)
	if c.Reason == "" {
		return configv1alpha1.ConfigNotReady(configv1alpha1.ConfigReasonPending, "config has not reported Ready yet")
	}
	return c
}

// getOrphanConfigsFromConfigSet returns the children owned by this configSet
func (r *reconciler) getOrphanConfigsFromConfigSet(ctx context.Context, configSet *configv1alpha1.ConfigSet) map[types.NamespacedName]*configv1alpha1.Config {
	log := log.FromContext(ctx)
	existingConfigs := map[types.NamespacedName]*configv1alpha1.Config{}
	// get existing configs
	configList := &configv1alpha1.ConfigList{}
	if err := r.client.List(ctx, configList); err != nil {
		log.Error("unexpected object in store")
		return existingConfigs
	}

	for _, config := range configList.Items {
		for _, ref := range config.OwnerReferences {
			if ref.APIVersion == configSet.APIVersion &&
				ref.Kind == configSet.Kind &&
				ref.Name == configSet.Name &&
				ref.UID == configSet.UID {
				existingConfigs[types.NamespacedName{Namespace: config.Namespace, Name: config.Name}] = config.DeepCopy()
			}
		}
	}

	return existingConfigs
}

func buildConfig(_ context.Context, configSet *configv1alpha1.ConfigSet, target types.NamespacedName) *configv1alpha1.Config {
	// clone: never write target-specific keys into the ConfigSet's own maps
	labels := maps.Clone(configSet.Labels)
	if labels == nil {
		labels = map[string]string{}
	}
	labels[config.TargetNameKey] = target.Name
	labels[config.TargetNamespaceKey] = target.Namespace

	return configv1alpha1.BuildConfig(
		metav1.ObjectMeta{
			Name:        fmt.Sprintf("%s-%s", configSet.Name, target.Name),
			Namespace:   configSet.Namespace,
			Labels:      labels,
			Annotations: maps.Clone(configSet.Annotations),
			OwnerReferences: []metav1.OwnerReference{
				{
					APIVersion: configSet.APIVersion,
					Kind:       configSet.Kind,
					Name:       configSet.Name,
					UID:        configSet.UID,
					Controller: ptr.To(true),
				},
			},
		},
		configv1alpha1.ConfigSpec{
			Lifecycle: configSet.Spec.Lifecycle,
			Priority:  configSet.Spec.Priority,
			Config:    configSet.Spec.Config,
			Revertive: configSet.Spec.Revertive,
		},
		configv1alpha1.ConfigStatus{},
	)
}

func mergeStringMap(dst, src map[string]string) map[string]string {
	if len(src) == 0 {
		return dst
	}
	if dst == nil {
		dst = make(map[string]string, len(src))
	}
	for k, v := range src {
		dst[k] = v
	}
	return dst
}

func (r *reconciler) handleSuccess(ctx context.Context, configSetOrig, configSet *configv1alpha1.ConfigSet) error {
	return r.updateStatus(ctx, configSetOrig, configSet, condv1alpha1.Ready(), corev1.EventTypeNormal)
}

func (r *reconciler) handlePending(ctx context.Context, configSetOrig, configSet *configv1alpha1.ConfigSet, msg string) error {
	return r.updateStatus(ctx, configSetOrig, configSet,
		condv1alpha1.NewCondition(condv1alpha1.ConditionTypeReady, metav1.ConditionFalse, configv1alpha1.ConfigReasonPending, msg),
		corev1.EventTypeNormal)
}

func (r *reconciler) handleError(ctx context.Context, configSetOrig, configSet *configv1alpha1.ConfigSet, msg string, err error) error {
	if err != nil {
		msg = fmt.Sprintf("%s err %s", msg, err.Error())
	}
	return r.updateStatus(ctx, configSetOrig, configSet, condv1alpha1.Failed(msg), corev1.EventTypeWarning)
}

// updateStatus applies the Ready condition and per-target statuses via SSA.
// lastTransitionTime is only moved when a condition's status actually changes,
// and no-op writes are skipped (comparison ignores timestamps).
func (r *reconciler) updateStatus(
	ctx context.Context,
	configSetOrig, configSet *configv1alpha1.ConfigSet,
	newCond condv1alpha1.Condition,
	eventType string,
) error {
	log := log.FromContext(ctx)

	newCond.ObservedGeneration = configSet.Generation
	oldCond := configSetOrig.GetCondition(condv1alpha1.ConditionTypeReady)
	newCond.LastTransitionTime = keepTransitionTime(oldCond, newCond)

	carryTargetTransitionTimes(configSetOrig.Status.Targets, configSet.Status.Targets)

	condChanged := !newCond.Equal(oldCond)
	if !condChanged && targetsEqual(configSet.Status.Targets, configSetOrig.Status.Targets) {
		log.Info("updateStatus -> no change", "reason", newCond.Reason)
		return nil
	}

	if condChanged {
		if eventType == corev1.EventTypeWarning {
			log.Error(newCond.Message)
		}
		msg := newCond.Message
		if msg == "" {
			msg = strings.ToLower(newCond.Reason)
		}
		reconcilers.RecordEvent(r.recorder, configSet, eventType, configv1alpha1.ConfigSetKind, msg)
	}

	statusApply := configv1alpha1apply.ConfigSetStatus().
		WithConditions(newCond).
		WithTargets(targetStatusToApply(configSet.Status.Targets)...)

	applyConfig := configv1alpha1apply.ConfigSet(configSet.Name, configSet.Namespace).
		WithStatus(statusApply)

	return r.client.Status().Apply(ctx, applyConfig, &client.SubResourceApplyOptions{
		ApplyOptions: client.ApplyOptions{
			FieldManager: reconcilerName,
		},
	})
}

type overallState int

const (
	stateReady overallState = iota
	statePending
	stateFailed
)

// classify maps a target's Ready condition to an aggregate state.
// Only the outcome reasons Failed and Unrecoverable are failures; every other
// Ready=False reason of a Config (Pending, TargetNotFound, TargetDeleting,
// TargetNotReady, TargetRuntimeNotReady) means the config is still waiting.
func classify(c condv1alpha1.Condition) overallState {
	if c.IsTrue() {
		return stateReady
	}
	switch condv1alpha1.ConditionReason(c.Reason) {
	case condv1alpha1.ConditionReasonFailed, condv1alpha1.ConditionReasonUnrecoverable:
		return stateFailed
	default:
		return statePending
	}
}

func determineOverallStatus(configSet *configv1alpha1.ConfigSet) (overallState, string) {
	var failed, pending []string
	for _, ts := range configSet.Status.Targets {
		switch classify(ts.Condition) {
		case stateFailed:
			failed = append(failed, fmt.Sprintf("target %s: %s", ts.Name, ts.Message))
		case statePending:
			pending = append(pending, fmt.Sprintf("target %s: %s", ts.Name, ts.Reason))
		}
	}
	switch {
	case len(failed) > 0:
		return stateFailed, "configs not ready; " + strings.Join(failed, "; ")
	case len(pending) > 0:
		return statePending, "configs pending; " + strings.Join(pending, "; ")
	default:
		return stateReady, ""
	}
}

// keepTransitionTime returns the old transition time if the status did not change.
func keepTransitionTime(oldCond, newCond condv1alpha1.Condition) metav1.Time {
	if oldCond.Status == newCond.Status && !oldCond.LastTransitionTime.IsZero() {
		return oldCond.LastTransitionTime
	}
	return newCond.LastTransitionTime
}

func carryTargetTransitionTimes(orig, cur []configv1alpha1.ConfigSetTargetStatus) {
	prev := make(map[string]condv1alpha1.Condition, len(orig))
	for _, t := range orig {
		prev[t.Name] = t.Condition
	}
	for i := range cur {
		if old, ok := prev[cur[i].Name]; ok {
			cur[i].LastTransitionTime = keepTransitionTime(old, cur[i].Condition)
		}
	}
}

func targetsEqual(a, b []configv1alpha1.ConfigSetTargetStatus) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i].Name != b[i].Name || !a[i].Equal(b[i].Condition) {
			return false
		}
	}
	return true
}

func targetStatusToApply(targets []configv1alpha1.ConfigSetTargetStatus) []*configv1alpha1apply.ConfigSetTargetStatusApplyConfiguration {
	result := make([]*configv1alpha1apply.ConfigSetTargetStatusApplyConfiguration, 0, len(targets))
	for _, t := range targets {
		ac := configv1alpha1apply.ConfigSetTargetStatus().
			WithName(t.Name)
		ac.Condition = t.Condition
		result = append(result, ac)
	}
	return result
}