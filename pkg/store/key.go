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

package store

import (
	"fmt"

	"k8s.io/apimachinery/pkg/types"
)

type Key struct {
	//schema.GroupVersionKind
	types.NamespacedName
}

// String returns the
func (r Key) String() string {
	if r.Namespace == "" {
		return r.Name
	}
	return fmt.Sprintf("%s.%s", r.Namespace, r.Name)
}

// KeyFromNSN takes a types.NamespacedName and returns it
// wrapped in the Key struct.
func KeyFromNSN(nsn types.NamespacedName) Key {
	return Key{
		NamespacedName: nsn,
	}
}

// ToKey takes a resource name and returns a types.NamespacedName initialized with the name.
// The namespace attribute however is uninitialized.
func ToKey(name string) Key {
	return Key{
		NamespacedName: types.NamespacedName{Name: name},
	}
}
