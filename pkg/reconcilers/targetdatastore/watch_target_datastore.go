package targetdatastore

import (
	"context"
	"time"

	invv1alpha1 "github.com/iptecharch/config-server/apis/inv/v1alpha1"
	"github.com/iptecharch/config-server/pkg/store"
	"github.com/iptecharch/config-server/pkg/target"
	sdcpb "github.com/iptecharch/sdc-protos/sdcpb"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/log"
)

type targetDataStoreWatcher struct {
	cancel      context.CancelFunc
	targetStore store.Storer[target.Context]
	client.Client
}

func newTargetDataStoreWatcher(client client.Client, targetStore store.Storer[target.Context]) *targetDataStoreWatcher {
	return &targetDataStoreWatcher{
		Client:      client,
		targetStore: targetStore,
	}
}

func (r *targetDataStoreWatcher) Stop(ctx context.Context) {
	if r.cancel != nil {
		r.cancel()
	}
}

func (r *targetDataStoreWatcher) Start(ctx context.Context) {
	ctx, r.cancel = context.WithCancel(ctx)
	log := log.FromContext(ctx).WithName("targetDataStoreWatcher")
	for {
		select {
		case <-ctx.Done():
			return
		default:
			// get target list
			targetList := &invv1alpha1.TargetList{}
			if err := r.List(ctx, targetList); err != nil {
				log.Error(err, "cannot get target list")
			}

			for _, target := range targetList.Items {
				key := store.KeyFromNSN(types.NamespacedName{Namespace: target.GetNamespace(), Name: target.GetName()})

				tctx, err := r.targetStore.Get(ctx, key)
				if err != nil {
					// not found
					log.Error(err, "k8s target does not have a corresponding k8s ctx", "key", key.String())
					continue
				}
				if tctx.Client == nil {
					log.Error(err, "k8s target does not have a corresponding dataserver client", "key", key.String())
					continue
				}
				resp, err := tctx.Client.GetDataStore(ctx, &sdcpb.GetDataStoreRequest{Name: key.String()})
				if err != nil {
					log.Error(err, "cannot get target from the datastore", "key", key.String())
				}
				condition := target.GetCondition(invv1alpha1.ConditionTypeDSReady)
				if resp.Target.Status != sdcpb.TargetStatus_CONNECTED {
					// Target is not connected
					if condition.Status == metav1.ConditionTrue {
						target.SetConditions(invv1alpha1.Failed(resp.Target.StatusDetails))
						if err := r.Status().Update(ctx, &target); err != nil {
							log.Error(err, "cannot update target status", "key", key.String())
						}
						log.Info("target status changed true -> false", "key", key.String())
						continue
					}
				} else {
					// Target is connected
					if condition.Status == metav1.ConditionFalse {
						target.SetConditions(invv1alpha1.Ready())
						if err := r.Status().Update(ctx, &target); err != nil {
							log.Error(err, "cannot update target status", "key", key.String())
						}
						log.Info("target status changed false -> true", "key", key.String())
						continue
					}
				}
				log.Info("target no change", "key", key.String())
			}
			log.Info("target status check finished, waiting for the next run")
			time.Sleep(1 * time.Minute)
		}
	}
}
