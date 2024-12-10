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
	"fmt"
	"io"
	"sync"

	"github.com/henderiw/apiserver-store/pkg/storebackend"
	"github.com/henderiw/logger/log"
	"github.com/openconfig/gnmic/pkg/cache"
	"github.com/prometheus/prometheus/prompb"
	"github.com/sdcio/config-server/apis/config"
	invv1alpha1 "github.com/sdcio/config-server/apis/inv/v1alpha1"
	dsclient "github.com/sdcio/config-server/pkg/sdc/dataserver/client"
	"github.com/sdcio/data-server/pkg/utils"
	sdcpb "github.com/sdcio/sdc-protos/sdcpb"
	"google.golang.org/grpc"
	"google.golang.org/protobuf/encoding/prototext"
	"google.golang.org/protobuf/proto"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

type Context struct {
	targetKey        storebackend.Key
	client           client.Client
	dsclient         dsclient.Client
	deviationWatcher *DeviationWatcher
	// Subscription parameters
	collector     *Collector
	subscriptions *Subscriptions

	m            sync.RWMutex
	ready        bool
	datastoreReq *sdcpb.CreateDataStoreRequest
}

func New(targetKey storebackend.Key, client client.Client, dsclient dsclient.Client) *Context {
	subscriptions := NewSubscriptions()
	return &Context{
		targetKey:        targetKey,
		client:           client,
		dsclient:         dsclient,
		deviationWatcher: NewDeviationWatcher(targetKey, client, dsclient),
		collector:        NewCollector(targetKey, subscriptions),
		subscriptions:    subscriptions,
		//cache:            cache.New([]string{}, cache.WithLogging(logging.NewLogrLogger())),
	}
}

func (r *Context) getDatastoreReq() *sdcpb.CreateDataStoreRequest {
	r.m.RLock()
	defer r.m.RUnlock()
	if r.datastoreReq == nil {
		return nil
	}
	clone := proto.Clone(r.datastoreReq)
	return clone.(*sdcpb.CreateDataStoreRequest)
}

func (r *Context) setDatastoreReq(req *sdcpb.CreateDataStoreRequest) {
	r.m.Lock()
	defer r.m.Unlock()
	r.datastoreReq = req
}

func (r *Context) getReady() bool {
	r.m.RLock()
	defer r.m.RUnlock()
	return r.ready
}

func (r *Context) setReady(b bool) {
	r.m.Lock()
	defer r.m.Unlock()
	r.ready = b
}

func (r *Context) GetAddress() string {
	if r.dsclient != nil {
		return r.dsclient.GetAddress()
	}
	return ""
}

func (r *Context) GetSchema() *config.ConfigStatusLastKnownGoodSchema {
	req := r.getDatastoreReq()
	if req == nil {
		return &config.ConfigStatusLastKnownGoodSchema{}
	}
	return &config.ConfigStatusLastKnownGoodSchema{
		Type:    req.Schema.Name,
		Vendor:  req.Schema.Vendor,
		Version: req.Schema.Version,
	}
}

func (r *Context) DeleteDS(ctx context.Context) error {
	log := log.FromContext(ctx).With("targetKey", r.targetKey.String())
	r.setReady(false)
	if r.deviationWatcher != nil {
		r.deviationWatcher.Stop(ctx)
	}
	if r.collector != nil {
		r.collector.Stop(ctx)
	}
	r.setDatastoreReq(nil)
	rsp, err := r.deleteDataStore(ctx, &sdcpb.DeleteDataStoreRequest{Name: r.targetKey.String()})
	if err != nil {
		log.Error("cannot delete datstore in dataserver", "error", err)
		return err
	}
	log.Debug("delete datastore succeeded", "resp", prototext.Format(rsp))
	return nil
}

func (r *Context) CreateDS(ctx context.Context, datastoreReq *sdcpb.CreateDataStoreRequest) error {
	log := log.FromContext(ctx).With("targetKey", r.targetKey.String())
	rsp, err := r.createDataStore(ctx, datastoreReq)
	if err != nil {
		log.Error("cannot create datastore in dataserver", "error", err)
		return err
	}
	r.setDatastoreReq(datastoreReq)
	r.setReady(true)
	if r.deviationWatcher != nil {
		r.deviationWatcher.Start(ctx)
	}
	// The collector is not started when a datastore is created but when a subscription is received.
	log.Info("create datastore succeeded", "resp", prototext.Format(rsp))
	return nil
}

func (r *Context) IsReady() bool {
	if r == nil {
		return false
	}
	if r.client != nil && r.getDatastoreReq() != nil && r.getReady() {
		return true
	}
	return false
}

