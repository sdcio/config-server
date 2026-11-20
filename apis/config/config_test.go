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
	"reflect"
	"testing"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// A status update writes status only: the object meta, generation included,
// comes from the stored object.
func TestPrepareForStatusUpdate_KeepsObjectMeta(t *testing.T) {
	old := &Config{ObjectMeta: metav1.ObjectMeta{
		Name:       "test",
		Generation: 3,
		Labels:     map[string]string{"a": "1"},
		Finalizers: []string{"config.config.sdcio.dev/finalizer"},
	}}
	obj := &Config{ObjectMeta: metav1.ObjectMeta{
		Name:       "test",
		Generation: 7,
		Labels:     map[string]string{"a": "2"},
	}}

	(&Config{}).PrepareForStatusUpdate(context.Background(), obj, old)

	if obj.Generation != 3 {
		t.Errorf("generation: got %d, want 3", obj.Generation)
	}
	if !reflect.DeepEqual(obj.Labels, old.Labels) {
		t.Errorf("labels: got %v, want %v", obj.Labels, old.Labels)
	}
	if !reflect.DeepEqual(obj.Finalizers, old.Finalizers) {
		t.Errorf("finalizers: got %v, want %v", obj.Finalizers, old.Finalizers)
	}
}