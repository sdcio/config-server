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
	"strings"

	"github.com/henderiw/logger/log"
	sdcpb "github.com/sdcio/sdc-protos/sdcpb"
	"google.golang.org/grpc/codes"
	"google.golang.org/protobuf/encoding/prototext"
)

// processTransactionResponse returns the warnings as a string and aggregates the errors in a single error and classifies them
// as recoverable or non recoverable.
func processTransactionResponse(ctx context.Context, rsp *sdcpb.TransactionSetResponse, rsperr error) (string, error) {
	log := log.FromContext(ctx)
	var errs error
	var collectedWarnings []string
	var recoverable bool
	if rsperr != nil {
		errs = errors.Join(errs, fmt.Errorf("error: %s", rsperr.Error()))
		recoverable = isRecoverableTransactionError(rsperr)
	}
	if rsp != nil {
		for _, warning := range rsp.Warnings {
			collectedWarnings = append(collectedWarnings, fmt.Sprintf("global warning: %q", warning))
		}
		for key, intent := range rsp.Intents {
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
	log.Debug("transaction response", "rsp", prototext.Format(rsp), "msg", msg, "error", err)
	return msg, err
}

// isRecoverableTransactionError reports whether a data-server transaction error
// is transient and worth retrying.
//
// Besides the gRPC codes for "busy / unreachable", this also covers the
// data-server errors that still arrive as codes.Unknown ("transaction
// ongoing", "context is closed"): they mean another transaction holds the
// datastore for now, never that the configs are wrong. Treating them as
// non-recoverable latched Configs (FailedUnrecoverable) and Targets
// (RecoveryFailed) on a lock the data-server releases by itself.
// NotFound is the datastore missing while the runtime recreates it.
func isRecoverableTransactionError(err error) bool {
	if err == nil {
		return false
	}
	if isTransientTxErr(err) {
		return true
	}
	return grpcCode(err) == codes.NotFound
}