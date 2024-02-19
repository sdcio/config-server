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

package store

import (
	"context"

	"github.com/dgraph-io/badger/v4"
	"github.com/henderiw/apiserver-builder/pkg/builder/resource"
	"github.com/henderiw/apiserver-store/pkg/storebackend"
	"github.com/henderiw/apiserver-store/pkg/storebackend/badgerdb"
	"github.com/henderiw/apiserver-store/pkg/storebackend/file"
	"github.com/henderiw/apiserver-store/pkg/storebackend/memory"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/serializer"
	"k8s.io/apiserver/pkg/server/storage"
	"k8s.io/apiserver/pkg/storage/storagebackend"
)

type StorageType int

const (
	StorageType_Memory StorageType = iota
	StorageType_File
	StorageType_KV
)

type Config struct {
	Prefix string
	Type   StorageType
	DB     *badger.DB
}

func CreateFileStore(ctx context.Context, scheme *runtime.Scheme, obj resource.Object, prefix string) (storebackend.Storer[runtime.Object], error) {
	/*
		scheme := runtime.NewScheme()
		// add the core object to the scheme
		for _, api := range (runtime.SchemeBuilder{
			configv1alpha1.AddToScheme,
		}) {
			if err := api(scheme); err != nil {
				return nil, err
			}
		}
	*/

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
	return file.NewStore[runtime.Object](&storebackend.Config[runtime.Object]{
		GroupResource: gr,
		Prefix:        prefix,
		Codec:         codec,
		NewFunc:       obj.New,
	})
}

func CreateKVStore(ctx context.Context, db *badger.DB, scheme *runtime.Scheme, obj resource.Object) (storebackend.Storer[runtime.Object], error) {
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
	return badgerdb.NewStore[runtime.Object](db, &storebackend.Config[runtime.Object]{
		GroupResource: gr,
		Codec:         codec,
		NewFunc:       obj.New,
	})
}

func CreateMemStore(ctx context.Context) storebackend.Storer[runtime.Object] {
	return memory.NewStore[runtime.Object]()
}
