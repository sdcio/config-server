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
	"fmt"
	"sort"
	"strings"

	"github.com/henderiw/logger/log"
	"github.com/pkg/errors"
	"github.com/sdcio/config-server/apis/config"
	configv1alpha1 "github.com/sdcio/config-server/apis/config/v1alpha1"
	invv1alpha1 "github.com/sdcio/config-server/apis/inv/v1alpha1"
	"github.com/sdcio/config-server/pkg/reconcilers"
	"github.com/sdcio/config-server/pkg/reconcilers/ctrlconfig"
	"github.com/sdcio/config-server/pkg/reconcilers/eventhandler"
	"github.com/sdcio/config-server/pkg/reconcilers/resource"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/tools/record"
	"k8s.io/utils/ptr"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/event"
	condv1alpha1 "github.com/sdcio/config-server/apis/condition/v1alpha1"
)

func init() {
	reconcilers.Register(crName, &reconciler{})
}

const (
	crName         = "configset"
	controllerName = "ConfigSetController"
	finalizer      = "configset.config.sdcio.dev/finalizer"
	// errors
	errGetCr           = "cannot get cr"
	errUpdateDataStore = "cannot update datastore"
	errUpdateStatus    = "cannot update status"
)

// SetupWithManager sets up the controller with the Manager.
func (r *reconciler) SetupWithManager(ctx context.Context, mgr ctrl.Manager, c interface{}) (map[schema.GroupVersionKind]chan event.GenericEvent, error) {

	r.Client = mgr.GetClient()
	r.finalizer = resource.NewAPIFinalizer(mgr.GetClient(), finalizer)
	r.recorder = mgr.GetEventRecorderFor(controllerName)

	return nil, ctrl.NewControllerManagedBy(mgr).
		Named(controllerName).
		Owns(&configv1alpha1.Config{}).
		For(&configv1alpha1.ConfigSet{}).
		Watches(&invv1alpha1.Target{}, &eventhandler.TargetForConfigSet{Client: mgr.GetClient(), ControllerName: controllerName}).
		Complete(r)
}

type reconciler struct {
	client.Client
	finalizer *resource.APIFinalizer
	recorder  record.EventRecorder
}

