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

package discoveryrule

import (
	"fmt"
	"strings"
)

type Key struct {
	APIVersion string
	Kind       string
	Namespace  string
	Name       string
}

func (r Key) String() string {
	return fmt.Sprintf("%s#%s#%s#%s",
		r.APIVersion,
		r.Kind,
		r.Namespace,
		r.Name,
	)
}

func GetKey(s string) Key {
	split := strings.Split(s, "#")
	if len(split) != 5 {
		return Key{}
	}
	return Key{
		APIVersion: split[0],
		Kind:       split[1],
		Namespace:  split[2],
		Name:       split[3],
	}
}
