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

package schema

import (
	"sync"

	"golang.org/x/sync/semaphore"
)

type RepoMgr struct {
	m     sync.RWMutex
	repos map[string]*semaphore.Weighted
}

// NewRepoMgr initializes a new RepoMgr
func NewRepoMgr() *RepoMgr {
	return &RepoMgr{
		repos: make(map[string]*semaphore.Weighted),
	}
}

// getOrAdd returns the semaphore for a repository, adding it if necessary
func (r *RepoMgr) GetOrAdd(url string) *semaphore.Weighted {
	r.m.Lock()
	defer r.m.Unlock()

	// Check if the semaphore already exists
	if sem, exists := r.repos[url]; exists {
		return sem
	}

	// Create and store a new semaphore
	sem := semaphore.NewWeighted(1)
	r.repos[url] = sem
	return sem
}
