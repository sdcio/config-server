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
	"unicode/utf8"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

const (
	// MaxMessageBytes bounds human-readable messages: condition messages
	// (built by the constructors in this package) and event notes (the
	// Kubernetes Event note limit is 1024 bytes). Long dataserver/device
	// errors stay readable in kubectl output and never exceed API limits.
	MaxMessageBytes = 1024

	// TruncatedSuffix marks a message that was shortened.
	TruncatedSuffix = "..."
)

// TruncateMessage returns msg unchanged when it fits in max bytes. Otherwise
// it cuts msg at a UTF-8 rune boundary and appends TruncatedSuffix, so the
// result is at most max bytes, valid UTF-8, and visibly shortened.
//
// The result is deterministic, so comparing a freshly built condition with a
// stored one (Condition.Equal) stays stable across reconciles.
func TruncateMessage(msg string, max int) string {
	if max <= 0 {
		return ""
	}
	if len(msg) <= max {
		return msg
	}
	if max <= len(TruncatedSuffix) {
		return cutAtRune(msg, max)
	}
	return cutAtRune(msg, max-len(TruncatedSuffix)) + TruncatedSuffix
}

// cutAtRune returns the longest prefix of s of at most n bytes that ends on
// a rune boundary. Safe on invalid UTF-8 (never walks below 0).
func cutAtRune(s string, n int) string {
	if n >= len(s) {
		return s
	}
	for n > 0 && !utf8.RuneStart(s[n]) {
		n--
	}
	return s[:n]
}

// NewCondition builds a condition with a bounded message. Resource-specific
// constructors (e.g. TargetNotReady) should use it so every condition
// message goes through the same limit.
func NewCondition(t ConditionType, status metav1.ConditionStatus, reason ConditionReason, msg string) Condition {
	return Condition{metav1.Condition{
		Type:               string(t),
		Status:             status,
		LastTransitionTime: metav1.Now(),
		Reason:             string(reason),
		Message:            TruncateMessage(msg, MaxMessageBytes),
	}}
}