func (r *reconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	ctx = ctrlconfig.InitContext(ctx, controllerName, req.NamespacedName)
	log := log.FromContext(ctx)
	log.Info("reconcile")

	configSet := &configv1alpha1.ConfigSet{}
	if err := r.Get(ctx, req.NamespacedName, configSet); err != nil {
		// if the resource no longer exists the reconcile loop is done
		if resource.IgnoreNotFound(err) != nil {
			log.Error(errGetCr, "error", err)
			return ctrl.Result{}, errors.Wrap(resource.IgnoreNotFound(err), errGetCr)
		}
		return ctrl.Result{}, nil
	}
	configSet = configSet.DeepCopy()

	if !configSet.GetDeletionTimestamp().IsZero() {
		//log.Info("delete")
		// list the configs per target
		existingChildConfigs := r.getOrphanConfigsFromConfigSet(ctx, configSet)
		//log.Info("delete existingConfigs", "total", len(existingChildConfigs))

		for nsn, existingChildConfig := range existingChildConfigs {
			//log.Info("delete existingChildConfig", "nsn", nsn)
			if err := r.Delete(ctx, existingChildConfig); err != nil {
				//log.Error("cannot delete config", "error", err.Error())
				r.recorder.Eventf(configSet, corev1.EventTypeWarning,
					"Error", "delete child config : %s error %s", nsn.String(), err.Error())
			}
		}

		if err := r.finalizer.RemoveFinalizer(ctx, configSet); err != nil {
			r.recorder.Eventf(configSet, corev1.EventTypeWarning,
				"Error", "error %s", err.Error())
			return ctrl.Result{Requeue: true}, errors.Wrap(r.Status().Update(ctx, configSet), errUpdateStatus)
		}
		return ctrl.Result{}, nil
	}

	if err := r.finalizer.AddFinalizer(ctx, configSet); err != nil {
		//log.Error("cannot add finalizer", "error", err)
		r.recorder.Eventf(configSet, corev1.EventTypeWarning,
			"Error", "error %s", err.Error())
		configSet.SetConditions(condv1alpha1.Failed(err.Error()))
		return ctrl.Result{Requeue: true}, errors.Wrap(r.Status().Update(ctx, configSet), errUpdateStatus)
	}

	targets, err := r.unrollDownstreamTargets(ctx, configSet)
	if err != nil {
		r.recorder.Eventf(configSet, corev1.EventTypeWarning,
			"Error", "error %s", err.Error())
		configSet.SetConditions(condv1alpha1.Failed(err.Error()))
		return ctrl.Result{Requeue: true}, errors.Wrap(r.Status().Update(ctx, configSet), errUpdateStatus)
	}

	if err := r.ensureConfigs(ctx, configSet, targets); err != nil {
		r.recorder.Eventf(configSet, corev1.EventTypeWarning,
			"Error", "error %s", err.Error())
		configSet.SetConditions(condv1alpha1.Failed(err.Error()))
		return ctrl.Result{Requeue: true}, errors.Wrap(r.Status().Update(ctx, configSet), errUpdateStatus)
	}

	msg := r.determineOverallStatus(ctx, configSet)
	if msg != "" {
		r.handleError(ctx, configSet, msg, nil)
	} else {
		configSet.SetConditions(condv1alpha1.Ready())
		r.recorder.Eventf(configSet, corev1.EventTypeNormal,
			"configset", "ready")
	}
	return ctrl.Result{}, errors.Wrap(r.Status().Update(ctx, configSet), errUpdateStatus)
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

	targetList := &invv1alpha1.TargetList{}
	if err := r.List(ctx, targetList, opts...); err != nil {
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

	// get the exisiting configs to see if the config is present; if the configset's target is no
	// longer applicable we will delete the config for this particular target
	existingConfigs := r.getOrphanConfigsFromConfigSet(ctx, configSet)

	// TODO run in parallel and/or try 1 first to see if the validation works or not
	TargetsStatus := make([]configv1alpha1.TargetStatus, len(targets))
	for i, target := range targets {
		TargetsStatus[i] = configv1alpha1.TargetStatus{Name: target.Name}

		var oldConfig *configv1alpha1.Config
		newConfig := buildConfig(ctx, configSet, target)

		// check if the config is part of the existing map
		// if not create the config
		// if yes check if the config needs updates
		// all other configs will be deleted afterwards since they are no longer needed
		nsnKey := types.NamespacedName{Namespace: newConfig.Namespace, Name: newConfig.Name}
		oldConfig, ok := existingConfigs[nsnKey]
		// delete the config from the existing configs -> this list is emptied such that the remaining entries
		// can be deleted
		delete(existingConfigs, nsnKey)
		if !ok { // config does not exist -> create it
			//log.Info("config does not exist", "nsn", nsnKey.String())

			if err := r.Create(ctx, newConfig); err != nil {
				TargetsStatus[i].Condition = condv1alpha1.Failed(err.Error())
				log.Error("cannot create config", "name", nsnKey.Name, "error", err.Error())
				continue
			}
			TargetsStatus[i].Condition = configv1alpha1.Creating()
		} else {
			//log.Info("config exists", "nsn", nsnKey.String())
			// TODO better logic to validate changes
			TargetsStatus[i].Condition = oldConfig.GetCondition(condv1alpha1.ConditionTypeReady)
			newConfig = oldConfig.DeepCopy()
			newConfig.Spec = configv1alpha1.ConfigSpec{
				Lifecycle: configSet.Spec.Lifecycle,
				Priority:  configSet.Spec.Priority,
				Config:    configSet.Spec.Config,
			}
			if len(newConfig.GetLabels()) == 0 {
				newConfig.Labels = make(map[string]string, len(configSet.GetLabels()))
			}
			for k, v := range configSet.GetLabels() {
				newConfig.Labels[k] = v
			}
			if len(newConfig.GetAnnotations()) == 0 {
				newConfig.Annotations = make(map[string]string, len(configSet.GetAnnotations()))
			}
			for k, v := range configSet.GetAnnotations() {
				newConfig.Annotations[k] = v
			}

			newHash, err := newConfig.CalculateHash()
			if err != nil {
				TargetsStatus[i].Condition = condv1alpha1.Failed(err.Error())
				log.Error("cannot calculate hash", "name", nsnKey.Name, "error", err.Error())
				continue
			}
			oldHash, err := oldConfig.CalculateHash()
			if err != nil {
				TargetsStatus[i].Condition = condv1alpha1.Failed(err.Error())
				log.Error("cannot calculate hash", "name", nsnKey.Name, "error", err.Error())
				continue
			}

			if oldHash == newHash {
				TargetsStatus[i] = configv1alpha1.TargetStatus{
					Name:      target.Name,
					Condition: oldConfig.GetCondition(condv1alpha1.ConditionTypeReady),
				}
				continue
			}
			if err := r.Update(ctx, newConfig); err != nil {
				TargetsStatus[i].Condition = condv1alpha1.Failed(err.Error())
				log.Error("cannot update config", "name", nsnKey.Name, "error", err.Error())
				continue
			}
			TargetsStatus[i].Condition = configv1alpha1.Updating()
		}
	}

	// These configs no longer match a target
	for _, existingConfig := range existingConfigs {
		log.Info("existing config delete", "existingConfig", existingConfig.Name)
		if err := r.Delete(ctx, existingConfig); err != nil {
			log.Error("delete existing intent failed", "error", err)
		}
	}
	configSet.Status.Targets = TargetsStatus

	return nil
}

// getOrphanConfigsFromConfigSet returns the children owned by this configSet
func (r *reconciler) getOrphanConfigsFromConfigSet(ctx context.Context, configSet *configv1alpha1.ConfigSet) map[types.NamespacedName]*configv1alpha1.Config {
	log := log.FromContext(ctx)
	existingConfigs := map[types.NamespacedName]*configv1alpha1.Config{}
	// get existing configs
	configList := &configv1alpha1.ConfigList{}
	if err := r.List(ctx, configList); err != nil {
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
	labels := configSet.Labels
	if len(labels) == 0 {
		labels = map[string]string{}
	}
	labels[config.TargetNameKey] = target.Name
	labels[config.TargetNamespaceKey] = target.Namespace

	return configv1alpha1.BuildConfig(
		metav1.ObjectMeta{
			Name:        fmt.Sprintf("%s-%s", configSet.Name, target.Name),
			Namespace:   configSet.Namespace,
			Labels:      labels,
			Annotations: configSet.Annotations,
			OwnerReferences: []metav1.OwnerReference{
				{
					APIVersion: configSet.APIVersion,
					Kind:       configSet.Kind,
					Name:       configSet.Name,
					UID:        configSet.UID,
					Controller: ptr.To[bool](true),
				},
			},
		},
		configv1alpha1.ConfigSpec{
			Lifecycle: configSet.Spec.Lifecycle,
			Priority:  configSet.Spec.Priority,
			Config:    configSet.Spec.Config,
		},
		configv1alpha1.ConfigStatus{},
	)
}

func (r *reconciler) handleError(ctx context.Context, cr *configv1alpha1.ConfigSet, msg string, err error) {
	log := log.FromContext(ctx)
	if err == nil {
		cr.SetConditions(condv1alpha1.Failed(msg))
		log.Error(msg)
		r.recorder.Eventf(cr, corev1.EventTypeWarning, crName, msg)
	} else {
		cr.SetConditions(condv1alpha1.Failed(err.Error()))
		log.Error(msg, "error", err)
		r.recorder.Eventf(cr, corev1.EventTypeWarning, crName, fmt.Sprintf("%s, err: %s", msg, err.Error()))
	}
}

func (r *reconciler) determineOverallStatus(_ context.Context, configSet *configv1alpha1.ConfigSet) string {
	var sb strings.Builder
	for _, targetStatus := range configSet.Status.Targets {
		if targetStatus.Condition.Status == metav1.ConditionFalse {
			sb.WriteString(fmt.Sprintf("target %s config not ready, msg %s;", targetStatus.Name, targetStatus.Condition.Message))
		}
	}
	return sb.String()
}
