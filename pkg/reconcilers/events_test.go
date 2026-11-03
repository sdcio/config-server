package reconcilers

import (
	"strings"
	"testing"
	"time"
	"unicode/utf8"

	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	eventsv1 "k8s.io/api/events/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/tools/events"
)

func TestRecordEvent(t *testing.T) {
	tests := map[string]struct {
		eventType string
		message   string
		wantNote  string
	}{
		"normal": {
			eventType: corev1.EventTypeNormal,
			message:   "ready",
			wantNote:  "ready",
		},
		"warning with literal format directives": {
			eventType: corev1.EventTypeWarning,
			message:   "failed at 50%: %s %d %[1]v",
			wantNote:  "failed at 50%: %s %d %[1]v",
		},
		"message exceeding action limit": {
			eventType: corev1.EventTypeWarning,
			message:   strings.Repeat("x", 129),
			wantNote:  strings.Repeat("x", 129),
		},
		"exact note limit": {
			eventType: corev1.EventTypeWarning,
			message:   strings.Repeat("x", 1024),
			wantNote:  strings.Repeat("x", 1024),
		},
		"one byte over note limit": {
			eventType: corev1.EventTypeWarning,
			message:   strings.Repeat("x", 1025),
			wantNote:  strings.Repeat("x", 1021) + "...",
		},
		"long normal message": {
			eventType: corev1.EventTypeNormal,
			message:   strings.Repeat("ready ref ", 200),
			wantNote:  strings.Repeat("ready ref ", 200)[:1021] + "...",
		},
		"multibyte character crossing truncation boundary": {
			eventType: corev1.EventTypeWarning,
			message:   strings.Repeat("x", 1020) + strings.Repeat("\u20ac", 10),
			wantNote:  strings.Repeat("x", 1020) + "...",
		},
		"four byte characters": {
			eventType: corev1.EventTypeWarning,
			message:   strings.Repeat("\U0001f600", 300),
			wantNote:  strings.Repeat("\U0001f600", 255) + "...",
		},
	}

	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			scheme := runtime.NewScheme()
			require.NoError(t, corev1.AddToScheme(scheme))
			broadcaster := events.NewBroadcaster(nil)
			t.Cleanup(broadcaster.Shutdown)

			recorded := make(chan *eventsv1.Event, 1)
			stop, err := broadcaster.StartEventWatcher(func(obj runtime.Object) {
				recorded <- obj.(*eventsv1.Event).DeepCopy()
			})
			require.NoError(t, err)
			t.Cleanup(stop)

			regarding := &corev1.ConfigMap{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test",
					Namespace: "default",
					UID:       types.UID("test-uid"),
				},
			}
			recorder := broadcaster.NewRecorder(scheme, "TestController")
			RecordEvent(recorder, regarding, tt.eventType, "TestReason", tt.message)

			select {
			case event := <-recorded:
				require.Equal(t, "Reconcile", event.Action)
				require.LessOrEqual(t, len(event.Action), 128)
				require.Equal(t, tt.wantNote, event.Note)
				require.LessOrEqual(t, len(event.Note), 1024)
				require.True(t, utf8.ValidString(event.Note))
				require.Equal(t, tt.eventType, event.Type)
				require.Equal(t, "TestReason", event.Reason)
				require.Equal(t, regarding.Name, event.Regarding.Name)
				require.Equal(t, regarding.Namespace, event.Regarding.Namespace)
				require.Equal(t, regarding.UID, event.Regarding.UID)
				require.Equal(t, "TestController", event.ReportingController)
				require.Nil(t, event.Related)
			case <-time.After(5 * time.Second):
				t.Fatal("timed out waiting for recorded event")
			}
		})
	}
}
