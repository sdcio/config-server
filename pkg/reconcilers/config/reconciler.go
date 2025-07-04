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

package config

import (
	"context"
	"encoding/json"
	"fmt"
	"reflect"
	"time"

	"github.com/henderiw/logger/log"
	"github.com/pkg/errors"
	condv1alpha1 "github.com/sdcio/config-server/apis/condition/v1alpha1"
	"github.com/sdcio/config-server/apis/config"
	configv1alpha1 "github.com/sdcio/config-server/apis/config/v1alpha1"
	invv1alpha1 "github.com/sdcio/config-server/apis/inv/v1alpha1"
	"github.com/sdcio/config-server/pkg/reconcilers"
	"github.com/sdcio/config-server/pkg/reconcilers/ctrlconfig"
	"github.com/sdcio/config-server/pkg/reconcilers/eventhandler"
	"github.com/sdcio/config-server/pkg/reconcilers/resource"
	"github.com/sdcio/config-server/pkg/target"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/equality"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/tools/record"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/event"
	"k8s.io/utils/ptr"
)

func init() {
	reconcilers.Register(crName, &reconciler{})
}

const (
	crName         = "config"
	reconcilerName = "ConfigController"
	finalizer      = "config.config.sdcio.dev/finalizer"
	// errors
	errGetCr           = "cannot get cr"
	errUpdateDataStore = "cannot update datastore"
	errUpdateStatus    = "cannot update status"
)

// SetupWithManager sets up the controller with the Manager.
func (r *reconciler) SetupWithManager(ctx context.Context, mgr ctrl.Manager, c interface{}) (map[schema.GroupVersionKind]chan event.GenericEvent, error) {
	cfg, ok := c.(*ctrlconfig.ControllerConfig)
	if !ok {
		return nil, fmt.Errorf("cannot initialize, expecting controllerConfig, got: %s", reflect.TypeOf(c).Name())
	}

	r.Client = mgr.GetClient()
	r.finalizer = resource.NewAPIFinalizer(mgr.GetClient(), finalizer, reconcilerName)
	r.targetHandler = cfg.TargetHandler
	r.recorder = mgr.GetEventRecorderFor(reconcilerName)

	return nil, ctrl.NewControllerManagedBy(mgr).
		Named(reconcilerName).
		For(&configv1alpha1.Config{}).
		Watches(&invv1alpha1.Target{}, &eventhandler.TargetForConfigEventHandler{Client: mgr.GetClient(), ControllerName: reconcilerName}).
		Watches(&configv1alpha1.Deviation{}, &eventhandler.DeviationForConfigEventHandler{Client: mgr.GetClient(), ControllerName: reconcilerName}).
		Complete(r)
}

type reconciler struct {
	client.Client
	finalizer     *resource.APIFinalizer
	targetHandler target.TargetHandler
	recorder      record.EventRecorder
}

