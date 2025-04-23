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

package target

import (
	"context"
	"strings"
	"sync"
	"time"

	"github.com/henderiw/apiserver-store/pkg/storebackend"
	"github.com/henderiw/logger/log"
	"github.com/pkg/errors"
	configv1alpha1 "github.com/sdcio/config-server/apis/config/v1alpha1"
	dsclient "github.com/sdcio/config-server/pkg/sdc/dataserver/client"
	sdcpb "github.com/sdcio/sdc-protos/sdcpb"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

const (
	unManagedConfigDeviation = "__"
)

type DeviationWatcher struct {
	targetKey storebackend.Key
	client    client.Client   // k8 client
	dsclient  dsclient.Client // datastore client

	m      sync.RWMutex
	cancel context.CancelFunc
}

func NewDeviationWatcher(targetKey storebackend.Key, client client.Client, dsclient dsclient.Client) *DeviationWatcher {
	return &DeviationWatcher{
		targetKey: targetKey,
		client:    client,
		dsclient:  dsclient,
	}
}

func (r *DeviationWatcher) Stop(ctx context.Context) {
	r.m.Lock()
	defer r.m.Unlock()
	if r.cancel == nil {
		return
	}
	log := log.FromContext(ctx).With("name", "targetDeviationWatcher", "target", r.targetKey.String())
	log.Info("stop deviationWatcher")
	r.cancel()
	r.cancel = nil
}

func (r *DeviationWatcher) Start(ctx context.Context) {
	r.Stop(ctx)
	// don't lock before since stop also locks
	r.m.Lock()
	defer r.m.Unlock()
	ctx, r.cancel = context.WithCancel(ctx)
	go r.start(ctx)
}

func (r *DeviationWatcher) start(ctx context.Context) {
	log := log.FromContext(ctx).With("name", "targetDeviationWatcher", "target", r.targetKey.String())
	log.Info("start deviationWatcher")
	var err error
	var stream sdcpb.DataServer_WatchDeviationsClient
	started := false
	// key is intent key,
	deviations := map[string][]*sdcpb.WatchDeviationResponse{}
	for {
		if stream == nil {
			if stream, err = r.dsclient.WatchDeviations(ctx, &sdcpb.WatchDeviationRequest{
				Name: []string{r.targetKey.String()},
			}); err != nil && !errors.Is(err, context.Canceled) {
				if er, ok := status.FromError(err); ok {
					switch er.Code() {
					case codes.Canceled:
						// dont log when context got cancelled
					default:
						log.Error("cannot subscribe", "error", err)
					}
				}
				time.Sleep(time.Second * 1) //- resilience for server crash
				// retry on failure
				continue
			}
		}
		resp, err := stream.Recv()
		if err != nil && !errors.Is(err, context.Canceled) {
			if er, ok := status.FromError(err); ok {
				switch er.Code() {
				case codes.Canceled:
					// dont log when context got cancelled
				default:
					log.Error("cannot recive msg from stream", "error", err)
				}
			}
			// clearing the stream will force the client to resubscribe in the next iteration
			stream.CloseSend() // to check if this works on the client side to inform the server to stop sending
			stream = nil
			time.Sleep(time.Second * 1) //- resilience for server crash, retry on failure

			continue
		}
		switch resp.Event {
		case sdcpb.DeviationEvent_START:
			if started {
				stream.CloseSend() // to check if this works on the client side to inform the server to stop sending
				stream = nil
				time.Sleep(time.Second * 1) //- resilience for server crash
				continue
			}
			deviations = make(map[string][]*sdcpb.WatchDeviationResponse, 0)
			// set the unmanaged devidations to 0 upon start; if no unmanaged deviations are reported
			// this will reset the unmanaged deviations.
			deviations[unManagedConfigDeviation] = make([]*sdcpb.WatchDeviationResponse, 0)
			started = true
		case sdcpb.DeviationEvent_UPDATE:
			if !started {
				stream.CloseSend() // to check if this works on the client side to inform the server to stop sending
				stream = nil
				time.Sleep(time.Second * 1) //- resilience for server crash
				continue
			}
			intent := resp.GetIntent()
			if resp.Reason == sdcpb.DeviationReason_UNHANDLED {
				intent = unManagedConfigDeviation
			}
			if _, ok := deviations[intent]; !ok {
				deviations[intent] = make([]*sdcpb.WatchDeviationResponse, 0)
			}
			deviations[intent] = append(deviations[intent], resp)
		case sdcpb.DeviationEvent_END:
			if !started {
				stream.CloseSend() // to check if this works on the client side to inform the server to stop sending
				stream = nil
				time.Sleep(time.Second * 1) //- resilience for server crash
				continue
			}
			started = false
			r.processDeviations(ctx, deviations) // Process & clear deviations
			deviations = make(map[string][]*sdcpb.WatchDeviationResponse, 0)
		case sdcpb.DeviationEvent_CLEAR:
			// manage them in batches going fwd, not implemented right now
			deviations = make(map[string][]*sdcpb.WatchDeviationResponse, 0)
		default:
			log.Info("unexecpted deviation event", "event", resp.Event)
		}
		resp = nil
	}
}

func (r *DeviationWatcher) processDeviations(ctx context.Context, deviations map[string][]*sdcpb.WatchDeviationResponse) {
	log := log.FromContext(ctx)
	log.Info("process deviations")
	for configName, devs := range deviations {
		if configName == "default" {
			continue
		}
		configDevs := configv1alpha1.ConvertSdcpbDeviations2ConfigDeviations(devs)

		nsn := r.targetKey.NamespacedName
		var cfg configv1alpha1.ConfigDeviations
		if configName == unManagedConfigDeviation {
			cfg = &configv1alpha1.UnManagedConfig{}
			log.Info("unmanaged deviations", "devs", len(configDevs))
			r.processConfigDeviationsForUnManagedConfig(ctx, nsn, cfg, configDevs)
		} else {
			cfg = &configv1alpha1.Config{}
			parts := strings.SplitN(configName, ".", 2)
			nsn = types.NamespacedName{
				Namespace: parts[0],
				Name:      parts[1],
			}
			if len(parts) != 2 {
				log.Info("unexpected configName", "got", configName)
				return
			}
			log.Info("managed deviations", "devs", len(configDevs))
			r.processConfigDeviationsForConfig(ctx, nsn, cfg, configDevs)
		}

	}
}

func (r *DeviationWatcher) processConfigDeviationsForConfig(ctx context.Context, nsn types.NamespacedName, cfg configv1alpha1.ConfigDeviations, devs []configv1alpha1.Deviation) {
	log := log.FromContext(ctx)
	/*
		if err := r.client.Get(ctx, nsn, cfg); err != nil {
			log.Error("cannot get intent for recieved deviation", "config", nsn)
			return
		}
	*/
	newConfig := configv1alpha1.BuildConfig(
		metav1.ObjectMeta{
			Namespace: nsn.Namespace,
			Name:      nsn.Name,
		},
		configv1alpha1.ConfigSpec{},
		configv1alpha1.ConfigStatus{},
	)

	//patch := client.MergeFrom(cfg.DeepObjectCopy())
	newConfig.SetDeviations(devs)
	if err := r.client.Status().Patch(ctx, newConfig, client.Apply, &client.SubResourcePatchOptions{
		PatchOptions: client.PatchOptions{
			FieldManager: "ConfigController",
		},
	}); err != nil {
		log.Error("cannot update intent for recieved deviation", "config", nsn)
	}
}

func (r *DeviationWatcher) processConfigDeviationsForUnManagedConfig(ctx context.Context, nsn types.NamespacedName, cfg configv1alpha1.ConfigDeviations, devs []configv1alpha1.Deviation) {
	log := log.FromContext(ctx)
	/*
	if err := r.client.Get(ctx, nsn, cfg); err != nil {
		log.Error("cannot get intent for recieved deviation", "config", nsn)
		return
	}
		*/

	newUnmanagedConfig := configv1alpha1.BuildUnManagedConfig(
		metav1.ObjectMeta{
			Namespace: nsn.Namespace,
			Name:      nsn.Name,
		},
		configv1alpha1.UnManagedConfigSpec{},
		configv1alpha1.UnManagedConfigStatus{},
	)

	//patch := client.MergeFrom(cfg.DeepObjectCopy())
	newUnmanagedConfig.SetDeviations(devs)
	if err := r.client.Status().Patch(ctx, newUnmanagedConfig, client.Apply, &client.SubResourcePatchOptions{
		PatchOptions: client.PatchOptions{
			FieldManager: "deviationManager",
			Force:        ptr.To(true),
		},
	}); err != nil {
		log.Error("cannot update intent for recieved deviation", "config", nsn)
	}
}
