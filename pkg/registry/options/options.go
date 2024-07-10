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
	"github.com/dgraph-io/badger/v4"
	"github.com/henderiw/apiserver-store/pkg/storebackend"
	"github.com/sdcio/config-server/pkg/target"
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
	Client      client.Client
	TargetStore storebackend.Storer[*target.Context]
}
