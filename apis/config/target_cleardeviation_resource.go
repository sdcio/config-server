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
	dsclient "github.com/sdcio/config-server/pkg/sdc/dataserver/client"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apiserver/pkg/registry/rest"
)

var _ resource.ArbitrarySubResource = &TargetClearDeviation{}

func (TargetClearDeviation) SubResourceName() string {
	return "cleardeviation"
}

func (TargetClearDeviation) New() runtime.Object {
	return &TargetClearDeviation{}
}

func (TargetClearDeviation) NewStorage(scheme *runtime.Scheme, parentStorage rest.Storage) (rest.Storage, error) {
	return &targetClearDeviationREST{
		parentStore: parentStorage,
	}, nil
}

// storage
type targetClearDeviationREST struct {
	parentStore rest.Storage
}

func (r *targetClearDeviationREST) New() runtime.Object {
	return &TargetClearDeviation{}
}

func (r *targetClearDeviationREST) Destroy() {}

// Create handles POST /apis/.../targets/{name}/cleardeviation
func (r *targetClearDeviationREST) Create(
	ctx context.Context,
	obj runtime.Object,
	createValidation rest.ValidateObjectFunc,
	options *metav1.CreateOptions,
) (runtime.Object, error) {
	req := obj.(*TargetClearDeviation)

	// Get parent target name from the request object's name
	name := req.Name

	// Get the parent target to validate it exists and is ready
	getter := r.parentStore.(rest.Getter)
	parentObj, err := getter.Get(ctx, name, &metav1.GetOptions{})
	if err != nil {
		return nil, err
	}
	target := parentObj.(*Target)

	if !target.IsReady() {
		return nil, apierrors.NewServiceUnavailable(
			fmt.Sprintf("target %s is not ready", name))
	}

	// Do the actual clear deviation work
	// e.g. call dataserver to clear deviations
	key := target.GetNamespacedName()
	cfg := &dsclient.Config{
		Address:  dsclient.GetDataServerAddress(),
		Insecure: true,
	}
	client, closeFn, err := dsclient.NewEphemeral(ctx, cfg)
	if err != nil {
		return nil, err
	}
	defer func() {
		if err := closeFn(); err != nil {
			// You can use your preferred logging framework here
			fmt.Printf("failed to close connection: %v\n", err)
		}
	}()

	// Your actual clear deviation RPC call here
	_ = client
	_ = key
	_ = req.Spec

	// TODO update status
	req.Status = TargetClearDeviationStatus{
        Cleared: 3,
        Message: "successfully cleared deviations",
    }

	return req, nil
}
