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

package targetdatastore

/*

import (
	"context"
	"time"

	"github.com/henderiw/apiserver-store/pkg/storebackend"
	"github.com/henderiw/logger/log"
	invv1alpha1 "github.com/sdcio/config-server/apis/inv/v1alpha1"
	"github.com/sdcio/config-server/pkg/target"
	sdcpb "github.com/sdcio/sdc-protos/sdcpb"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

type targetDataStoreWatcher struct {
	cancel      context.CancelFunc
	targetStore storebackend.Storer[*target.Context]
	client.Client
}

func newTargetDataStoreWatcher(client client.Client, targetStore storebackend.Storer[*target.Context]) *targetDataStoreWatcher {
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
	log := log.FromContext(ctx).With("name", "targetDataStoreWatcher")
	for {
		select {
		case <-ctx.Done():
			return
		default:
			// get target list
			targetList := &invv1alpha1.TargetList{}
			if err := r.List(ctx, targetList); err != nil {
				log.Error("cannot get target list", "error", err)
				continue
			}

			for _, target := range targetList.Items {
				target := target
				key := storebackend.KeyFromNSN(types.NamespacedName{Namespace: target.GetNamespace(), Name: target.GetName()})

				tctx, err := r.targetStore.Get(ctx, key)
				if err != nil {
					// not found
					log.Error("k8s target does not have a corresponding k8s ctx", "key", key.String(), "error", err)
					target.SetConditions(invv1alpha1.DatastoreFailed(err.Error()))
					target.SetOverallStatus()
					if err := r.Status().Update(ctx, &target); err != nil {
						log.Error("cannot update target status", "key", key.String(), "error", err)
					}
					log.Info("target status changed true -> false", "key", key.String())
					continue
				}
				if !tctx.IsReady() {
					tctx.SetReady(ctx, false)
					log.Error("k8s target does not have a corresponding dataserver client", "key", key.String(), "error", err)
					target.SetConditions(invv1alpha1.DatastoreFailed("target ctx not ready"))
					target.SetOverallStatus()
					if err := r.Status().Update(ctx, &target); err != nil {
						log.Error("cannot update target status", "key", key.String(), "error", err)
					}
					log.Info("target status changed true -> false", "key", key.String())
					continue
				}
				condition := target.GetCondition(invv1alpha1.ConditionTypeDatastoreReady)
				resp, err := tctx.GetDataStore(ctx, &sdcpb.GetDataStoreRequest{Name: key.String()})
				if err != nil {
					log.Error("cannot get target from the datastore", "key", key.String(), "error", err)
					if condition.Status == metav1.ConditionTrue {
						tctx.SetReady(ctx, false)
						target.SetConditions(invv1alpha1.DatastoreFailed(err.Error()))
						target.SetOverallStatus()
						if err := r.Status().Update(ctx, &target); err != nil {
							log.Error("cannot update target status", "key", key.String(), "error", err)
						}
						log.Info("target status changed true -> false", "key", key.String())
						continue
					}
					continue
				}
				if resp.Target.Status != sdcpb.TargetStatus_CONNECTED {
					// Target is not connected
					tctx.SetReady(ctx, false)
					if condition.Status == metav1.ConditionTrue {
						target.SetConditions(invv1alpha1.DatastoreFailed(resp.Target.StatusDetails))
						target.SetOverallStatus()
						if err := r.Status().Update(ctx, &target); err != nil {
							log.Error("cannot update target status", "key", key.String(), "error", err)
						}
						log.Info("target status changed true -> false", "key", key.String())
						continue
					}
				} else {
					// Target is connected
					if condition.Status == metav1.ConditionFalse {
						tctx.SetReady(ctx, true)
						target.SetConditions(invv1alpha1.DatastoreReady())
						target.SetOverallStatus()
						if err := r.Status().Update(ctx, &target); err != nil {
							log.Error("cannot update target status", "key", key.String(), "error", err)
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
*/