func (r *Context) SetNotReady(ctx context.Context) {
	r.setReady(false)
	if r.deviationWatcher != nil {
		r.deviationWatcher.Stop(ctx)
	}
	if r.collector != nil {
		r.collector.Stop(ctx)
	}
}

func (r *Context) SetReady(ctx context.Context) {
	log := log.FromContext(ctx)
	r.setReady(true)
	if r.deviationWatcher != nil {
		r.deviationWatcher.Start(ctx)
	}
	if r.subscriptions.HasSubscriptions() && r.collector != nil && !r.collector.IsRunning() {
		if err := r.collector.Start(ctx, r.getDatastoreReq()); err != nil {
			log.Error("setready starting collector failed", "err", err)
		}
	}
}

func (r *Context) deleteDataStore(ctx context.Context, in *sdcpb.DeleteDataStoreRequest, opts ...grpc.CallOption) (*sdcpb.DeleteDataStoreResponse, error) {
	if r == nil {
		return nil, fmt.Errorf("datastore client not initialized")
	}
	if r.dsclient == nil {
		return nil, fmt.Errorf("datastore client not initialized")
	}
	return r.dsclient.DeleteDataStore(ctx, in, opts...)
}

func (r *Context) createDataStore(ctx context.Context, in *sdcpb.CreateDataStoreRequest, opts ...grpc.CallOption) (*sdcpb.CreateDataStoreResponse, error) {
	if r == nil {
		return nil, fmt.Errorf("datastore client not initialized")
	}
	if r.dsclient == nil {
		return nil, fmt.Errorf("datastore client not initialized")
	}
	return r.dsclient.CreateDataStore(ctx, in, opts...)
}

func (r *Context) GetDataStore(ctx context.Context, in *sdcpb.GetDataStoreRequest, opts ...grpc.CallOption) (*sdcpb.GetDataStoreResponse, error) {
	if r == nil {
		return nil, fmt.Errorf("datastore client not initialized")
	}
	if r.dsclient == nil {
		return nil, fmt.Errorf("datastore client not initialized")
	}
	return r.dsclient.GetDataStore(ctx, in, opts...)
}

// useSpec indicates to use the spec as the confifSpec, typically set to true; when set to false it means we are recovering
// the config
func (r *Context) getIntentUpdate(ctx context.Context, key storebackend.Key, config *config.Config, useSpec bool) ([]*sdcpb.Update, error) {
	log := log.FromContext(ctx)
	update := make([]*sdcpb.Update, 0, len(config.Spec.Config))
	configSpec := config.Spec.Config
	if !useSpec && config.Status.AppliedConfig != nil {
		update = make([]*sdcpb.Update, 0, len(config.Status.AppliedConfig.Config))
		configSpec = config.Status.AppliedConfig.Config
	}

	for _, config := range configSpec {
		path, err := utils.ParsePath(config.Path)
		if err != nil {
			return nil, fmt.Errorf("create data failed for target %s, path %s invalid", key.String(), config.Path)
		}
		log.Info("setIntent", "configSpec", string(config.Value.Raw))
		update = append(update, &sdcpb.Update{
			Path: path,
			Value: &sdcpb.TypedValue{
				Value: &sdcpb.TypedValue_JsonVal{
					JsonVal: config.Value.Raw,
				},
			},
		})
	}
	return update, nil
}

func (r *Context) SetIntent(ctx context.Context, key storebackend.Key, config *config.Config, useSpec, dryRun bool) (*config.Config, error) {
	log := log.FromContext(ctx).With("target", key.String(), "intent", getGVKNSN(config))
	if !r.IsReady() {
		return nil, fmt.Errorf("target context not ready")
	}

	update, err := r.getIntentUpdate(ctx, key, config, useSpec)
	if err != nil {
		return config, err
	}
	log.Debug("SetIntent", "update", update)

	rsp, err := r.dsclient.SetIntent(ctx, &sdcpb.SetIntentRequest{
		Name:     key.String(),
		Intent:   getGVKNSN(config),
		Priority: int32(config.Spec.Priority),
		Update:   update,
	})
	if err != nil {
		log.Info("set intent failed", "error", err.Error())
		return config, err
	}
	log.Info("set intent succeeded", "rsp", prototext.Format(rsp))
	return config, nil
}

