package handlers

import (
	"context"
	"fmt"

	"github.com/henderiw/apiserver-store/pkg/storebackend"
	"github.com/sdcio/config-server/apis/config"
	configv1alpha1 "github.com/sdcio/config-server/apis/config/v1alpha1"
	targetmanager "github.com/sdcio/config-server/pkg/sdc/target/manager"
	sdcpb "github.com/sdcio/sdc-protos/sdcpb"
	"k8s.io/apimachinery/pkg/api/meta"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

type ConfigStoreHandler struct {
	Client client.Client
}

func (r *ConfigStoreHandler) DryRunCreateFn(ctx context.Context, key types.NamespacedName, obj runtime.Object, dryrun bool) (runtime.Object, error) {
	c, target, err := r.prepareConfigAndTarget(ctx, key, obj)
	if err != nil {
		return obj, err
	}

	updates, err := targetmanager.GetIntentUpdate(ctx, storebackend.KeyFromNSN(key), c, true)
	if err != nil {
		return nil, err
	}

	intents := []*sdcpb.TransactionIntent{
		{
			Intent:   targetmanager.GetGVKNSN(c),
			Priority: int32(c.Spec.Priority),
			Update:   updates,
		},
	}

	return targetmanager.RunDryRunTransaction(ctx, key, c, target, intents, dryrun)
}
func (r *ConfigStoreHandler) DryRunUpdateFn(ctx context.Context, key types.NamespacedName, obj, old runtime.Object, dryrun bool) (runtime.Object, error) {
	c, target, err := r.prepareConfigAndTarget(ctx, key, obj)
	if err != nil {
		return obj, err
	}

	updates, err := targetmanager.GetIntentUpdate(ctx, storebackend.KeyFromNSN(key), c, true)
	if err != nil {
		return nil, err
	}

	intents := []*sdcpb.TransactionIntent{
		{
			Intent:   targetmanager.GetGVKNSN(c),
			Priority: int32(c.Spec.Priority),
			Update:   updates,
		},
	}

	return targetmanager.RunDryRunTransaction(ctx, key, c, target, intents, dryrun)
}
func (r *ConfigStoreHandler) DryRunDeleteFn(ctx context.Context, key types.NamespacedName, obj runtime.Object, dryrun bool) (runtime.Object, error) {
	c, target, err := r.prepareConfigAndTarget(ctx, key, obj)
	if err != nil {
		return obj, err
	}

	intents := []*sdcpb.TransactionIntent{
		{
			Intent: targetmanager.GetGVKNSN(c),
			Delete: true,
		},
	}

	return targetmanager.RunDryRunTransaction(ctx, key, c, target, intents, dryrun)
}

// prepareConfigAndTarget validates labels, casts the object, fetches the Target
// and ensures it's ready.
func (r *ConfigStoreHandler) prepareConfigAndTarget(
	ctx context.Context,
	key types.NamespacedName,
	obj runtime.Object,
) (*config.Config, *configv1alpha1.Target, error) {
	accessor, err := meta.Accessor(obj)
	if err != nil {
		return nil, nil, err
	}

	if _, err := config.GetTargetKey(accessor.GetLabels()); err != nil {
		return nil, nil, err
	}

	c, ok := obj.(*config.Config)
	if !ok {
		return nil, nil, fmt.Errorf("expected *config.Config, got %T", obj)
	}

	target := &configv1alpha1.Target{}
	if err := r.Client.Get(ctx, key, target); err != nil {
		return nil, nil, err
	}

	if !target.IsReady() {
		return nil, nil, fmt.Errorf("target not ready %s", key)
	}

	return c, target, nil
}
