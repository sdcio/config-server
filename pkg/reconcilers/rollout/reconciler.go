/*
Copyright 2025 Nokia.

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

package rollout

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"sort"
	"time"

	"github.com/henderiw/apiserver-store/pkg/storebackend"
	memstore "github.com/henderiw/apiserver-store/pkg/storebackend/memory"
	"github.com/henderiw/logger/log"
	pkgerrors "github.com/pkg/errors"
	condv1alpha1 "github.com/sdcio/config-server/apis/condition/v1alpha1"
	configapi "github.com/sdcio/config-server/apis/config"
	configv1alpha1 "github.com/sdcio/config-server/apis/config/v1alpha1"
	invv1alpha1 "github.com/sdcio/config-server/apis/inv/v1alpha1"
	invv1alpha1apply "github.com/sdcio/config-server/pkg/generated/applyconfiguration/inv/v1alpha1"
	"github.com/sdcio/config-server/pkg/git/auth/secret"
	"github.com/sdcio/config-server/pkg/reconcilers"
	"github.com/sdcio/config-server/pkg/reconcilers/ctrlconfig"
	"github.com/sdcio/config-server/pkg/reconcilers/resource"
	workspacereader "github.com/sdcio/config-server/pkg/workspace"
	corev1 "k8s.io/api/core/v1"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/tools/events"
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
	crName                = "rollout"
	fieldmanagerfinalizer = "RolloutController-finalizer"
	reconcilerName        = "RolloutController"
	finalizer             = "rollout.inv.sdcio.dev/finalizer"
	// errors
	errGetCr        = "cannot get cr"
	errUpdateStatus = "cannot update status"
)

// SetupWithManager sets up the controller with the Manager.
func (r *reconciler) SetupWithManager(ctx context.Context, mgr ctrl.Manager, c interface{}) (map[schema.GroupVersionKind]chan event.GenericEvent, error) {
	var err error
	cfg, ok := c.(*ctrlconfig.ControllerConfig)
	if !ok {
		return nil, fmt.Errorf("cannot initialize, expecting controllerConfig, got: %s", reflect.TypeOf(c).Name())
	}

	r.client = mgr.GetClient()
	r.finalizer = resource.NewAPIFinalizer(
		mgr.GetClient(),
		finalizer,
		fieldmanagerfinalizer,
		func(name, namespace string, finalizers ...string) runtime.ApplyConfiguration {
			ac := invv1alpha1apply.Rollout(name, namespace)
			if len(finalizers) > 0 {
				ac.WithFinalizers(finalizers...)
			}
			return ac
		},
	)
	// initializes the directory
	r.workspaceReader, err = workspacereader.NewReader(
		cfg.WorkspaceDir,
		secret.NewCredentialResolver(mgr.GetClient(), []secret.Resolver{
			secret.NewBasicAuthResolver(),
		}),
	)
	if err != nil {
		return nil, pkgerrors.Wrap(err, "cannot initialize RolloutController")
	}
	r.recorder = mgr.GetEventRecorder(reconcilerName)

	return nil, ctrl.NewControllerManagedBy(mgr).
		Named(reconcilerName).
		For(&invv1alpha1.Rollout{}, builder.WithPredicates(predicate.GenerationChangedPredicate{})).
		Complete(r)
}

type reconciler struct {
	client          client.Client
	finalizer       *resource.APIFinalizer
	workspaceReader *workspacereader.Reader
	recorder        events.EventRecorder
}

func (r *reconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	ctx = ctrlconfig.InitContext(ctx, reconcilerName, req.NamespacedName)
	log := log.FromContext(ctx)
	log.Info("reconcile")

	rollout := &invv1alpha1.Rollout{}
	if err := r.client.Get(ctx, req.NamespacedName, rollout); err != nil {
		// if the resource no longer exists the reconcile loop is done
		if !k8serrors.IsNotFound(err) {
			log.Error(errGetCr, "error", err)
			return ctrl.Result{}, pkgerrors.Wrap(resource.IgnoreNotFound(err), errGetCr)
		}
		return ctrl.Result{}, nil
	}
	rolloutOrig := rollout.DeepCopy()

	if !rollout.GetDeletionTimestamp().IsZero() {
		// Remove all configs from the system
		existingConfigList := &configv1alpha1.ConfigList{}
		if err := r.client.List(ctx, existingConfigList); err != nil {
			return r.handleStatus(ctx, rolloutOrig, nil, condv1alpha1.Rollout("cannot get existing configs from apiserver"), true, err)
		}

		// Calculate the deleted configs with an empty newTargetUpdateConfigStore since we are deleting
		newTargetUpdateConfigStore := memstore.NewStore[storebackend.Storer[*configapi.Config]]() // empty
		newTargetDeleteConfigStore, err := getToBeDeletedconfigs(ctx, newTargetUpdateConfigStore, existingConfigList)
		if err != nil {
			return r.handleStatus(ctx, rolloutOrig, nil, condv1alpha1.Rollout("cannot calculate deleted configs"), true, err)
		}

		tm := NewTransactionManager(
			newTargetUpdateConfigStore,
			newTargetDeleteConfigStore,
			r.client,
			1*time.Minute,
			30*time.Second,
			rollout.GetSkipUnavailableTarget(),
		)
		targetStatus, err := tm.TransactToAllTargets(ctx, rollout.Spec.Ref)
		if err != nil {
			return r.handleStatus(ctx, rolloutOrig, targetStatus, condv1alpha1.Rollout("delete transaction failed"), false, err)
		}

		if err := r.updateConfigFromAPIServer(ctx, newTargetUpdateConfigStore, newTargetDeleteConfigStore); err != nil {
			return r.handleStatus(ctx, rolloutOrig, targetStatus, condv1alpha1.Rollout("cannot remove cr resources from apiserver"), true, err)
		}

		// remove the finalizer
		if err := r.finalizer.RemoveFinalizer(ctx, rollout); err != nil {
			return r.handleStatus(ctx, rolloutOrig, targetStatus, condv1alpha1.Rollout("cannot remove finalizer"), true, err)
		}
		// done deleting
		return ctrl.Result{}, nil
	}

	if err := r.finalizer.AddFinalizer(ctx, rollout); err != nil {
		// we always retry when status fails -> optimistic concurrency
		return r.handleStatus(ctx, rolloutOrig, nil, condv1alpha1.Rollout("cannot add finalizer"), true, err)
	}

	newTargetUpdateConfigStore, err := r.workspaceReader.GetConfigs(ctx, rollout)
	if err != nil {
		// we always retry when status fails -> optimistic concurrency
		return r.handleStatus(ctx, rolloutOrig, nil, condv1alpha1.Rollout("cannot get configs from git"), true, err)
	}

	configList := &configv1alpha1.ConfigList{}
	if err := r.client.List(ctx, configList); err != nil {
		// we always retry when status fails -> optimistic concurrency
		return r.handleStatus(ctx, rolloutOrig, nil, condv1alpha1.Rollout("cannot get existing configs from apiserver"), true, err)
	}

	newTargetDeleteConfigStore, err := getToBeDeletedconfigs(ctx, newTargetUpdateConfigStore, configList)
	if err != nil {
		// we always retry when status fails -> optimistic concurrency
		return r.handleStatus(ctx, rolloutOrig, nil, condv1alpha1.Rollout("cannot calculate deleted configs"), true, err)
	}

	tm := NewTransactionManager(
		newTargetUpdateConfigStore,
		newTargetDeleteConfigStore,
		r.client,
		1*time.Minute,
		30*time.Second,
		rollout.GetSkipUnavailableTarget(),
	)
	targetStatus, err := tm.TransactToAllTargets(ctx, rollout.Spec.Ref)
	if err != nil {
		return r.handleStatus(ctx, rolloutOrig, targetStatus, condv1alpha1.Failed("transaction failed"), false, err)
	}

	if err := r.updateConfigFromAPIServer(ctx, newTargetUpdateConfigStore, newTargetDeleteConfigStore); err != nil {
		return r.handleStatus(ctx, rolloutOrig, targetStatus, condv1alpha1.Rollout("cannot update api resources"), true, err)
	}
	// workspace ready -> rollout done and reference match
	return r.handleStatus(ctx, rolloutOrig, targetStatus, condv1alpha1.Ready(), false, nil)
}

func getToBeDeletedconfigs(
	ctx context.Context,
	newTargetConfigStore storebackend.Storer[storebackend.Storer[*configapi.Config]],
	existingConfigList *configv1alpha1.ConfigList,
) (storebackend.Storer[storebackend.Storer[*configapi.Config]], error) {

	log := log.FromContext(ctx)
	newTargetDeleteConfigStore := memstore.NewStore[storebackend.Storer[*configapi.Config]]()
	for _, existingConfig := range existingConfigList.Items {
		targetName, ok := existingConfig.Labels[configapi.TargetNameKey]
		if !ok {
			log.Warn("Skipping config missing targetName", "config", existingConfig.Name)
			continue
		}
		targetNamespace, ok := existingConfig.Labels[configapi.TargetNamespaceKey]
		if !ok {
			log.Warn("Skipping config missing targetNamespace", "config", existingConfig.Name)
			continue
		}

		internalcfg := &configapi.Config{}
		if err := configv1alpha1.Convert_v1alpha1_Config_To_config_Config(&existingConfig, internalcfg, nil); err != nil {
			return newTargetDeleteConfigStore, err
		}

		targetKey := storebackend.KeyFromNSN(types.NamespacedName{
			Namespace: targetNamespace,
			Name:      targetName,
		})
		configKey := storebackend.KeyFromNSN(types.NamespacedName{
			Namespace: existingConfig.Namespace,
			Name:      existingConfig.Name,
		})

		// Check if the target still exists in the new config store
		configStore, err := newTargetConfigStore.Get(ctx, targetKey)
		if err != nil {
			// If target is not found, mark all its configs for deletion
			deleteConfigStore, err := newTargetDeleteConfigStore.Get(ctx, targetKey)
			if err != nil {
				deleteConfigStore = memstore.NewStore[*configapi.Config]()
				if err := newTargetDeleteConfigStore.Create(ctx, targetKey, deleteConfigStore); err != nil {
					return nil, pkgerrors.Wrap(err, "cannot create target delete configstore")
				}
			}
			if err := deleteConfigStore.Create(ctx, configKey, internalcfg); err != nil {
				return nil, pkgerrors.Wrap(err, "cannot create config in delete config store")
			}
			if err := newTargetDeleteConfigStore.Update(ctx, targetKey, deleteConfigStore); err != nil {
				return nil, pkgerrors.Wrap(err, "cannot update target delete configstore")
			}
			continue
		}

		// If the config itself is missing from the new config store, mark for deletion
		if _, err := configStore.Get(ctx, configKey); err != nil {
			deleteConfigStore, err := newTargetDeleteConfigStore.Get(ctx, targetKey)
			if err != nil {
				deleteConfigStore = memstore.NewStore[*configapi.Config]()
				if err := newTargetDeleteConfigStore.Create(ctx, targetKey, deleteConfigStore); err != nil {
					return nil, pkgerrors.Wrap(err, "cannot create target delete configstore")
				}
			}
			if err := deleteConfigStore.Create(ctx, configKey, internalcfg); err != nil {
				return nil, pkgerrors.Wrap(err, "cannot create config in delete config store")
			}
			if err := newTargetDeleteConfigStore.Update(ctx, targetKey, deleteConfigStore); err != nil {
				return nil, pkgerrors.Wrap(err, "cannot update target delete configstore")
			}
		}
	}

	if err := newTargetDeleteConfigStore.List(ctx, func(ctx context.Context, k storebackend.Key, deleteConfigStore storebackend.Storer[*configapi.Config]) {
		configCount := 0
		if err := deleteConfigStore.List(ctx, func(ctx context.Context, k storebackend.Key, cfg *configapi.Config) {
			configCount++
		}); err != nil {
			log.Error("list failed", "err", err)
		}
		if configCount == 0 {
			_ = newTargetDeleteConfigStore.Delete(ctx, k)
		}
	}); err != nil {
		log.Error("list failed", "err", err)
	}

	return newTargetDeleteConfigStore, nil
}

func (r *reconciler) updateConfigFromAPIServer(ctx context.Context, updateStore, deleteStore storebackend.Storer[storebackend.Storer[*configapi.Config]]) error {
	log := log.FromContext(ctx)
	var errs error
	if err := updateStore.List(ctx, func(ctx context.Context, k storebackend.Key, s storebackend.Storer[*configapi.Config]) {
		if err := s.List(ctx, func(ctx context.Context, k storebackend.Key, c *configapi.Config) {
			if err := r.client.Create(ctx, c, &client.CreateOptions{
				FieldManager: reconcilerName,
			}); err != nil {
				errs = errors.Join(errs, err)
			}
		}); err != nil {
			log.Error("list failed", "err", err)
		}
	}); err != nil {
		log.Error("list failed", "err", err)
	}

	if err := deleteStore.List(ctx, func(ctx context.Context, k storebackend.Key, s storebackend.Storer[*configapi.Config]) {
		if err := s.List(ctx, func(ctx context.Context, k storebackend.Key, c *configapi.Config) {
			if err := r.client.Delete(ctx, c); err != nil {
				errs = errors.Join(errs, err)
			}
		}); err != nil {
			log.Error("list failed", "err", err)
		}

	}); err != nil {
		log.Error("list failed", "err", err)
	}
	return errs
}

func (r *reconciler) handleStatus(
	ctx context.Context,
	rollout *invv1alpha1.Rollout,
	targetStatus storebackend.Storer[invv1alpha1.RolloutTargetStatus],
	condition condv1alpha1.Condition,
	requeue bool,
	err error,
) (ctrl.Result, error) {
	log := log.FromContext(ctx).With("ref", rollout.Spec.Ref)

	if err != nil {
		condition.Message = fmt.Sprintf("%s err %s", condition.Message, err.Error())
	}

	newTargets := getTargetStatus(ctx, targetStatus)
	oldCond := rollout.GetCondition(condv1alpha1.ConditionType(condition.Type))

	if condition.Equal(oldCond) && reflect.DeepEqual(newTargets, rollout.Status.Targets) {
		log.Info("handleStatus -> no change")
		return ctrl.Result{Requeue: requeue}, nil
	}

	if condition.Type == string(condv1alpha1.ConditionTypeReady) {
		r.recorder.Eventf(rollout, nil, corev1.EventTypeNormal, crName, fmt.Sprintf("ready ref %s", rollout.Spec.Ref), "")
	} else {
		log.Error(condition.Message)
		r.recorder.Eventf(rollout, nil, corev1.EventTypeWarning, crName, condition.Message, "")
	}

	statusApply := invv1alpha1apply.RolloutStatus().
		WithConditions(condition).
		WithTargets(rolloutTargetStatusToApply(newTargets)...)

	applyConfig := invv1alpha1apply.Rollout(rollout.Name, rollout.Namespace).
		WithStatus(statusApply)

	result := ctrl.Result{Requeue: requeue}
	return result, pkgerrors.Wrap(r.client.Status().Apply(ctx, applyConfig, &client.SubResourceApplyOptions{
		ApplyOptions: client.ApplyOptions{
			FieldManager: reconcilerName,
		},
	}), errUpdateStatus)
}

// getTargetStatus is a convenience fn to reflect the target status in the Status field of the CR
func getTargetStatus(ctx context.Context, storeTargetStatus storebackend.Storer[invv1alpha1.RolloutTargetStatus]) []invv1alpha1.RolloutTargetStatus {
	log := log.FromContext(ctx)
	targetStatus := []invv1alpha1.RolloutTargetStatus{}

	if storeTargetStatus == nil {
		return targetStatus
	}

	if err := storeTargetStatus.List(ctx, func(ctx context.Context, k storebackend.Key, rts invv1alpha1.RolloutTargetStatus) {
		targetStatus = append(targetStatus, rts)
	}); err != nil {
		log.Error("list failed", "err", err)
	}

	sort.SliceStable(targetStatus, func(i, j int) bool {
		return targetStatus[i].Name < targetStatus[j].Name
	})
	return targetStatus
}

func rolloutTargetStatusToApply(targets []invv1alpha1.RolloutTargetStatus) []*invv1alpha1apply.RolloutTargetStatusApplyConfiguration {
	result := make([]*invv1alpha1apply.RolloutTargetStatusApplyConfiguration, 0, len(targets))
	for _, t := range targets {
		result = append(result, invv1alpha1apply.RolloutTargetStatus().
			WithName(t.Name),
		)
	}
	return result
}
