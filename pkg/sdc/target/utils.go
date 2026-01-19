package target

import (
	"context"
	"fmt"
	"errors"
	"strings"

	"github.com/henderiw/apiserver-store/pkg/storebackend"
	"github.com/henderiw/logger/log"
	"github.com/sdcio/config-server/apis/config"
	sdcpb "github.com/sdcio/sdc-protos/sdcpb"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"google.golang.org/protobuf/encoding/prototext"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

func GetGVKNSN(obj client.Object) string {
	return fmt.Sprintf("%s.%s", obj.GetNamespace(), obj.GetName())
}

// useSpec indicates to use the spec as the confifSpec, typically set to true; when set to false it means we are recovering
// the config
func GetIntentUpdate(ctx context.Context, key storebackend.Key, config *config.Config, useSpec bool) ([]*sdcpb.Update, error) {
	logger := log.FromContext(ctx)
	update := make([]*sdcpb.Update, 0, len(config.Spec.Config))
	configSpec := config.Spec.Config
	if !useSpec && config.Status.AppliedConfig != nil {
		update = make([]*sdcpb.Update, 0, len(config.Status.AppliedConfig.Config))
		configSpec = config.Status.AppliedConfig.Config
	}

	for _, config := range configSpec {
		path, err := sdcpb.ParsePath(config.Path)
		if err != nil {
			return nil, fmt.Errorf("create data failed for target %s, path %s invalid", key.String(), config.Path)
		}
		logger.Debug("setIntent", "configSpec", string(config.Value.Raw))
		update = append(update, &sdcpb.Update{
			Path: path,
			Value: &sdcpb.TypedValue{
				Value: &sdcpb.TypedValue_JsonVal{
					JsonVal: config.Value.Raw,
				},
			},
		})
	}
	return update, nil
}

// processTransactionResponse returns the warnings as a string and aggregates the errors in a single error and classifies them
// as recoverable or non recoverable.
func processTransactionResponse(ctx context.Context, rsp *sdcpb.TransactionSetResponse, rsperr error) (string, error) {
	log := log.FromContext(ctx)
	var errs error
	var collectedWarnings []string
	var recoverable bool
	if rsperr != nil {
		errs = errors.Join(errs, fmt.Errorf("error: %s", rsperr.Error()))
		if er, ok := status.FromError(rsperr); ok {
			switch er.Code() {
			// Aborted is the refering to a lock in the dataserver
			case codes.Aborted, codes.ResourceExhausted:
				recoverable = true
			default:
				recoverable = false
			}
		}
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