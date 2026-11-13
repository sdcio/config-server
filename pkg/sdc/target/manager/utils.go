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

package targetmanager

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strings"

	"github.com/sdcio/data-server/pkg/api"
	logf "github.com/sdcio/logger"
)

// processTransactionResult returns the warnings as a string and aggregates the
// errors in a single TransactionError, classified recoverable when the call
// failed with a retryable error. Intent validation errors are never recoverable.
func processTransactionResult(ctx context.Context, rsp *api.TransactionResult, rsperr error) (string, error) {
	log := logf.FromContext(ctx)
	var errs error
	var collectedWarnings []string
	var recoverable bool
	if rsperr != nil {
		errs = errors.Join(errs, fmt.Errorf("error: %s", rsperr.Error()))
		recoverable = api.IsRetryable(rsperr)
	}
	if rsp != nil {
		for _, warning := range rsp.Warnings {
			collectedWarnings = append(collectedWarnings, fmt.Sprintf("global warning: %q", warning))
		}
		for _, key := range sortedKeys(rsp.Intents) {
			intent := rsp.Intents[key]
			for _, intentError := range intent.Errors {
				errs = errors.Join(errs, fmt.Errorf("intent %q error: %q", key, intentError))
			}
			for _, intentWarning := range intent.Warnings {
				collectedWarnings = append(collectedWarnings, fmt.Sprintf("intent %q warning: %q", key, intentWarning))
			}
		}
	}
	var err error
	var msg string
	if errs != nil {
		err = NewTransactionError(errs, recoverable)
	}
	if len(collectedWarnings) > 0 {
		msg = strings.Join(collectedWarnings, "; ")
	}
	log.V(logf.VDebug).Info("transaction response", "rsp", rsp, "msg", msg, "error", err)
	return msg, err
}

func sortedKeys[T any](m map[string]T) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}
