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
	"time"

	"github.com/henderiw/apiserver-store/pkg/storebackend"
	"github.com/henderiw/logger/log"
	"github.com/pkg/errors"
	configv1alpha1 "github.com/sdcio/config-server/apis/config/v1alpha1"
	dsclient "github.com/sdcio/config-server/pkg/sdc/dataserver/client"
	sdcpb "github.com/sdcio/sdc-protos/sdcpb"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apiserver/pkg/registry/generic/registry"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

const (
	unIntendedConfigDeviation = "__"
)

type DeviationWatcher struct {
	targetKey storebackend.Key
	client    client.Client   // k8 client
	dsclient  dsclient.Client // datastore client
	//targetStore storebackend.Storer[target.Context]
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
	if r.cancel == nil {
		return
	}
	log := log.FromContext(ctx).With("name", "targetDeviationWatcher", "target", r.targetKey.String())
	log.Info("stop")
	r.cancel()
	r.cancel = nil
}

func (r *DeviationWatcher) Start(ctx context.Context) {
	ctx, r.cancel = context.WithCancel(ctx)
	go r.start(ctx)
}

func (r *DeviationWatcher) start(ctx context.Context) {
	log := log.FromContext(ctx).With("name", "targetDeviationWatcher", "target", r.targetKey.String())
	log.Info("start")
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
			time.Sleep(time.Second * 1) //- resilience for server crash
			// retry on failure
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
				intent = unIntendedConfigDeviation
			}
			if _, ok := deviations[intent]; !ok {
				deviations[intent] = make([]*sdcpb.WatchDeviationResponse, 0, 1)
			}
			deviations[intent] = append(deviations[intent], resp)
			continue
		case sdcpb.DeviationEvent_END:
			if !started {
				stream.CloseSend() // to check if this works on the client side to inform the server to stop sending
				stream = nil
				time.Sleep(time.Second * 1) //- resilience for server crash
				continue
			}

			started = false
		case sdcpb.DeviationEvent_CLEAR:
			// manage them in batches going fwd, not implemented right now
			continue
		default:
			log.Info("unexecpted deviation event", "event", resp.Event)
			continue
		}
		for configName, devs := range deviations {
			configDevs := configv1alpha1.ConvertSdcpbDeviations2ConfigDeviations(devs)
			if configName == unIntendedConfigDeviation {
				// TODO add deviation to target or deviation object
				continue
			}
			parts := strings.SplitN(configName, ".", 2)
			if len(parts) != 2 {
				log.Info("unexpected configName", "got", configName)
				continue
			}
	UpdateConfig:
			c := &configv1alpha1.Config{}
			if err := r.client.Get(ctx, types.NamespacedName{Namespace: parts[0], Name: parts[1]}, c); err != nil {
				log.Error("cannot get intent for recieved deviation", "config", configName)
				continue
			}
			c.Status.Deviations = configDevs
			if err := r.client.Update(ctx, c); err != nil {
				log.Error("cannot update intent for recieved deviation", "config", configName)
				// TODO check if resourceversion is bad retry
				if strings.Contains(err.Error(), registry.OptimisticLockErrorMsg) {
					goto UpdateConfig
				}
				continue
			}
		}
	}
}
