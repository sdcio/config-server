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


// Interface can be implemented by anything that knows how to watch and report changes.
type Interface[T1 any] interface {
	// Stop stops watching. Will close the channel returned by ResultChan(). Releases
	// any resources used by the watch.
	Stop()

	// ResultChan returns a chan which will receive all the events. If an error occurs
	// or Stop() is called, the implementation will close this channel and
	// release any resources used by the watch.
	ResultChan() <-chan Event[T1]
}

type EventType int

const (
	Added EventType = iota
	Modified
	Deleted
)

func (r EventType) String() string {
	return [...]string{"Added", "Modified", "Deleted"}[r]
}

// CreateEvent is an event where a object was created.
type Event[T1 any] struct {
	Type EventType
	// Object is the object from the event
	Object T1
}