func (r *reconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	ctx = ctrlconfig.InitContext(ctx, reconcilerName, req.NamespacedName)
	log := log.FromContext(ctx)
	log.Info("reconcile")

	cfg := &configv1alpha1.Config{}
	if err := r.Get(ctx, req.NamespacedName, cfg); err != nil {
		// if the resource no longer exists the reconcile loop is done
		if resource.IgnoreNotFound(err) != nil {
			log.Error(errGetCr, "error", err)
			return ctrl.Result{}, errors.Wrap(resource.IgnoreNotFound(err), errGetCr)
		}
		return ctrl.Result{}, nil
	}
	cfgOrig := cfg.DeepCopy()
	internalcfg := &config.Config{}
	if err := configv1alpha1.Convert_v1alpha1_Config_To_config_Config(cfg, internalcfg, nil); err != nil {
		r.handleError(ctx, cfgOrig, cfg, "cannot convert config", err, true)
		return ctrl.Result{}, errors.Wrap(r.Status().Update(ctx, cfg), errUpdateStatus)
	}

	targetKey, err := config.GetTargetKey(cfg.GetLabels())
	if err != nil {
		// this should never fail since validation covered this
		return ctrl.Result{}, nil
	}

	if !cfg.GetDeletionTimestamp().IsZero() {
		if _, err := r.targetHandler.DeleteIntent(ctx, targetKey, internalcfg, false); err != nil {
			if errors.Is(err, target.ErrLookup) {
				log.Warn("deleted config, target unavailable", "config", req, "err", err)
				// Since the target is not available we delete the resource
				// The target config might not be deleted
				if err := r.finalizer.RemoveFinalizer(ctx, cfg); err != nil {
					return ctrl.Result{Requeue: true},
						errors.Wrap(r.handleError(ctx, cfgOrig, cfg, "cannot delete finalizer", err, true), errUpdateStatus)
				}
				return ctrl.Result{}, nil
			}
			// all grpc errors except resource exhausted will not retry
			// and a human need to intervene
			var txErr *target.TransactionError
			if errors.As(err, &txErr) && txErr.Recoverable {
				// Retry logic for recoverable errors
				return ctrl.Result{Requeue: true, RequeueAfter: 5 * time.Second},
					errors.Wrap(r.handleError(ctx, cfgOrig, cfg, "set intent failed (recoverable)", err, true), errUpdateStatus)
			}

			return ctrl.Result{}, errors.Wrap(r.handleError(ctx, cfgOrig, cfg, "delete intent failed", err, false), errUpdateStatus)

		}

		// delete the deviation
		deviation := configv1alpha1.BuildDeviation(metav1.ObjectMeta{Name: cfg.Name, Namespace: cfg.Namespace}, nil, nil)
		if err = r.Client.Delete(ctx, deviation); err != nil {
			if resource.IgnoreNotFound(err) != nil {
				return ctrl.Result{Requeue: true},
				errors.Wrap(r.handleError(ctx, cfgOrig, cfg, "cannot delete finalizer", err, true), errUpdateStatus)
			}
		}

		if err := r.finalizer.RemoveFinalizer(ctx, cfg); err != nil {
			return ctrl.Result{Requeue: true},
				errors.Wrap(r.handleError(ctx, cfgOrig, cfg, "cannot delete finalizer", err, true), errUpdateStatus)
		}
		return ctrl.Result{}, nil
	}

	if err := r.finalizer.AddFinalizer(ctx, cfg); err != nil {
		return ctrl.Result{Requeue: true},
			errors.Wrap(r.handleError(ctx, cfgOrig, cfg, "cannot add finalizer", err, true), errUpdateStatus)
	}

	if _, _, err := r.targetHandler.GetTargetContext(ctx, targetKey); err != nil {
		log.Info("applying config -> target not ready")
		return ctrl.Result{}, errors.Wrap(r.handleError(ctx, cfgOrig, cfg, "target not ready", err, true), errUpdateStatus)
	}

	log.Info("applying config -> target ready")

	deviation, err := r.getDeviation(ctx, cfg)
	if err != nil {
		log.Info("getting deviation failed")
		return ctrl.Result{}, errors.Wrap(r.handleError(ctx, cfgOrig, cfg, "cannot get deviation", err, true), errUpdateStatus)
	}


	// check if we have to reapply the config
	// if condition is false -> reapply the config
	// if the applied Config is not set -> reapply the config
	// if the applied Config is different than the spec -> reapply the config
	// if the deviation is having the reason xx -> reapply the config
	if cfg.IsConditionReady() &&
		cfg.Status.AppliedConfig != nil &&
		cfg.Spec.GetShaSum(ctx) == cfg.Status.AppliedConfig.GetShaSum(ctx) &&
		cfg.HashDeviationGenerationChanged(deviation) {
		return ctrl.Result{}, nil
	}

	// Check if we got an unrecoverable error and if the resourceVersion has not changed we can stop here
	if !cfg.IsRecoverable() {
		return ctrl.Result{}, nil
	}

	internalDeviation := &config.Deviation{}
	if err := configv1alpha1.Convert_v1alpha1_Deviation_To_config_Deviation(deviation, internalDeviation, nil); err != nil {
		r.handleError(ctx, cfgOrig, cfg, "cannot convert deviation", err, true)
		return ctrl.Result{}, errors.Wrap(r.Status().Update(ctx, cfg), errUpdateStatus)
	}

	internalSchema, warnings, err := r.targetHandler.SetIntent(ctx, targetKey, internalcfg, internalDeviation, false)
	if err != nil {
		// TODO distinguish between recoeverable and non recoverable
		var txErr *target.TransactionError
		if errors.As(err, &txErr) && txErr.Recoverable {
			// Retry logic for recoverable errors
			return ctrl.Result{Requeue: true, RequeueAfter: 5 * time.Second},
				errors.Wrap(r.handleError(ctx, cfgOrig, cfg, processMessageWithWarning("set intent failed (recoverable)", warnings), err, true), errUpdateStatus)
		}
		return ctrl.Result{}, errors.Wrap(r.handleError(ctx, cfgOrig, cfg, processMessageWithWarning("set intent failed", warnings), err, true), errUpdateStatus)
	}

	schema := &configv1alpha1.ConfigStatusLastKnownGoodSchema{}
	if err := configv1alpha1.Convert_config_ConfigStatusLastKnownGoodSchema_To_v1alpha1_ConfigStatusLastKnownGoodSchema(internalSchema, schema, nil); err != nil {
		return ctrl.Result{Requeue: true},
			errors.Wrap(r.handleError(ctx, cfgOrig, cfg, processMessageWithWarning("cannot convert schema", warnings), err, true), errUpdateStatus)
	}

	// in the revertive case we can delete the deviation
	if cfg.IsRevertive() {
		if err := r.deleteDeviation(ctx, cfg); err != nil {
			errors.Wrap(r.handleError(ctx, cfgOrig, cfg, processMessageWithWarning("cannot delete deviation", warnings), err, true), errUpdateStatus)
		}
	}

	return ctrl.Result{}, errors.Wrap(r.handleSuccess(ctx, cfgOrig, schema, deviation, warnings), errUpdateStatus)
}

