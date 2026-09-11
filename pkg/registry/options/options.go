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

package options

import (
	"context"

	"github.com/dgraph-io/badger/v4"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

type StorageType int

const (
	StorageType_Memory StorageType = iota
	StorageType_File
	StorageType_KV
)

type Options struct {
	// Storage
	Prefix string
	Type   StorageType
	DB     *badger.DB
	// Target
	Client client.Client
	// DisableCreateOnUpdate makes the registry return a clean NotFound when a
	// PATCH (merge-patch) request targets a resource that does not yet exist,
	// instead of the confusing "update failed to construct UpdatedObject" error
	// that the henderiw apiserver-store emits when AllowCreateOnUpdate=true
	// causes it to merge-patch over a nil existing object.
	//
	// Set this to true for TargetSnapshot: its writers (configread.Modify /
	// configread.Delete) already handle NotFound by falling back to Create,
	// so a clean NotFound is exactly what they need.
	DisableCreateOnUpdate bool
	// specific functions
	DryRunCreateFn func(ctx context.Context, key types.NamespacedName, obj runtime.Object, dryrun bool) (runtime.Object, error)
	DryRunUpdateFn func(ctx context.Context, key types.NamespacedName, obj, old runtime.Object, dryrun bool) (runtime.Object, error)
	DryRunDeleteFn func(ctx context.Context, key types.NamespacedName, obj runtime.Object, dryrun bool) (runtime.Object, error)
}
