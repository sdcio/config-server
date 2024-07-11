package handlers

import (
	"context"

	"github.com/sdcio/config-server/apis/config"
	"github.com/sdcio/config-server/pkg/target"
	"k8s.io/apimachinery/pkg/api/meta"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
)

type ConfigStoreHandler struct {
	Handler *target.TargetHandler
}

func (r *ConfigStoreHandler) DryRunCreateFn(ctx context.Context, key types.NamespacedName, obj runtime.Object, dryrun bool) (runtime.Object, error) {
	accessor, err := meta.Accessor(obj)
	if err != nil {
		return obj, err
	}
	targetKey, err := config.GetTargetKey(accessor.GetLabels())
	if err != nil {
		return obj, err
	}
	cfg := obj.(*config.Config)
	obj, _, err = r.Handler.SetIntent(ctx, targetKey, cfg, true, dryrun)
	if err != nil {
		return obj, err
	}
	return obj, nil
}
func (r *ConfigStoreHandler) DryRunUpdateFn(ctx context.Context, key types.NamespacedName, obj, old runtime.Object, dryrun bool) (runtime.Object, error) {
	accessor, err := meta.Accessor(obj)
	if err != nil {
		return obj, err
	}
	targetKey, err := config.GetTargetKey(accessor.GetLabels())
	if err != nil {
		return obj, err
	}
	cfg := obj.(*config.Config)
	obj, _, err = r.Handler.SetIntent(ctx, targetKey, cfg, true, dryrun)
	if err != nil {
		return obj, err
	}
	return obj, nil
}
func (r *ConfigStoreHandler) DryRunDeleteFn(ctx context.Context, key types.NamespacedName, obj runtime.Object, dryrun bool) (runtime.Object, error) {
	accessor, err := meta.Accessor(obj)
	if err != nil {
		return obj, err
	}
	targetKey, err := config.GetTargetKey(accessor.GetLabels())
	if err != nil {
		return obj, err
	}
	cfg := obj.(*config.Config)
	return r.Handler.DeleteIntent(ctx, targetKey, cfg, dryrun)
}
