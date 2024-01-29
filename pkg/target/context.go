// Copyright 2023 The xxx Authors.
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//    http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package target

import (
	"context"
	"encoding/json"
	"fmt"
	"io"

	"github.com/henderiw/logger/log"
	configv1alpha1 "github.com/iptecharch/config-server/apis/config/v1alpha1"
	"github.com/iptecharch/config-server/pkg/lease"
	dsclient "github.com/iptecharch/config-server/pkg/sdc/dataserver/client"
	"github.com/iptecharch/config-server/pkg/store"
	sdcpb "github.com/iptecharch/sdc-protos/sdcpb"
	"google.golang.org/protobuf/encoding/prototext"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
)

type Context struct {
	Lease     lease.Lease
	Ready     bool
	DataStore *sdcpb.CreateDataStoreRequest
	Client    dsclient.Client
}

func getGVKNSN(obj *configv1alpha1.Config) string {
	if obj.Namespace == "" {
		return fmt.Sprintf("%s.%s.%s", obj.APIVersion, obj.Kind, obj.Name)
	}
	return fmt.Sprintf("%s.%s.%s.%s", obj.APIVersion, obj.Kind, obj.Namespace, obj.Name)
}

func (r *Context) Validate(ctx context.Context, key store.Key) error {
	if r.Client == nil {
		return fmt.Errorf("client for target %s unavailable", key.String())
	}
	if r.DataStore == nil {
		return fmt.Errorf("datastore for target %s does not exist", key.String())
	}
	return nil
}

func (r *Context) getIntentUpdate(ctx context.Context, key store.Key, config *configv1alpha1.Config, spec bool) ([]*sdcpb.Update, error) {
	log := log.FromContext(ctx)
	update := make([]*sdcpb.Update, 0, len(config.Spec.Config))
	configSpec := config.Spec.Config
	if !spec && config.Status.AppliedConfig != nil {
		update = make([]*sdcpb.Update, 0, len(config.Status.AppliedConfig.Config))
		configSpec = config.Status.AppliedConfig.Config
	}

	for _, config := range configSpec {
		path, err := ParsePath(config.Path)
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

func (r *Context) SetIntent(ctx context.Context, key store.Key, config *configv1alpha1.Config, spec bool) error {
	log := log.FromContext(ctx).With("target", key.String(), "intent", getGVKNSN(config))
	if err := r.Validate(ctx, key); err != nil {
		return err
	}

	update, err := r.getIntentUpdate(ctx, key, config, spec)
	if err != nil {
		return err
	}
	log.Info("SetIntent", "update", update)

	rsp, err := r.Client.SetIntent(ctx, &sdcpb.SetIntentRequest{
		Name:     key.String(),
		Intent:   getGVKNSN(config),
		Priority: int32(config.Spec.Priority),
		Update:   update,
	})
	if err != nil {
		log.Info("set intent failed", "error", err.Error())
		return err
	}
	log.Info("set intent succeeded", "rsp", prototext.Format(rsp))
	return nil
}

func (r *Context) DeleteIntent(ctx context.Context, key store.Key, config *configv1alpha1.Config) error {
	log := log.FromContext(ctx).With("target", key.String(), "intent", getGVKNSN(config))
	if err := r.Validate(ctx, key); err != nil {
		return err
	}

	if config.Status.AppliedConfig == nil {
		log.Info("delete intent was never applied")
		return nil
	}

	rsp, err := r.Client.SetIntent(ctx, &sdcpb.SetIntentRequest{
		Name:     key.String(),
		Intent:   getGVKNSN(config),
		Priority: int32(config.Spec.Priority),
		Delete:   true,
	})
	if err != nil {
		log.Info("delete intent failed", "error", err.Error())
		return err
	}
	log.Info("delete intent succeeded", "rsp", prototext.Format(rsp))
	return nil
}

func (r *Context) GetData(ctx context.Context, key store.Key) (*configv1alpha1.RunningConfig, error) {
	log := log.FromContext(ctx).With("target", key.String())
	if err := r.Validate(ctx, key); err != nil {
		return nil, err
	}

	stream, err := r.Client.GetData(ctx, &sdcpb.GetDataRequest{
		Name:      key.String(),
		Datastore: &sdcpb.DataStore{Type: sdcpb.Type_MAIN},
		Path:      []*sdcpb.Path{},
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

		b, err = json.Marshal(rsp)
		if err != nil {
			return nil, err
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