func (r *Context) DeleteIntent(ctx context.Context, key storebackend.Key, config *config.Config, dryRun bool) (*config.Config, error) {
	log := log.FromContext(ctx).With("target", key.String(), "intent", getGVKNSN(config))
	if !r.IsReady() {
		return nil, fmt.Errorf("target context not ready")
	}

	if config.Status.AppliedConfig == nil {
		log.Info("delete intent was never applied")
		return config, nil
	}

	rsp, err := r.dsclient.SetIntent(ctx, &sdcpb.SetIntentRequest{
		Name:     key.String(),
		Intent:   getGVKNSN(config),
		Priority: int32(config.Spec.Priority),
		Delete:   true,
	})
	if err != nil {
		log.Info("delete intent failed", "error", err.Error())
		return config, err
	}
	log.Info("delete intent succeeded", "rsp", prototext.Format(rsp))
	return config, nil
}

func (r *Context) GetData(ctx context.Context, key storebackend.Key) (*config.RunningConfig, error) {
	log := log.FromContext(ctx).With("target", key.String())
	if !r.IsReady() {
		return nil, fmt.Errorf("target context not ready")
	}
	path, err := utils.ParsePath("/")
	if err != nil {
		return nil, fmt.Errorf("create data failed for target %s, path %s invalid", key.String(), "/")
	}

	stream, err := r.dsclient.GetData(ctx, &sdcpb.GetDataRequest{
		Name:      key.String(),
		Datastore: &sdcpb.DataStore{Type: sdcpb.Type_MAIN},
		Path:      []*sdcpb.Path{path},
		DataType:  sdcpb.DataType_CONFIG,
		Encoding:  sdcpb.Encoding_JSON,
	})
	if err != nil {
		log.Info("get data failed", "error", err.Error())
		return nil, err
	}

	var b []byte
	for {
		rsp, err := stream.Recv()
		if err != nil {
			if err == io.EOF {
				break
			}
			return nil, err
		}

		if len(rsp.GetNotification()) == 1 {
			if len(rsp.GetNotification()[0].GetUpdate()) == 1 {
				b = rsp.GetNotification()[0].GetUpdate()[0].GetValue().GetJsonVal()
			} else {
				log.Info("get data", "updates", len(rsp.GetNotification()[0].GetUpdate()))
			}
		} else {
			log.Info("get data", "notifications", len(rsp.GetNotification()))
		}
	}

	return config.BuildRunningConfig(
		metav1.ObjectMeta{
			Name:      key.Name,
			Namespace: key.Namespace,
		},
		config.RunningConfigSpec{},
		config.RunningConfigStatus{
			Value: runtime.RawExtension{
				Raw: b,
			},
		},
	), nil
}

func (r *Context) DeleteSubscription(ctx context.Context, sub *invv1alpha1.Subscription) error {
	if err := r.subscriptions.DelSubscription(sub); err != nil {
		return err
	}
	// if we have no longer subscriptions we stop the collector
	if r.collector != nil && r.collector.IsRunning() && !r.subscriptions.HasSubscriptions() {
		r.collector.Stop(ctx)
		return nil
	}
	r.updateSubscription()
	return nil
}

func (r *Context) updateSubscription() {
	// other subscriptions exist, update the subscription
	subCh := r.collector.GetUpdateChan()
	subCh <- struct{}{}
}

func (r *Context) UpsertSubscription(ctx context.Context, sub *invv1alpha1.Subscription) error {
	// This should change to the target context and discovery
	if sub.Spec.Protocol != invv1alpha1.Protocol_GNMI {
		return fmt.Errorf("subscriptions only supported using gnmi, got %s", string(sub.Spec.Protocol))
	}
	r.collector.SetPort(sub.Spec.Port)
	if err := r.subscriptions.AddSubscription(sub); err != nil {
		return err
	}
	// above should be changed to subscription profile in the target

	if r.collector != nil && !r.collector.IsRunning() {
		req := r.getDatastoreReq()
		if r.IsReady() && req != nil && req.Target != nil {
			return nil
			//return fmt.Errorf("cannot start subscription on target %s that is not ready", r.targetKey.String())
		}
		if err := r.collector.Start(ctx, req); err != nil {
			return err
		}
		// starting a collector also updates the subscriptions
		return nil
	}

	r.updateSubscription()
	return nil
}

func (r *Context) GetCache() cache.Cache {
	return r.collector.cache
}

func (r *Context) GetPrombLabels() []prompb.Label {
	labels := make([]prompb.Label, 0)
	labels = append(labels, prompb.Label{Name: "target_name", Value: r.targetKey.String()})

	req := r.getDatastoreReq()
	if req != nil {
		labels = append(labels, prompb.Label{Name: "vendor", Value: req.Schema.Vendor})
		labels = append(labels, prompb.Label{Name: "version", Value: req.Schema.Version})
		labels = append(labels, prompb.Label{Name: "address", Value: req.Target.Address})
	}
	return labels
}