func (r *reconciler) handleSuccess(ctx context.Context, cfg *configv1alpha1.Config, schema *configv1alpha1.ConfigStatusLastKnownGoodSchema, deviation *configv1alpha1.Deviation, msg string) error {
	log := log.FromContext(ctx)
	log.Debug("handleSuccess", "key", cfg.GetNamespacedName(), "status old", cfg.DeepCopy().Status)
	// take a snapshot of the current object
	//patch := client.MergeFrom(cfg.DeepCopy())
	// update status
	newConfig := configv1alpha1.BuildConfig(
		metav1.ObjectMeta{
			Namespace: cfg.Namespace,
			Name:      cfg.Name,
		},
		configv1alpha1.ConfigSpec{},
		configv1alpha1.ConfigStatus{},
	)

	newConfig.SetConditions(cfg.GetCondition(condv1alpha1.ConditionTypeReady))
	newConfig.SetConditions(condv1alpha1.ReadyWithMsg(msg))
	newConfig.Status.LastKnownGoodSchema = schema
	newConfig.Status.AppliedConfig = &cfg.Spec

	if cfg.IsRevertive() || deviation == nil {
		newConfig.Status.DeviationGeneration = nil
	} else {
		newConfig.Status.DeviationGeneration = ptr.To(deviation.GetGeneration())
	}

	deviationChange := cfg.HashDeviationGenerationChanged(deviation)

	if newConfig.GetCondition(condv1alpha1.ConditionTypeReady).Equal(cfg.GetCondition(condv1alpha1.ConditionTypeReady)) &&
		equalSchema(newConfig.Status.LastKnownGoodSchema, cfg.Status.LastKnownGoodSchema) &&
		deviationChange&&
		equalAppliedConfig(newConfig.Status.AppliedConfig, cfg.Status.AppliedConfig) {
		log.Info("handleSuccess -> no change")
		return nil
	}
	log.Info("handleSuccess -> changes")

	if !newConfig.GetCondition(condv1alpha1.ConditionTypeReady).Equal(cfg.GetCondition(condv1alpha1.ConditionTypeReady)) {
		log.Info("handleSuccess -> condition changed")
	}
	if !equalSchema(newConfig.Status.LastKnownGoodSchema, cfg.Status.LastKnownGoodSchema) {
		log.Info("handleSuccess -> LastKnownGoodSchema changed", "schema-a", newConfig.Status.LastKnownGoodSchema, "schema-b", cfg.Status.LastKnownGoodSchema)
	}
	if deviationChange {
		log.Info("handleSuccess -> Deviations changed", "dev-a", newConfig.Status.DeviationGeneration, "dev-b", cfg.Status.DeviationGeneration)
	}
	if !equalAppliedConfig(newConfig.Status.AppliedConfig, cfg.Status.AppliedConfig) {
		log.Info("handleSuccess -> AppliedConfig changed")
	}

	log.Debug("handleSuccess", "key", cfg.GetNamespacedName(), "status new", cfg.Status)

	r.recorder.Eventf(newConfig, corev1.EventTypeNormal, configv1alpha1.ConfigKind, "ready")

	return r.Client.Status().Patch(ctx, newConfig, client.Apply, &client.SubResourcePatchOptions{
		PatchOptions: client.PatchOptions{
			FieldManager: reconcilerName,
			Force:        ptr.To(true),
		},
	})
}

