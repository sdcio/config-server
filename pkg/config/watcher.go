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

package config

import (
	"context"
	"time"

	"github.com/iptecharch/config-server/pkg/store"
	"github.com/iptecharch/config-server/pkg/target"
	"github.com/henderiw/logger/log"
)

const (
	defaultWaitTime = 5 * time.Second
)

type targetWatcher struct {
	targetStore store.Storer[target.Context]
}

func (r *targetWatcher) Watch(ctx context.Context) {
	go r.watch(ctx)
}

func (r *targetWatcher) watch(ctx context.Context) {
	log := log.FromContext(ctx)
	log.Info("watch targets")
WATCH:
	wi, err := r.targetStore.Watch(ctx)
	if err != nil {
		log.Error("failed to create watch", "error", err)
		time.Sleep(defaultWaitTime)
		goto WATCH
	}

	for {
		select {
		case <-ctx.Done():
			wi.Stop()
			return
		case e, ok := <-wi.ResultChan():
			if !ok {
				// Channel is closed
				log.Error("watch targets", "ok", ok)
				goto WATCH
			}
			name := ""
			if e.Object.DataStore != nil {
				name = e.Object.DataStore.Name
			}
			log.Info("target changed", "eventType", e.Type.String(), "key", name)
		}
	}
}
