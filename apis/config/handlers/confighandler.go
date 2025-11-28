package handlers

import (
	"context"

	"github.com/sdcio/config-server/apis/config"
	"k8s.io/apimachinery/pkg/api/meta"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
)

type ConfigStoreHandler struct {
	//Handler target.TargetHandler
}

func (r *ConfigStoreHandler) DryRunCreateFn(ctx context.Context, key types.NamespacedName, obj runtime.Object, dryrun bool) (runtime.Object, error) {
	accessor, err := meta.Accessor(obj)
	if err != nil {
		return obj, err
	}
	if _, err := config.GetTargetKey(accessor.GetLabels()); err != nil {
		return obj, err
	}
	cfg := obj.(*config.Config)
	/*
	schema, warnings, err := r.Handler.SetIntent(ctx, targetKey, cfg, config.Deviation{}, dryrun)
	if err != nil {
		msg := fmt.Sprintf("%s err %s", warnings, err.Error())
		cfg.SetConditions(condition.Failed(msg))
		return cfg, err
	}
	cfg.SetConditions(condition.ReadyWithMsg(warnings))
	cfg.Status.LastKnownGoodSchema = schema
	cfg.Status.AppliedConfig = &cfg.Spec
	*/
	return cfg, nil
}
func (r *ConfigStoreHandler) DryRunUpdateFn(ctx context.Context, key types.NamespacedName, obj, old runtime.Object, dryrun bool) (runtime.Object, error) {
	accessor, err := meta.Accessor(obj)
	if err != nil {
		return obj, err
	}
	if _, err := config.GetTargetKey(accessor.GetLabels());  err != nil {
		return obj, err
	}
	cfg := obj.(*config.Config)
	/*
	schema, warnings, err := r.Handler.SetIntent(ctx, targetKey, cfg, config.Deviation{}, dryrun)
	if err != nil {
		msg := fmt.Sprintf("%s err %s", warnings, err.Error())
		cfg.SetConditions(condition.Failed(msg))
		return cfg, err
	}
	cfg.SetConditions(condition.ReadyWithMsg(warnings))
	cfg.Status.LastKnownGoodSchema = schema
	cfg.Status.AppliedConfig = &cfg.Spec
	*/
	return cfg, nil
}
func (r *ConfigStoreHandler) DryRunDeleteFn(ctx context.Context, key types.NamespacedName, obj runtime.Object, dryrun bool) (runtime.Object, error) {
	accessor, err := meta.Accessor(obj)
	if err != nil {
		return obj, err
	}
	if _, err := config.GetTargetKey(accessor.GetLabels()); err != nil {
		return obj, err
	}
	cfg := obj.(*config.Config)
	/*
	warnings, err := r.Handler.DeleteIntent(ctx, targetKey, cfg, dryrun)
	if err != nil {
		msg := fmt.Sprintf("%s err %s", warnings, err.Error())
		cfg.SetConditions(condition.Failed(msg))
		return cfg, err
	}
	*/
	return cfg, nil
}
