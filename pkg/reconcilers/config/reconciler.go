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
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/tools/record"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/event"
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
		r.handleError(ctx, cfg, "cannot convert config", err, true)
		return ctrl.Result{}, errors.Wrap(r.Status().Update(ctx, cfg), errUpdateStatus)
	}

	targetKey, err := config.GetTargetKey(cfg.GetLabels())
	if err != nil {
		// this should never fail since validation covered this
		return ctrl.Result{}, nil
	}

	if !cfg.GetDeletionTimestamp().IsZero() {
		if _, err := r.targetHandler.DeleteIntent(ctx, targetKey, internalcfg, false); err != nil {
			if errors.Is(err, target.TargetLookupErr) {
				log.Warn("deleted config, target unavailable", "config", req, "err", err)
				// Since the target is not available we delete the resource
				// The target config might not be deleted
				if err := r.finalizer.RemoveFinalizer(ctx, cfg); err != nil {
					return ctrl.Result{Requeue: true},
						errors.Wrap(r.handleError(ctx, cfgOrig, "cannot delete finalizer", err, true), errUpdateStatus)
				}
			}
			// all grpc errors except resource exhausted will not retry
			// and a human need to intervene
			var txErr *target.TransactionError
			if errors.As(err, &txErr) && txErr.Recoverable {
				// Retry logic for recoverable errors
				return ctrl.Result{Requeue: true, RequeueAfter: 5 * time.Second},
					errors.Wrap(r.handleError(ctx, cfgOrig, "set intent failed (recoverable)", err, true), errUpdateStatus)
			}

			return ctrl.Result{}, errors.Wrap(r.handleError(ctx, cfgOrig, "delete intent failed", err, false), errUpdateStatus)

		}
		if err := r.finalizer.RemoveFinalizer(ctx, cfg); err != nil {
			return ctrl.Result{Requeue: true},
				errors.Wrap(r.handleError(ctx, cfgOrig, "cannot delete finalizer", err, true), errUpdateStatus)
		}
		return ctrl.Result{}, nil
	}

	if err := r.finalizer.AddFinalizer(ctx, cfg); err != nil {
		return ctrl.Result{Requeue: true},
			errors.Wrap(r.handleError(ctx, cfgOrig, "cannot add finalizer", err, true), errUpdateStatus)
	}

	if _, _, err := r.targetHandler.GetTargetContext(ctx, targetKey); err != nil {
		cfg.Status.LastKnownGoodSchema = nil
		cfg.Status.Deviations = []configv1alpha1.Deviation{} // reset deviations
		cfg.Status.AppliedConfig = &cfg.Spec
		return ctrl.Result{}, errors.Wrap(r.handleError(ctx, cfgOrig, "target error", err, true), errUpdateStatus)
	}

	// check if we have to reapply the config
	// if condition is false -> reapply the config
	// if the applied Config is not set -> reapply the config
	// if the applied Config is different than the spec -> reapply the config
	// if the deviation is having the reason xx -> reapply the config
	if cfg.IsConditionReady() &&
		cfg.Status.AppliedConfig != nil &&
		cfg.Spec.GetShaSum(ctx) == cfg.Status.AppliedConfig.GetShaSum(ctx) &&
		!cfg.Status.HasNotAppliedDeviation() {
		return ctrl.Result{}, nil
	}

	// Check if we got an unrecoverable error and if the resourceVersion has not changed we can stop here
	if !cfg.IsRecoverable() {
		return ctrl.Result{}, nil
	}

	internalSchema, warnings, err := r.targetHandler.SetIntent(ctx, targetKey, internalcfg, false)
	if err != nil {
		// TODO distinguish between recoeverable and non recoverable
		var txErr *target.TransactionError
		if errors.As(err, &txErr) && txErr.Recoverable {
			// Retry logic for recoverable errors
			return ctrl.Result{Requeue: true, RequeueAfter: 5 * time.Second},
				errors.Wrap(r.handleError(ctx, cfgOrig, processMessageWithWarning("set intent failed (recoverable)", warnings), err, true), errUpdateStatus)
		}
		return ctrl.Result{}, errors.Wrap(r.handleError(ctx, cfgOrig, processMessageWithWarning("set intent failed", warnings), err, true), errUpdateStatus)
	}

	schema := &configv1alpha1.ConfigStatusLastKnownGoodSchema{}
	if err := configv1alpha1.Convert_config_ConfigStatusLastKnownGoodSchema_To_v1alpha1_ConfigStatusLastKnownGoodSchema(internalSchema, schema, nil); err != nil {
		return ctrl.Result{Requeue: true},
			errors.Wrap(r.handleError(ctx, cfgOrig, processMessageWithWarning("cannot convert schema", warnings), err, true), errUpdateStatus)
	}

	return ctrl.Result{}, errors.Wrap(r.handleSuccess(ctx, cfgOrig, schema, warnings), errUpdateStatus)
}

func (r *reconciler) handleSuccess(ctx context.Context, cfg *configv1alpha1.Config, schema *configv1alpha1.ConfigStatusLastKnownGoodSchema, msg string) error {
	log := log.FromContext(ctx)
	log.Debug("handleSuccess", "key", cfg.GetNamespacedName(), "status old", cfg.DeepCopy().Status)
	// take a snapshot of the current object
	patch := client.MergeFrom(cfg.DeepCopy())
	// update status
	cfg.SetConditions(condv1alpha1.ReadyWithMsg(msg))
	cfg.Status.LastKnownGoodSchema = schema
	cfg.Status.Deviations = []configv1alpha1.Deviation{} // reset deviations
	cfg.Status.AppliedConfig = &cfg.Spec
	r.recorder.Eventf(cfg, corev1.EventTypeNormal, configv1alpha1.ConfigKind, "ready")

	log.Debug("handleSuccess", "key", cfg.GetNamespacedName(), "status new", cfg.Status)

	return r.Client.Status().Patch(ctx, cfg, patch, &client.SubResourcePatchOptions{
		PatchOptions: client.PatchOptions{
			FieldManager: reconcilerName,
		},
	})
}

func (r *reconciler) handleError(ctx context.Context, cfg *configv1alpha1.Config, msg string, err error, recoverable bool) error {
	log := log.FromContext(ctx)
	// take a snapshot of the current object
	patch := client.MergeFrom(cfg.DeepCopy())

	if err != nil {
		msg = fmt.Sprintf("%s err %s", msg, err.Error())
	}

	if recoverable {
		cfg.SetConditions(condv1alpha1.Failed(msg))
	} else {
		newMessage := condv1alpha1.UnrecoverableMessage{
			ResourceVersion: cfg.GetResourceVersion(),
			Message:         msg,
		}
		newmsg, err := json.Marshal(newMessage)
		if err != nil {
			return err
		}
		cfg.SetConditions(condv1alpha1.FailedUnRecoverable(string(newmsg)))
	}
	log.Error(msg)
	r.recorder.Eventf(cfg, corev1.EventTypeWarning, configv1alpha1.ConfigKind, msg)

	return r.Client.Status().Patch(ctx, cfg, patch, &client.SubResourcePatchOptions{
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
