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

package file

import (
	"context"
	"fmt"
	"path/filepath"
	"reflect"

	"github.com/iptecharch/config-server/pkg/store"
	"github.com/iptecharch/config-server/pkg/store/watch"
	"github.com/henderiw/logger/log"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
)

const (
	// errors
	NotFound = "not found"
)

type Config[T1 any] struct {
	GroupResource schema.GroupResource
	RootPath      string
	Codec         runtime.Codec
	NewFunc       func() runtime.Object
}

func NewStore[T1 any](cfg *Config[T1]) (store.Storer[T1], error) {
	objRootPath := filepath.Join(cfg.RootPath, cfg.GroupResource.Group, cfg.GroupResource.Resource)
	if err := ensureDir(objRootPath); err != nil {
		return nil, fmt.Errorf("unable to write data dir: %s", err)
	}
	return &file[T1]{
		objRootPath: objRootPath,
		codec:       cfg.Codec,
		newFunc:     cfg.NewFunc,
		watchers:    watch.NewWatchers[T1](32),
	}, nil
}

type file[T1 any] struct {
	objRootPath string
	codec       runtime.Codec
	newFunc     func() runtime.Object
	watchers    *watch.Watchers[T1]
}

// Get return the type
func (r *file[T1]) Get(ctx context.Context, key store.Key) (T1, error) {
	return r.readFile(ctx, key)
}

func (r *file[T1]) List(ctx context.Context, visitorFunc func(ctx context.Context, key store.Key, obj T1)) {
	log := log.FromContext(ctx)

	if err := r.visitDir(ctx, visitorFunc); err != nil {
		log.Error("cannot list visiting dir failed", "error", err.Error())
	}
}

func (r *file[T1]) Create(ctx context.Context, key store.Key, data T1) error {
	// if an error is returned the entry already exists
	if _, err := r.Get(ctx, key); err == nil {
		return fmt.Errorf("duplicate entry %v", key.String())
	}
	// update the store before calling the callback since the cb fn will use this data
	if err := r.update(ctx, key, data); err != nil {
		return err
	}

	// notify watchers
	r.watchers.NotifyWatchers(ctx, watch.Event[T1]{
		Type:   watch.Added,
		Object: data,
	})
	return nil
}

// Upsert creates or updates the entry in the cache
func (r *file[T1]) Update(ctx context.Context, key store.Key, data T1) error {
	exists := true
	oldd, err := r.Get(ctx, key)
	if err != nil {
		exists = false
	}

	// update the cache before calling the callback since the cb fn will use this data
	if err := r.update(ctx, key, data); err != nil {
		return err
	}

	// // notify watchers based on the fact the data got modified or not
	if exists {
		if !reflect.DeepEqual(oldd, data) {
			r.watchers.NotifyWatchers(ctx, watch.Event[T1]{
				Type:   watch.Modified,
				Object: data,
			})
		}
	} else {
		r.watchers.NotifyWatchers(ctx, watch.Event[T1]{
			Type:   watch.Added,
			Object: data,
		})
	}
	return nil
}

func (r *file[T1]) update(ctx context.Context, key store.Key, newd T1) error {
	return r.writeFile(ctx, key, newd)
}

func (r *file[T1]) delete(ctx context.Context, key store.Key) error {
	return r.deleteFile(ctx, key)
}

// Delete deletes the entry in the cache
func (r *file[T1]) Delete(ctx context.Context, key store.Key) error {
	// only if an exisitng object gets deleted we
	// call the registered callbacks
	exists := true
	obj, err := r.Get(ctx, key)
	if err != nil {
		return nil
	}
	// if exists call the callback
	if exists {
		r.watchers.NotifyWatchers(ctx, watch.Event[T1]{
			Type:   watch.Deleted,
			Object: obj,
		})
	}
	// delete the entry to ensure the cb uses the proper data
	return r.delete(ctx, key)
}

func (r *file[T1]) Watch(ctx context.Context) (watch.Interface[T1], error) {
	// lock is not required here
	log := log.FromContext(ctx)
	log.Info("watch file store")
	if r.watchers.IsExhausted() {
		return nil, fmt.Errorf("cannot allocate watcher, out of resources")
	}
	w := r.watchers.GetWatchContext()

	// On initial watch, send all the existing objects
	items := map[store.Key]T1{}
	r.List(ctx, func(ctx context.Context, key store.Key, obj T1) {
		items[key] = obj
	})
	log.Info("watch list items", "len", len(items))
	for _, obj := range items {
		w.ResultCh <- watch.Event[T1]{
			Type:   watch.Added,
			Object: obj,
		}
	}
	// this ensures the initial events from the list
	// get processed first
	log.Info("watcher add")
	if err := r.watchers.Add(w); err != nil {
		log.Info("cannot add watcher", "error", err.Error())
		return nil, err
	}
	log.Info("watcher added")
	return w, nil
}