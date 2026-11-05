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

package v1alpha1

import (
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/stretchr/testify/assert"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func TestTruncateMessage(t *testing.T) {
	long := strings.Repeat("a", 100)

	cases := map[string]struct {
		msg  string
		max  int
		want string
	}{
		"fits":                    {msg: "short", max: 10, want: "short"},
		"exact fit":               {msg: "0123456789", max: 10, want: "0123456789"},
		"empty":                   {msg: "", max: 10, want: ""},
		"zero max":                {msg: "abc", max: 0, want: ""},
		"cut with marker":         {msg: long, max: 20, want: strings.Repeat("a", 20-len(TruncatedSuffix)) + TruncatedSuffix},
		"max smaller than marker": {msg: long, max: 2, want: "aa"},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			got := TruncateMessage(tc.msg, tc.max)
			assert.Equal(t, tc.want, got)
			assert.LessOrEqual(t, len(got), max(tc.max, 0))
		})
	}
}

// Never split a multi-byte character, always stay within the limit, and
// mark the cut when there is room for the marker.
func TestTruncateMessageRuneBoundary(t *testing.T) {
	msg := strings.Repeat("é", 50) // 2 bytes each
	for limit := 1; limit <= len(msg)+1; limit++ {
		got := TruncateMessage(msg, limit)
		assert.True(t, utf8.ValidString(got), "limit %d: invalid UTF-8 %q", limit, got)
		assert.LessOrEqual(t, len(got), limit, "limit %d", limit)
		if len(msg) > limit && limit > len(TruncatedSuffix) {
			assert.True(t, strings.HasSuffix(got, TruncatedSuffix), "limit %d must mark the cut", limit)
		}
	}
}

// Invalid UTF-8 must not make the cut walk below 0 (the loop in the
// original RecordEvent had no lower bound).
func TestTruncateMessageInvalidUTF8(t *testing.T) {
	msg := strings.Repeat("\x80", 50) // continuation bytes only
	assert.NotPanics(t, func() { _ = TruncateMessage(msg, 10) })
}

func TestConstructorsBoundMessage(t *testing.T) {
	long := strings.Repeat("x", 5*MaxMessageBytes)

	for name, c := range map[string]Condition{
		"Failed":              Failed(long),
		"FailedUnRecoverable": FailedUnRecoverable(long),
		"Rollout":             Rollout(long),
		"ReadyWithMsg":        ReadyWithMsg(long),
		"WithMessage":         Failed("").WithMessage(long),
		"NewCondition":        NewCondition(ConditionTypeReady, metav1.ConditionFalse, ConditionReasonFailed, long),
	} {
		t.Run(name, func(t *testing.T) {
			assert.LessOrEqual(t, len(c.Message), MaxMessageBytes)
			assert.True(t, strings.HasSuffix(c.Message, TruncatedSuffix))
		})
	}
}

// Truncation is deterministic, so the no-change guards (Condition.Equal)
// stay stable when the same long error is reported again.
func TestTruncatedConditionsCompareEqual(t *testing.T) {
	long := strings.Repeat("x", 5*MaxMessageBytes)
	assert.True(t, Failed(long).Equal(Failed(long)))
}