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

	"github.com/henderiw/apiserver-store/pkg/storebackend"
	"github.com/henderiw/logger/log"
	configv1alpha1 "github.com/sdcio/config-server/apis/config/v1alpha1"
	"sigs.k8s.io/controller-runtime/pkg/client"

	//"github.com/sdcio/config-server/pkg/lease"
	dsclient "github.com/sdcio/config-server/pkg/sdc/dataserver/client"
	"github.com/sdcio/data-server/pkg/utils"
	sdcpb "github.com/sdcio/sdc-protos/sdcpb"
	"google.golang.org/grpc"
	"google.golang.org/protobuf/encoding/prototext"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
)

type Context struct {
	targetKey        storebackend.Key
	ready            bool
	dataStore        *sdcpb.CreateDataStoreRequest
	client           client.Client
	dsclient         dsclient.Client
	deviationWatcher *DeviationWatcher
}

func New(targetKey storebackend.Key, client client.Client, dsclient dsclient.Client) *Context {
	return &Context{
		targetKey:        targetKey,
		client:           client,
		dsclient:         dsclient,
		deviationWatcher: NewDeviationWatcher(targetKey, client, dsclient),
	}
}

func (r *Context) GetAddress() string {
	if r.dsclient != nil {
		return r.dsclient.GetAddress()
	}
	return ""
}

func (r *Context) GetSchema() *configv1alpha1.ConfigStatusLastKnownGoodSchema {
	if r.dataStore == nil {
		return &configv1alpha1.ConfigStatusLastKnownGoodSchema{}
	}
	return &configv1alpha1.ConfigStatusLastKnownGoodSchema{
		Type:    r.dataStore.Schema.Name,
		Vendor:  r.dataStore.Schema.Vendor,
		Version: r.dataStore.Schema.Version,
	}
}

func (r *Context) DeleteDS(ctx context.Context) error {
	log := log.FromContext(ctx).With("targetKey", r.targetKey.String())
	r.ready = false
	if r.deviationWatcher != nil {
		r.deviationWatcher.Stop(ctx)
	}
	r.dataStore = nil
	rsp, err := r.deleteDataStore(ctx, &sdcpb.DeleteDataStoreRequest{Name: r.targetKey.String()})
	if err != nil {
		log.Error("cannot delete datstore in dataserver", "error", err)
		return err
	}
	log.Debug("delete datastore succeeded", "resp", prototext.Format(rsp))
	return nil
}

func (r *Context) CreateDS(ctx context.Context, req *sdcpb.CreateDataStoreRequest) error {
	log := log.FromContext(ctx).With("targetKey", r.targetKey.String())
	rsp, err := r.createDataStore(ctx, req)
	if err != nil {
		log.Error("cannot create datastore in dataserver", "error", err)
		return err
	}
	r.dataStore = req
	r.ready = true
	if r.deviationWatcher != nil {
		r.deviationWatcher.Start(ctx)
	}
	log.Info("create datastore succeeded", "resp", prototext.Format(rsp))
	return nil
}

func (r *Context) IsReady() bool {
	if r.client != nil && r.dataStore != nil && r.ready {
		return true
	}
	return false
}

func (r *Context) SetReady(ctx context.Context, ready bool) {
	r.ready = ready
	if r.deviationWatcher != nil {
		if ready {
			r.deviationWatcher.Stop(ctx)
		} else {
			r.deviationWatcher.Start(ctx)
		}
	}
}

func (r *Context) deleteDataStore(ctx context.Context, in *sdcpb.DeleteDataStoreRequest, opts ...grpc.CallOption) (*sdcpb.DeleteDataStoreResponse, error) {
	if r.dsclient == nil {
		return nil, fmt.Errorf("datastore client not initialized")
	}
	return r.dsclient.DeleteDataStore(ctx, in, opts...)
}

func (r *Context) createDataStore(ctx context.Context, in *sdcpb.CreateDataStoreRequest, opts ...grpc.CallOption) (*sdcpb.CreateDataStoreResponse, error) {
	if r.dsclient == nil {
		return nil, fmt.Errorf("datastore client not initialized")
	}
	return r.dsclient.CreateDataStore(ctx, in, opts...)
}

func (r *Context) GetDataStore(ctx context.Context, in *sdcpb.GetDataStoreRequest, opts ...grpc.CallOption) (*sdcpb.GetDataStoreResponse, error) {
	if r.dsclient == nil {
		return nil, fmt.Errorf("datastore client not initialized")
	}
	return r.dsclient.GetDataStore(ctx, in, opts...)
}

// useSpec indicates to use the spec as the confifSpec, typically set to true; when set to false it means we are recovering
// the config
func (r *Context) getIntentUpdate(ctx context.Context, key storebackend.Key, config *configv1alpha1.Config, useSpec bool) ([]*sdcpb.Update, error) {
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

func (r *Context) SetIntent(ctx context.Context, key storebackend.Key, config *configv1alpha1.Config, useSpec, dryRun bool) (*configv1alpha1.Config, error) {
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

func (r *Context) DeleteIntent(ctx context.Context, key storebackend.Key, config *configv1alpha1.Config, dryRun bool) (*configv1alpha1.Config, error) {
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

func (r *Context) GetData(ctx context.Context, key storebackend.Key) (*configv1alpha1.RunningConfig, error) {
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

	return configv1alpha1.BuildRunningConfig(
		metav1.ObjectMeta{
			Name:      key.Name,
			Namespace: key.Namespace,
		},
		configv1alpha1.RunningConfigSpec{},
		configv1alpha1.RunningConfigStatus{
			Value: runtime.RawExtension{
				Raw: b,
			},
		},
	), nil
}
