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
)

// Storer defines the interface for a generic storage system.
type Storer[T1 any] interface {
	// Retrieve retrieves data for the given key from the storage
	Get(ctx context.Context, key Key) (T1, error)

	// Retrieve retrieves data for the given key from the storage
	List(ctx context.Context, visitorFunc func(context.Context, Key, T1))

	// Create data with the given key in the storage
	Create(ctx context.Context, key Key, data T1) error

	// Update data with the given key in the storage
	Update(ctx context.Context, key Key, data T1) error

	// Delete deletes data and key from the storage
	Delete(ctx context.Context, key Key) error

	// Watch watches change
	//Watch(ctx context.Context) (watch.Interface[T1], error)
}
