package reconcilers

import (
	"unicode/utf8"

	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/tools/events"
)

// RecordEvent records a literal message, truncating it to the Event note's 1024-byte limit.
func RecordEvent(recorder events.EventRecorder, regarding runtime.Object, eventType, reason, message string) {
	const maxNoteBytes = 1024
	const suffix = "..."

	if len(message) > maxNoteBytes {
		end := maxNoteBytes - len(suffix)
		for !utf8.RuneStart(message[end]) {
			end--
		}
		message = message[:end] + suffix
	}

	recorder.Eventf(regarding, nil, eventType, reason, "Reconcile", "%s", message)
}
