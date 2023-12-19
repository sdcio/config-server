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
	"fmt"

	configv1alpha1 "github.com/iptecharch/config-server/apis/config/v1alpha1"
	dsclient "github.com/iptecharch/config-server/pkg/sdc/dataserver/client"
	"github.com/iptecharch/config-server/pkg/store"
	"github.com/henderiw/logger/log"
	sdcpb "github.com/iptecharch/sdc-protos/sdcpb"
	"google.golang.org/protobuf/encoding/prototext"
)

type Context struct {
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

func (r *Context) SetIntent(ctx context.Context, key store.Key, obj *configv1alpha1.Config) error {
	log := log.FromContext(ctx).With("target", key.String(), "intent", getGVKNSN(obj))
	if err := r.Validate(ctx, key); err != nil {
		return err
	}
	update := make([]*sdcpb.Update, 0, len(obj.Spec.Config))
	for _, config := range obj.Spec.Config {
		path, err := ParsePath(config.Path)
		if err != nil {
			return fmt.Errorf("create data failed for target %s, path %s invalid", key.String(), config.Path)
		}
		update = append(update, &sdcpb.Update{
			Path: path,
			Value: &sdcpb.TypedValue{
				Value: &sdcpb.TypedValue_JsonVal{
					JsonVal: config.Value.Raw,
				},
			},
		})
	}

	rsp, err := r.Client.SetIntent(ctx, &sdcpb.SetIntentRequest{
		Name:     key.String(),
		Intent:   getGVKNSN(obj),
		Priority: int32(obj.Spec.Priority),
		Update:   update,
	})
	if err != nil {
		log.Info("set intent failed", "error", err.Error())
		return err
	}
	log.Info("set intent succeeded", "rsp", prototext.Format(rsp))
	return nil
}

func (r *Context) DeleteIntent(ctx context.Context, key store.Key, obj *configv1alpha1.Config) error {
	log := log.FromContext(ctx).With("target", key.String(), "intent", getGVKNSN(obj))
	if err := r.Validate(ctx, key); err != nil {
		return err
	}

	rsp, err := r.Client.SetIntent(ctx, &sdcpb.SetIntentRequest{
		Name:     key.String(),
		Intent:   getGVKNSN(obj),
		Priority: int32(obj.Spec.Priority),
		Delete:   true,
	})
	if err != nil {
		log.Info("delete intent failed", "error", err.Error())
		return err
	}
	log.Info("delete intent succeeded", "rsp", prototext.Format(rsp))
	return nil
}

/*
func (r *Context) Create(ctx context.Context, key store.Key, obj *configv1alpha1.Config) error {
	if err := r.Validate(ctx, key); err != nil {
		return err
	}
	if err := r.CreateCandidate(ctx, key, obj); err != nil {
		return err
	}
	if err := r.CreateData(ctx, key, obj); err != nil {
		return err
	}
	if err := r.Commit(ctx, key, obj); err != nil {
		return err
	}
	return nil
}

// TODO -> get old and new obj
func (r *Context) Update(ctx context.Context, key store.Key, oldObj *configv1alpha1.Config, newObj *configv1alpha1.Config) error {
	if err := r.Validate(ctx, key); err != nil {
		return err
	}
	// TO BE UPDATE with the new intent api
	if err := r.CreateCandidate(ctx, key, newObj); err != nil {
		return err
	}
	if err := r.CreateData(ctx, key, newObj); err != nil {
		return err
	}
	if err := r.Commit(ctx, key, newObj); err != nil {
		return err
	}
	return nil
}

func (r *Context) Delete(ctx context.Context, key store.Key, obj *configv1alpha1.Config) error {
	if err := r.Validate(ctx, key); err != nil {
		return err
	}
	if err := r.CreateCandidate(ctx, key, obj); err != nil {
		return err
	}
	if err := r.DeleteData(ctx, key, obj); err != nil {
		return err
	}
	if err := r.Commit(ctx, key, obj); err != nil {
		return err
	}
	return nil
}

func (r *Context) CreateCandidate(ctx context.Context, key store.Key, obj *configv1alpha1.Config) error {
	log := log.FromContext(ctx)
	rsp, err := r.Client.CreateDataStore(ctx, &sdcpb.CreateDataStoreRequest{
		Name:      key.String(),
		Datastore: getCandidateDatastore(obj),
	})
	if err != nil {
		if !strings.Contains(err.Error(), "already exists") {
			return fmt.Errorf("create candidate failed for target %s", key.String())
		}
		log.Info("create candidate ds already exists", "rsp", prototext.Format(rsp), "error", err.Error())
	} else {
		log.Info("create candidate ds succeeded", "rsp", prototext.Format(rsp))
	}
	return nil
}

func (r *Context) DeleteCandidate(ctx context.Context, key store.Key, obj *configv1alpha1.Config) error {
	log := log.FromContext(ctx)
	rsp, err := r.Client.DeleteDataStore(ctx, &sdcpb.DeleteDataStoreRequest{
		Name:      key.String(),
		Datastore: getCandidateDatastore(obj),
	})
	if err != nil {
		if !strings.Contains(err.Error(), "not found") {
			return fmt.Errorf("create candidate failed for target %s", key.String())
		}
		log.Info("create candidate ds not found", "rsp", prototext.Format(rsp), "error", err.Error())
	} else {
		log.Info("create candidate ds succeeded", "rsp", prototext.Format(rsp))
	}
	return nil
}

func (r *Context) CreateData(ctx context.Context, key store.Key, obj *configv1alpha1.Config) error {
	log := log.FromContext(ctx)

	req := &sdcpb.SetDataRequest{
		Name:      key.String(),
		Datastore: getCandidateDatastore(obj),
		Update:    []*sdcpb.Update{},
	}

	for _, config := range obj.Spec.Config {
		path, err := ParsePath(config.Path)
		if err != nil {
			return fmt.Errorf("create data failed for target %s, path %s invalid", key.String(), config.Path)
		}
		req.Update = append(req.Update, &sdcpb.Update{
			Path: path,
			Value: &sdcpb.TypedValue{
				Value: &sdcpb.TypedValue_JsonVal{
					JsonVal: config.Value.Raw,
				},
			},
		})
	}

	rsp, err := r.Client.SetData(ctx, req)
	if err != nil {
		return fmt.Errorf("set data failed for target %s, err: %s", key.String(), err.Error())
	}
	log.Info("create set succeeded", "rsp", prototext.Format(rsp))

	return nil
}

// TODO rework after the intent api changed
func (r *Context) UpdateData(ctx context.Context, key store.Key, oldObj *configv1alpha1.Config, newObj *configv1alpha1.Config) error {
	log := log.FromContext(ctx)

	req := &sdcpb.SetDataRequest{
		Name:      key.String(),
		Datastore: getCandidateDatastore(newObj),
		Update:    []*sdcpb.Update{},
	}

	for _, config := range oldObj.Spec.Config {
		path, err := ParsePath(config.Path)
		if err != nil {
			return fmt.Errorf("create data failed for target %s, path %s invalid", key.String(), config.Path)
		}
		req.Update = append(req.Update, &sdcpb.Update{
			Path: path,
			Value: &sdcpb.TypedValue{
				Value: &sdcpb.TypedValue_JsonVal{
					JsonVal: config.Value.Raw,
				},
			},
		})
	}

	rsp, err := r.Client.SetData(ctx, req)
	if err != nil {
		return fmt.Errorf("set data failed for target %s, err: %s", key.String(), err.Error())
	}
	log.Info("create set succeeded", "rsp", prototext.Format(rsp))

	return nil
}

// TODO delete
// TODO rework after the intent api changed
func (r *Context) DeleteData(ctx context.Context, key store.Key, obj *configv1alpha1.Config) error {
	log := log.FromContext(ctx)

	req := &sdcpb.SetDataRequest{
		Name:      key.String(),
		Datastore: getCandidateDatastore(obj),
		Update:    []*sdcpb.Update{},
	}

	// TODO update

	rsp, err := r.Client.SetData(ctx, req)
	if err != nil {
		return fmt.Errorf("set data failed for target %s, err: %s", key.String(), err.Error())
	}
	log.Info("create set succeeded", "rsp", prototext.Format(rsp))

	return nil
}

func (r *Context) Commit(ctx context.Context, key store.Key, obj *configv1alpha1.Config) error {
	log := log.FromContext(ctx).With("key", key.String())
	rsp, err := r.Client.Commit(ctx, &sdcpb.CommitRequest{
		Name:      key.String(),
		Datastore: getCandidateDatastore(obj),
		Rebase:    true,
		Stay:      false,
	})
	if err != nil {
		if err := r.DeleteCandidate(ctx, key, obj); err != nil {
			return fmt.Errorf("commit failed for target %s, and subsequent candidate delete err: %s", key.String(), err.Error())
		}
		return fmt.Errorf("commit failed for target %s, err: %s", key.String(), err.Error())
	}
	log.Info("commit succeeded", "rsp", prototext.Format(rsp))
	return nil
}
*/
