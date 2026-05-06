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

	"github.com/henderiw/logger/log"
	"github.com/sdcio/config-server/apis/config"
	sdcpb "github.com/sdcio/sdc-protos/sdcpb"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/encoding/prototext"
	"k8s.io/utils/ptr"
)

// Transactor is responsible exclusively for gRPC communication with the datastore.
// It has no knowledge of Kubernetes resources, secrets, or conditions.
type Transactor struct{}

func NewTransactor() *Transactor {
	return &Transactor{}
}

// Execute sends a TransactionSet to the datastore and returns the raw response.
// It does NOT confirm — the caller decides whether to confirm or rollback.
func (t *Transactor) Execute(
	ctx context.Context,
	dsctx *DatastoreHandle,
	txID string,
	intents []*sdcpb.TransactionIntent,
	dryRun bool,
) (*sdcpb.TransactionSetResponse, error) {
	log := log.FromContext(ctx).With("transactionID", txID, "datastore", dsctx.DatastoreName)
	log.Info("executing transaction", "intents", len(intents), "dryRun", dryRun)

	rsp, err := dsctx.Client.TransactionSet(ctx, &sdcpb.TransactionSetRequest{
		TransactionId: txID,
		DatastoreName: dsctx.DatastoreName,
		DryRun:        dryRun,
		Timeout:       ptr.To(int32(60)),
		Intents:       intents,
	})
	if rsp != nil {
		log.Debug("transaction response", "rsp", prototext.Format(rsp))
	}
	return rsp, err
}

// Confirm commits a previously executed transaction.
func (t *Transactor) Confirm(
	ctx context.Context,
	dsctx *DatastoreHandle,
	txID string,
) error {
	_, err := dsctx.Client.TransactionConfirm(ctx, &sdcpb.TransactionConfirmRequest{
		DatastoreName: dsctx.DatastoreName,
		TransactionId: txID,
	})
	return err
}

// TransactionSet is a convenience method that executes and immediately confirms.
// Used by the recovery path where the two-step flow is not needed.
func (t *Transactor) TransactionSet(
	ctx context.Context,
	dsctx *DatastoreHandle,
	req *sdcpb.TransactionSetRequest,
) (string, error) {
	rsp, err := dsctx.Client.TransactionSet(ctx, req)
	msg, err := processTransactionResponse(ctx, rsp, err)
	if err != nil {
		return msg, err
	}
	if req.DryRun {
		return msg, nil
	}
	if _, err := dsctx.Client.TransactionConfirm(ctx, &sdcpb.TransactionConfirmRequest{
		DatastoreName: req.DatastoreName,
		TransactionId: req.TransactionId,
	}); err != nil {
		return msg, err
	}
	return msg, nil
}

// BuildGRPCIntents converts IntentInputs into sdcpb.TransactionIntents.
// Pure transformation — no external calls.
func BuildGRPCIntents(
	toUpdate []IntentInput,
	toDelete []IntentInput,
) ([]*sdcpb.TransactionIntent, error) {
	intents := make([]*sdcpb.TransactionIntent, 0, len(toUpdate)+len(toDelete))

	for _, inp := range toUpdate {
		update, err := config.GetIntentUpdateFromBlobs(inp.Config.Spec.Config)
		if err != nil {
			return nil, fmt.Errorf("build update intent for %s: %w", config.GetGVKNSN(inp.Config), err)
		}
		intents = append(intents, &sdcpb.TransactionIntent{
			Intent:       config.GetGVKNSN(inp.Config),
			Priority:     inp.Priority,
			Update:       update,
			NonRevertive: inp.NonRevertive,
		})
	}

	for _, inp := range toDelete {
		intents = append(intents, &sdcpb.TransactionIntent{
			Intent:              config.GetGVKNSN(inp.Config),
			Delete:              true,
			DeleteIgnoreNoExist: true,
			Orphan:              inp.Config.Orphan(),
		})
	}

	return intents, nil
}

// AnalyzeIntentResponse inspects the gRPC response and error to produce a
// structured TransactionResult. Pure function — no external calls.
func AnalyzeIntentResponse(err error, rsp *sdcpb.TransactionSetResponse) TransactionResult {
	result := TransactionResult{}

	if err != nil {
		result.GlobalError = fmt.Errorf("transaction error: %w", err)
		if statusErr, ok := status.FromError(err); ok {
			switch statusErr.Code() {
			case codes.Aborted, codes.ResourceExhausted:
				result.Recoverable = true
			default:
				result.Recoverable = false
			}
		}
	}

	if rsp != nil {
		result.GlobalWarnings = append(result.GlobalWarnings, rsp.Warnings...)
		for _, intent := range rsp.Intents {
			for _, intentError := range intent.Errors {
				result.IntentErrors = errors.Join(result.IntentErrors, fmt.Errorf("%s", intentError))
				result.Recoverable = false
			}
		}
	}

	return result
}

func collectWarnings(msgs []string) []string {
	out := make([]string, 0, len(msgs))
	for _, m := range msgs {
		out = append(out, fmt.Sprintf("warning: %q", m))
	}
	return out
}

func processTransactionResponse(ctx context.Context, rsp *sdcpb.TransactionSetResponse, err error) (string, error) {
	if err != nil {
		return err.Error(), err
	}
	if rsp != nil && len(rsp.Warnings) > 0 {
		log.FromContext(ctx).Warn("transaction warnings", "warnings", rsp.Warnings)
	}
	return "", nil
}