func equalAppliedConfig(a, b *configv1alpha1.ConfigSpec) bool {
	if a == nil && b == nil {
		return true
	}
	if a == nil || b == nil {
		return false
	}
	return equality.Semantic.DeepEqual(*a, *b)
}

func equalSchema(a, b *configv1alpha1.ConfigStatusLastKnownGoodSchema ) bool {
	if a == nil && b == nil {
		return true
	}
	if a == nil || b == nil {
		return false
	}
	return equality.Semantic.DeepEqual(*a, *b)
}

func (r *reconciler) handleError(ctx context.Context, configOrig, config *configv1alpha1.Config, msg string, err error, recoverable bool) error {
	log := log.FromContext(ctx)
	// take a snapshot of the current object
	patch := client.MergeFrom(configOrig)

	if err != nil {
		msg = fmt.Sprintf("%s err %s", msg, err.Error())
	}

	if recoverable {
		config.SetConditions(condv1alpha1.Failed(msg))
	} else {
		newMessage := condv1alpha1.UnrecoverableMessage{
			ResourceVersion: config.GetResourceVersion(),
			Message:         msg,
		}
		newmsg, err := json.Marshal(newMessage)
		if err != nil {
			return err
		}
		config.SetConditions(condv1alpha1.FailedUnRecoverable(string(newmsg)))
	}

	log.Error(msg)
	r.recorder.Eventf(config, corev1.EventTypeWarning, configv1alpha1.ConfigKind, msg)

	return r.Client.Status().Patch(ctx, config, patch, &client.SubResourcePatchOptions{
		PatchOptions: client.PatchOptions{
			FieldManager: reconcilerName,
		},
	})
}

func processMessageWithWarning(msg, warnings string) string {
	if warnings != "" {
		return fmt.Sprintf("%s warnings: %s", msg, warnings)
	}
	return msg
}

// if no deviation is found we return nil
func (r *reconciler) getDeviation(ctx context.Context, config *configv1alpha1.Config) (*configv1alpha1.Deviation, error) {
	deviation := &configv1alpha1.Deviation{}
	if err := r.Client.Get(ctx, config.GetNamespacedName(), deviation); err != nil {
		if resource.IgnoreNotFound(err) == nil {
			return nil, nil
		}
		return nil, err
	}
	return deviation, nil
}


func (r *reconciler) deleteDeviation(ctx context.Context, config *configv1alpha1.Config) error {
	deviation := configv1alpha1.BuildDeviation(metav1.ObjectMeta{Name: config.Name, Namespace: config.Namespace}, nil, nil)
	if err := r.Client.Delete(ctx, deviation); err != nil {
		if resource.IgnoreNotFound(err) != nil {
			return err
		}
	}
	return nil
}

/*
- revertive mode
-> fetch the deviation and see if something needs to happen -> detemrine yes or no
-> if all good delete the deviation

- non revretive mode
-> fecth the deviation and apply it to the device with higher priority
no action on the deviaiton

*/