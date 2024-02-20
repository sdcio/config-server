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
	"time"

	"github.com/henderiw/apiserver-store/pkg/storebackend"
	"github.com/henderiw/logger/log"
	"github.com/pkg/errors"
	dsclient "github.com/sdcio/config-server/pkg/sdc/dataserver/client"
	sdcpb "github.com/sdcio/sdc-protos/sdcpb"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

const (
	unconfiguredDeviation = "__"
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
	log := log.FromContext(ctx).With("name", "targetDeviationWatcher", "target", r.targetKey.String())
	log.Info("stop")
	if r.cancel != nil {
		r.cancel()
	}
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
	var stream sdcpb.DataServer_SubscribeClient
	started := false
	// key is intent key,
	deviations := map[string][]string{}
	for {
		if stream == nil {
			if stream, err = r.dsclient.Subscribe(ctx, &sdcpb.SubscribeRequest{}); err != nil && !errors.Is(err, context.Canceled) {
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
			stream = nil
			time.Sleep(time.Second * 1) //- resilience for server crash
			// retry on failure
			continue
		}
		// switch event type
		// start -> set started
		// update -> check if started is true; if not set stream = nil; if yes update the deviation map (unconfigured deviations have a special key)
		// end -> check is started is true; if not set stream = nil; if yes flush the cache and update the related CR(s); clear started
	}
}
