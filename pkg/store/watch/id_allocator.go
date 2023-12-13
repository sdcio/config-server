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

package watch

import (
	"fmt"
	"sync"
)

// IDAllocator represents the ID allocation service.
type idAllocator struct {
	maxID        int
	availableIDs []int
	allocatedIDs map[int]bool
	m            sync.RWMutex
}

// newIDAllocator creates a new instance of IDAllocator with a specified range of IDs.
func newIDAllocator(maxID int) *idAllocator {
	availableIDs := make([]int, 0, maxID+1)
	for i := 0; i <= maxID; i++ {
		availableIDs = append(availableIDs, i)
	}

	return &idAllocator{
		maxID:        maxID,
		availableIDs: availableIDs,
		allocatedIDs: make(map[int]bool),
	}
}

func (r *idAllocator) IsExhausted() bool {
	r.m.RLock()
	defer r.m.RUnlock()
	return len(r.availableIDs) == 0
}

// AllocateID allocates an available ID.
func (r *idAllocator) AllocateID() (int, error) {
	r.m.Lock()
	defer r.m.Unlock()

	if len(r.availableIDs) == 0 {
		return 0, fmt.Errorf("mac available ID(s) %d reached", r.maxID)
	}

	// Get the first available ID
	id := r.availableIDs[0]

	// Remove the allocated ID from the available IDs slice
	r.availableIDs = r.availableIDs[1:]

	// Mark the ID as allocated
	r.allocatedIDs[id] = true

	return id, nil
}

// ReleaseID releases a previously allocated ID.
func (r *idAllocator) ReleaseID(id int) error {
	r.m.Lock()
	defer r.m.Unlock()

	// Check if the ID is allocated
	if !r.allocatedIDs[id] {
		return fmt.Errorf("ID %d is not allocated", id)
	}

	// Add the released ID back to the available IDs slice
	r.availableIDs = append(r.availableIDs, id)

	// Mark the ID as released
	delete(r.allocatedIDs, id)

	return nil
}
