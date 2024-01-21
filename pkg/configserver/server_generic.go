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

package configserver

import (
	"context"

	"github.com/henderiw/apiserver-builder/pkg/builder/resource"
	"github.com/iptecharch/config-server/pkg/store"
	"github.com/iptecharch/config-server/pkg/store/file"
	"github.com/iptecharch/config-server/pkg/store/memory"
	"go.opentelemetry.io/otel"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/serializer"
	"k8s.io/apiserver/pkg/registry/rest"
	"k8s.io/apiserver/pkg/server/storage"
	"k8s.io/apiserver/pkg/storage/storagebackend"
)

var tracer = otel.Tracer("config-server")

type ResourceProvider interface {
	rest.Storage
	rest.StandardStorage
	GetStore() store.Storer[runtime.Object]
	UpdateStore(context.Context, store.Key, runtime.Object)
	UpdateTarget(context.Context, store.Key, store.Key, runtime.Object) error
}

func createFileStore(ctx context.Context, obj resource.Object, scheme *runtime.Scheme, rootPath string) (store.Storer[runtime.Object], error) {
	gr := obj.GetGroupVersionResource().GroupResource()
	codec, _, err := storage.NewStorageCodec(storage.StorageCodecConfig{
		StorageMediaType:  runtime.ContentTypeJSON,
		StorageSerializer: serializer.NewCodecFactory(scheme),
		StorageVersion:    scheme.PrioritizedVersionsForGroup(obj.GetGroupVersionResource().Group)[0],
		MemoryVersion:     scheme.PrioritizedVersionsForGroup(obj.GetGroupVersionResource().Group)[0],
		Config:            storagebackend.Config{}, // useless fields..
	})
	if err != nil {
		return nil, err
	}
	return file.NewStore[runtime.Object](&file.Config[runtime.Object]{
		GroupResource: gr,
		RootPath:      rootPath,
		Codec:         codec,
		NewFunc:       obj.New,
	})
}

func createMemStore(ctx context.Context) store.Storer[runtime.Object] {
	return memory.NewStore[runtime.Object]()
}
