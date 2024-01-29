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

package watchermanager

import (
	metainternalversion "k8s.io/apimachinery/pkg/apis/meta/internalversion"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/watch"
)

type Watcher interface {
	OnChange(eventType watch.EventType, obj runtime.Object) bool
}

type watcher struct {
	key string // uuid allocated to allow for delete
	// isDone should return non-nil when the watcher is finished.
	// This is normally bound to ctx.Err()
	isDone        func() error
	callback      Watcher                          // interface that handles OnChange
	filterOptions *metainternalversion.ListOptions // TODO update this
}
