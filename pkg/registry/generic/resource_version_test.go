/*
Copyright 2026 Nokia.

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

package generic

import (
	"context"
	"errors"
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/watch"
)

func objWithRV(rv string) runtime.Object {
	return &corev1.ConfigMap{ObjectMeta: metav1.ObjectMeta{ResourceVersion: rv}}
}

func listOf(objs ...runtime.Object) func(func(runtime.Object)) error {
	return func(visit func(runtime.Object)) error {
		for _, o := range objs {
			visit(o)
		}
		return nil
	}
}

func TestAllocatorInitFromStorage(t *testing.T) {
	a := &rvAllocator{}
	// per-object resourceVersions of the old scheme, plus junk
	a.ensureInit(context.Background(), listOf(objWithRV("6"), objWithRV("0"), objWithRV("12"), objWithRV("x")))
	assert.Equal(t, uint64(12), a.Current())

	// init runs once
	a.ensureInit(context.Background(), listOf(objWithRV("99")))
	assert.Equal(t, uint64(12), a.Current())
}

func TestAllocatorWriteIsMonotonicAndOrdered(t *testing.T) {
	a := &rvAllocator{}
	a.ensureInit(context.Background(), listOf())

	var mu sync.Mutex
	var published []string
	publish := func(ev watch.Event) {
		mu.Lock()
		defer mu.Unlock()
		published = append(published, ev.Object.(*corev1.ConfigMap).ResourceVersion)
	}

	var wg sync.WaitGroup
	for i := 0; i < 50; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_ = a.write(func(rv string) (*watch.Event, error) {
				return &watch.Event{Type: watch.Added, Object: objWithRV(rv)}, nil
			}, publish)
		}()
	}
	wg.Wait()

	assert.Equal(t, uint64(50), a.Current())
	require.Len(t, published, 50)
	// published in resourceVersion order
	for i, rv := range published {
		n, ok := resourceVersionOf(objWithRV(rv))
		require.True(t, ok)
		assert.Equal(t, uint64(i+1), n)
	}
}

func TestAllocatorFailedWriteDoesNotAdvance(t *testing.T) {
	a := &rvAllocator{}
	a.ensureInit(context.Background(), listOf(objWithRV("5")))

	published := 0
	err := a.write(func(rv string) (*watch.Event, error) {
		return nil, errors.New("storage failed")
	}, func(watch.Event) { published++ })

	assert.Error(t, err)
	assert.Equal(t, uint64(5), a.Current())
	assert.Equal(t, 0, published)
}

func TestAllocatorSharedPerGroupResource(t *testing.T) {
	gr := schema.GroupResource{Group: "test.sdcio.dev", Resource: "things"}
	assert.Same(t, allocatorFor(gr), allocatorFor(gr), "main and status strategy must share one counter")
	assert.NotSame(t, allocatorFor(gr), allocatorFor(schema.GroupResource{Group: "test.sdcio.dev", Resource: "others"}))
}
