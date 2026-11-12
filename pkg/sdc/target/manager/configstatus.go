/*
Copyright 2026 Nokia.

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

package targetmanager

// Config status
//
// Every write of Config status is in this file. Three field managers each
// own a disjoint set of fields and write them through one
// resource.StatusWriter:
//
//	manager                 fields                              written by                        decided by
//	TargetConfigController  Ready                               UpdateConfigsReady                a target stage (reconciler, NotReady), or the outcome (Transact)
//	transactor              ConfigReady, deviationGeneration    writeStatus, setRecoveredConfigs  transaction or recovery outcome
//	transactor-applied      appliedConfig, lastKnownGoodSchema  writeStatus                       successful transaction
//
// The reconciler decides Ready while a target stage blocks; once it
// transacts, Ready follows the outcome and Transact writes it. Every write is
// executed here.
//
// A failed transaction writes ConfigReady only: appliedConfig is not part of
// that apply, so a stale cache can neither remove nor regress it.
//
// Every write is applied: the apiserver computes the delta and drops a status
// update that changes nothing (no resourceVersion, no watch event).
//
// Freshness: an outcome is written unconditionally, it comes from the
// dataserver. Ready derived from the listed (cached) Config is conditional: a
// 409 means the Config changed since it was listed, and the reconcile that
// change triggers decides Ready again. Ready carries the observedGeneration
// of what it is derived from: the outcome, or the Config itself.

import (
	"context"
	"errors"
	"fmt"

	"github.com/henderiw/logger/log"
	condv1alpha1 "github.com/sdcio/config-server/apis/condition/v1alpha1"
	"github.com/sdcio/config-server/apis/config"
	configv1alpha1 "github.com/sdcio/config-server/apis/config/v1alpha1"
	configv1alpha1apply "github.com/sdcio/config-server/pkg/generated/applyconfiguration/config/v1alpha1"
	"github.com/sdcio/config-server/pkg/reconcilers/resource"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

const (
	configReadyFieldManager   = "TargetConfigController"
	configOutcomeFieldManager = "transactor"
	configAppliedFieldManager = "transactor-applied"
)

// configStatus holds one StatusWriter per field manager of the Config status.
type configStatus struct {
	ready   *resource.StatusWriter
	outcome *resource.StatusWriter
	applied *resource.StatusWriter
}

// force: the managers are ours and own disjoint fields; force only matters
// for fields another manager wrote before (e.g. Ready, applied config).
func newConfigStatus(c client.Client) *configStatus {
	return &configStatus{
		ready:   resource.NewStatusWriter(c, configReadyFieldManager, resource.WithForceOwnership()),
		outcome: resource.NewStatusWriter(c, configOutcomeFieldManager, resource.WithForceOwnership()),
		applied: resource.NewStatusWriter(c, configAppliedFieldManager, resource.WithForceOwnership()),
	}
}

// ListTargetConfigs lists the Configs of a target.
func ListTargetConfigs(ctx context.Context, c client.Reader, target *configv1alpha1.Target) (*configv1alpha1.ConfigList, error) {
	cfgList := &configv1alpha1.ConfigList{}
	if err := c.List(ctx, cfgList,
		client.InNamespace(target.GetNamespace()),
		client.MatchingLabels{
			config.TargetNamespaceKey: target.GetNamespace(),
			config.TargetNameKey:      target.GetName(),
		},
	); err != nil {
		return nil, err
	}
	return cfgList, nil
}

// --- Ready ---

// ReadyFn returns the Ready condition for a listed Config, and whether it is
// derived from the listed (cached) Config; such a write is conditional. The
// reconciler passes NotReady while a target stage blocks.
type ReadyFn func(cfg *configv1alpha1.Config) (ready condv1alpha1.Condition, fromList bool)

// NotReady: a target stage blocks, every Config of the target is not ready
// with that reason. The message is bounded by the condition constructor.
func NotReady(reason condv1alpha1.ConditionReason, msg string) ReadyFn {
	return func(cfg *configv1alpha1.Config) (condv1alpha1.Condition, bool) {
		return configv1alpha1.ConfigNotReady(cfg.GetGeneration(), reason, msg), true
	}
}

// readyFromOutcomes: no target stage blocks. A Config the transaction just
// wrote follows that outcome, which is fresh, so the write is unconditional;
// the others follow their last outcome as listed.
func readyFromOutcomes(written outcomes) ReadyFn {
	return func(cfg *configv1alpha1.Config) (condv1alpha1.Condition, bool) {
		if configReady, ok := written.configReady(cfg); ok {
			return configv1alpha1.ReadyFromConfigReady(configReady), false
		}
		return cfg.ReadyFromApplied(), true
	}
}

// UpdateConfigsReady writes the Ready condition of every listed Config, as
// ready decides it. A Config released since it was listed is gone: there is
// nothing to report on. All Configs are attempted, so one failure does not
// stop the others.
func (r *Transactor) UpdateConfigsReady(ctx context.Context, cfgs *configv1alpha1.ConfigList, ready ReadyFn) error {
	var errs error
	for i := range cfgs.Items {
		cfg := &cfgs.Items[i]
		cond, fromList := ready(cfg)
		log.FromContext(ctx).Debug("update config ready", "config", cfg.GetName(),
			"ready", cond.IsTrue(), "reason", cond.Reason, "observedGeneration", cond.ObservedGeneration)
		if err := r.status.ready.Write(ctx, cfg, resource.StatusUpdate{
			Conditions:  []condv1alpha1.Condition{cond},
			Conditional: fromList,
			Build: func(conds []condv1alpha1.Condition, rv string) runtime.ApplyConfiguration {
				return configApply(cfg, rv, configv1alpha1apply.ConfigStatus().WithConditions(conds...))
			},
		}); resource.IgnoreNotFound(err) != nil {
			errs = errors.Join(errs, err)
		}
	}
	if errs != nil {
		return fmt.Errorf("cannot update config ready: %w", errs)
	}
	return nil
}

// --- outcome: transaction and recovery ---

// outcomes is the ConfigReady condition a transaction wrote, per Config.
type outcomes map[string]condv1alpha1.Condition

// configReady returns the outcome written for cfg, if it was transacted.
func (o outcomes) configReady(cfg client.Object) (condv1alpha1.Condition, bool) {
	c, ok := o[config.GetGVKNSN(cfg)]
	return c, ok
}

// txResult is the outcome of the transaction for one config.
type txResult struct {
	configReady condv1alpha1.Condition
	applied     *configv1alpha1.ConfigSpec // success only
	schema      *configv1alpha1.ConfigStatusLastKnownGoodSchema
}

// applyTo projects the result onto an in-memory Config: the fields the
// outcome writes set. Ready is not part of it, see UpdateConfigsReady.
func (t txResult) applyTo(cfg *configv1alpha1.Config) {
	cfg.SetConditions(t.configReady)
	cfg.Status.DeviationGeneration = deviationGenerationFor(t.configReady)
	if t.applied != nil {
		cfg.Status.AppliedConfig = t.applied
		cfg.Status.LastKnownGoodSchema = t.schema
	}
}

// writeStatus writes the result of every transacted config: the outcome, and
// the applied config on success. It returns the outcomes it wrote. All
// configs are attempted, so one failure does not lose the results of the
// others.
func (r *Transactor) writeStatus(ctx context.Context, cfgs *configv1alpha1.ConfigList, results map[string]txResult) (outcomes, error) {
	written := make(outcomes, len(results))
	var errs error
	for i := range cfgs.Items {
		cached := &cfgs.Items[i]
		if cached.GetDeletionTimestamp() != nil {
			continue
		}
		key := config.GetGVKNSN(cached)
		res, ok := results[key]
		if !ok {
			continue
		}
		written[key] = res.configReady
		if res.applied != nil {
			errs = errors.Join(errs, r.status.writeApplied(ctx, cached, res.applied, res.schema))
		}
		if err := r.status.writeOutcome(ctx, cached, res.configReady); err != nil {
			errs = errors.Join(errs, err)
			continue
		}
	}
	return written, errs
}

// setRecoveredConfigs writes the recovery outcome (ConfigReady) on the
// Configs that were part of the recovery (the ones with an applied config),
// from the list the recovery replayed. Ready is left to the targetconfig
// reconciler: the target is not ready yet while recovering.
func (r *Transactor) setRecoveredConfigs(ctx context.Context, cfgs *configv1alpha1.ConfigList, cond func(cfg *configv1alpha1.Config) condv1alpha1.Condition) error {
	var errs error
	for i := range cfgs.Items {
		cfg := &cfgs.Items[i]
		if cfg.Status.AppliedConfig == nil {
			continue
		}
		errs = errors.Join(errs, r.status.writeOutcome(ctx, cfg, cond(cfg)))
	}
	return errs
}

// writeOutcome writes ConfigReady and deviationGeneration. The input is the
// outcome itself, so the apply is unconditional.
func (s *configStatus) writeOutcome(ctx context.Context, cfg *configv1alpha1.Config, configReady condv1alpha1.Condition) error {
	devGen := deviationGenerationFor(configReady)
	return s.outcome.Write(ctx, cfg, resource.StatusUpdate{
		Conditions: []condv1alpha1.Condition{configReady},
		Build: func(conds []condv1alpha1.Condition, rv string) runtime.ApplyConfiguration {
			status := configv1alpha1apply.ConfigStatus().WithConditions(conds...)
			if devGen != nil {
				status.WithDeviationGeneration(*devGen)
			}
			return configApply(cfg, rv, status)
		},
	})
}

// deviationGenerationFor is set (to 0) by an unrecoverable failure and
// cleared by any other outcome.
func deviationGenerationFor(configReady condv1alpha1.Condition) *int64 {
	if configReady.Reason == string(condv1alpha1.ConditionReasonUnrecoverable) {
		return ptr.To[int64](0)
	}
	return nil
}

// writeApplied writes the applied config and the schema it was applied
// with. Only a successful transaction calls it.
func (s *configStatus) writeApplied(ctx context.Context, cfg *configv1alpha1.Config, applied *configv1alpha1.ConfigSpec, schema *configv1alpha1.ConfigStatusLastKnownGoodSchema) error {
	return s.applied.Write(ctx, cfg, resource.StatusUpdate{
		Build: func(_ []condv1alpha1.Condition, rv string) runtime.ApplyConfiguration {
			status := configv1alpha1apply.ConfigStatus().WithAppliedConfig(configSpecToApply(applied))
			if schema != nil {
				status.WithLastKnownGoodSchema(schemaToApply(schema))
			}
			return configApply(cfg, rv, status)
		},
	})
}

func configApply(cfg *configv1alpha1.Config, rv string, status *configv1alpha1apply.ConfigStatusApplyConfiguration) *configv1alpha1apply.ConfigApplyConfiguration {
	ac := configv1alpha1apply.Config(cfg.Name, cfg.Namespace).WithStatus(status)
	if rv != "" {
		ac.WithResourceVersion(rv)
	}
	return ac
}

// --- conversion to apply configurations ---

func schemaToApply(s *configv1alpha1.ConfigStatusLastKnownGoodSchema) *configv1alpha1apply.ConfigStatusLastKnownGoodSchemaApplyConfiguration {
	if s == nil {
		return nil
	}
	a := configv1alpha1apply.ConfigStatusLastKnownGoodSchema()
	if s.Type != "" {
		a.WithType(s.Type)
	}
	if s.Vendor != "" {
		a.WithVendor(s.Vendor)
	}
	if s.Version != "" {
		a.WithVersion(s.Version)
	}
	return a
}

func configSpecToApply(s *configv1alpha1.ConfigSpec) *configv1alpha1apply.ConfigSpecApplyConfiguration {
	if s == nil {
		return nil
	}
	a := configv1alpha1apply.ConfigSpec().
		WithPriority(s.Priority)
	if s.Revertive != nil {
		a.WithRevertive(*s.Revertive)
	}
	if s.Lifecycle != nil {
		a.WithLifecycle(lifecycleToApply(s.Lifecycle))
	}
	for i := range s.Config {
		a.WithConfig(configBlobToApply(&s.Config[i]))
	}
	return a
}

func lifecycleToApply(l *configv1alpha1.Lifecycle) *configv1alpha1apply.LifecycleApplyConfiguration {
	if l == nil {
		return nil
	}
	a := configv1alpha1apply.Lifecycle()
	a.WithDeletionPolicy(l.DeletionPolicy)
	return a
}

func configBlobToApply(b *configv1alpha1.ConfigBlob) *configv1alpha1apply.ConfigBlobApplyConfiguration {
	if b == nil {
		return nil
	}
	a := configv1alpha1apply.ConfigBlob()
	if b.Path != "" {
		a.WithPath(b.Path)
	}
	a.WithValue(b.Value)
	return a
}