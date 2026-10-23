package dataservermanager

import (
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
)

func newTestManager() *DSConnManager {
	return &DSConnManager{subs: map[chan Event]struct{}{}, state: DSDown}
}

// A subscriber that never drains still ends up holding the latest transition.
func TestBroadcastSlowSubscriberKeepsLatestEvent(t *testing.T) {
	m := newTestManager()
	ch, cancel := m.Subscribe(2) // buffer already holds the initial snapshot
	defer cancel()

	m.broadcast(Event{State: DSUp})
	m.broadcast(Event{State: DSDown, Err: errors.New("flap")})
	m.broadcast(Event{State: DSUp})

	var last Event
	for len(ch) > 0 {
		last = <-ch
	}
	assert.Equal(t, DSUp, last.State, "newest transition must not be lost when the buffer is full")
}

func TestBroadcastDoesNotBlockOnFullSubscriber(t *testing.T) {
	m := newTestManager()
	_, cancel := m.Subscribe(1)
	defer cancel()

	for i := 0; i < 100; i++ {
		m.broadcast(Event{State: DSUp})
	}
}
