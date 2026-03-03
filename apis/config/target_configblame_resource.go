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

package config

import (
	"context"
	"fmt"

	"github.com/henderiw/apiserver-builder/pkg/builder/resource"
	"github.com/henderiw/apiserver-store/pkg/storebackend"
	"github.com/sdcio/config-server/apis/condition"
	dsclient "github.com/sdcio/config-server/pkg/sdc/dataserver/client"
	"github.com/sdcio/sdc-protos/sdcpb"
	"google.golang.org/protobuf/encoding/protojson"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apiserver/pkg/registry/rest"
)

var _ resource.ArbitrarySubResource = &TargetConfigBlame{}

func (TargetConfigBlame) SubResourceName() string {
	return "configblame"
}

func (TargetConfigBlame) New() runtime.Object {
	return &TargetConfigBlame{} // returns parent type — GET returns the full Target
}

func (TargetConfigBlame) NewStorage(scheme *runtime.Scheme, parentStorage rest.Storage) (rest.Storage, error) {
	return &targetConfigBlameREST{
		parentStore: parentStorage,
	}, nil
}

// targetBlameREST implements rest.Storage + rest.Getter
type targetConfigBlameREST struct {
	parentStore rest.Storage
}

func (r *targetConfigBlameREST) New() runtime.Object {
	return &TargetConfigBlame{}
}

func (r *targetConfigBlameREST) Destroy() {}

func (r *targetConfigBlameREST) Get(ctx context.Context, name string, options *metav1.GetOptions) (runtime.Object, error) {
	// Get the parent Target from the parent store
	getter := r.parentStore.(rest.Getter)

	obj, err := getter.Get(ctx, name, options)
	if err != nil {
		return nil, err
	}
	target := obj.(*Target)

	if !target.IsReady() {
		return nil, apierrors.NewServiceUnavailable(
			fmt.Sprintf("target %s is not ready: %s", name, target.GetCondition(condition.ConditionTypeReady).Message))
	}

	cfg := &dsclient.Config{
		Address:  dsclient.GetDataServerAddress(),
		Insecure: true,
	}

	dsclient, closeFn, err := dsclient.NewEphemeral(ctx, cfg)
	if err != nil {
		return nil, err
	}
	defer func() {
		if err := closeFn(); err != nil {
			// You can use your preferred logging framework here
			fmt.Printf("failed to close connection: %v\n", err)
		}
	}()

	// check if the schema exists; this is == nil check; in case of err it does not exist
	key := target.GetNamespacedName()
	rsp, err := dsclient.BlameConfig(ctx, &sdcpb.BlameConfigRequest{
		DatastoreName:   storebackend.KeyFromNSN(key).String(),
		IncludeDefaults: true,
	})
	if err != nil {
		return nil, err
	}

	if rsp == nil || rsp.ConfigTree == nil {
		return &TargetConfigBlame{
			ObjectMeta: metav1.ObjectMeta{
				Name:      target.Name,
				Namespace: target.Namespace,
			},
			Value: runtime.RawExtension{Raw: nil},
		}, nil
	}

	json, err := protojson.Marshal(rsp.ConfigTree)
	if err != nil {
		return nil, err
	}

	return &TargetConfigBlame{
		ObjectMeta: metav1.ObjectMeta{
			Name:      target.Name,
			Namespace: target.Namespace,
		},
		Value: runtime.RawExtension{Raw: json},
	}, nil
}
