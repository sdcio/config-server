/*
Copyright 2024 Nokia.

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

package config

import (
	"context"
	"fmt"
	"strings"

	"github.com/google/uuid"
	"github.com/henderiw/apiserver-store/pkg/storebackend"
	"github.com/henderiw/logger/log"
	"github.com/sdcio/config-server/apis/condition"
	dsclient "github.com/sdcio/config-server/pkg/sdc/dataserver/client"
	"github.com/sdcio/sdc-protos/sdcpb"
	"google.golang.org/protobuf/encoding/protojson"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/validation/field"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

const RunningIntentName = "running"

// GetCondition returns the condition based on the condition kind
func (r *Target) GetConditions() []condition.Condition {
	return r.Status.GetConditions()
}

// GetCondition returns the condition based on the condition kind
func (r *Target) GetCondition(t condition.ConditionType) condition.Condition {
	return r.Status.GetCondition(t)
}

// SetConditions sets the conditions on the resource. it allows for 0, 1 or more conditions
// to be set at once
func (r *Target) SetConditions(c ...condition.Condition) {
	r.Status.SetConditions(c...)
}

func (r *Target) IsConditionReady() bool {
	return r.GetCondition(condition.ConditionTypeReady).Status == metav1.ConditionTrue
}

func (r *TargetStatus) GetDiscoveryInfo() DiscoveryInfo {
	if r.DiscoveryInfo != nil {
		return *r.DiscoveryInfo
	}
	return DiscoveryInfo{}
}

// BuildTarget returns a reource from a client Object a Spec/Status
func BuildTarget(meta metav1.ObjectMeta, spec TargetSpec) *Target {
	return &Target{
		TypeMeta: metav1.TypeMeta{
			APIVersion: SchemeGroupVersion.Identifier(),
			Kind:       TargetKind,
		},
		ObjectMeta: meta,
		Spec:       spec,
	}
}

func BuildEmptyTarget() *Target {
	return &Target{
		TypeMeta: metav1.TypeMeta{
			APIVersion: SchemeGroupVersion.Identifier(),
			Kind:       TargetKind,
		},
	}
}

func (r *Target) IsReady() bool {
	return r.GetCondition(condition.ConditionTypeReady).Status == metav1.ConditionTrue &&
		r.GetCondition(ConditionTypeTargetDiscoveryReady).Status == metav1.ConditionTrue &&
		r.GetCondition(ConditionTypeTargetDatastoreReady).Status == metav1.ConditionTrue &&
		r.GetCondition(ConditionTypeTargetConnectionReady).Status == metav1.ConditionTrue
}

func (r *Target) GetNamespacedName() types.NamespacedName {
	return types.NamespacedName{Namespace: r.Namespace, Name: r.Name}
}

func (r *Target) GetRunningConfig(ctx context.Context, opts *TargetRunningConfigOptions) (runtime.Object, error) {
	targetKey := r.GetNamespacedName()
	if !r.IsReady() {
		return nil, apierrors.NewServiceUnavailable(
			fmt.Sprintf("target %s is not ready: %s", targetKey,
				r.GetCondition(condition.ConditionTypeReady).Message))
	}

	var format TargetFormat
	if opts != nil {
		format = ParseTargetFormat(opts.Format)
	}

	cfg := &dsclient.Config{
		Address:  dsclient.GetDataServerAddress(),
		Insecure: true,
	}

	dsclient, closeFn, err := dsclient.NewEphemeral(ctx, cfg)
	if err != nil {
		return nil, err
	}
	defer func() {
		if err := closeFn(); err != nil {
			log.FromContext(ctx).Error("failed to close connection", "Error", err)
		}
	}()

	rsp, err := dsclient.GetIntent(ctx, &sdcpb.GetIntentRequest{
		DatastoreName: storebackend.KeyFromNSN(targetKey).String(),
		Intent:        RunningIntentName,
		Format:        FormatToProto(format),
	})
	if err != nil {
		return nil, err
	}

	return &TargetRunningConfig{
		ObjectMeta: metav1.ObjectMeta{
			Name:      r.Name,
			Namespace: r.Namespace,
		},
		Value:  string(rsp.GetBlob()),
		Format: string(format),
	}, nil
}

func FormatToProto(f TargetFormat) sdcpb.Format {
	switch f {
	case Format_JSON_IETF:
		return sdcpb.Format_Intent_Format_JSON_IETF
	case Format_XML:
		return sdcpb.Format_Intent_Format_XML
	case Format_PROTO:
		return sdcpb.Format_Intent_Format_PROTO
	case Format_XPATH:
		return sdcpb.Format_Intent_Format_XPATH
	default:
		return sdcpb.Format_Intent_Format_JSON
	}
}

func (r *Target) GetConfigBlame(ctx context.Context) (runtime.Object, error) {
	targetKey := r.GetNamespacedName()
	if !r.IsReady() {
		return nil, apierrors.NewServiceUnavailable(
			fmt.Sprintf("target %s is not ready: %s", targetKey,
				r.GetCondition(condition.ConditionTypeReady).Message))
	}

	cfg := &dsclient.Config{
		Address:  dsclient.GetDataServerAddress(),
		Insecure: true,
	}

	dsclient, closeFn, err := dsclient.NewEphemeral(ctx, cfg)
	if err != nil {
		return nil, err
	}
	defer func() {
		if err := closeFn(); err != nil {
			// You can use your preferred logging framework here
			log.FromContext(ctx).Error("failed to close connection", "Error", err)
		}
	}()

	rsp, err := dsclient.BlameConfig(ctx, &sdcpb.BlameConfigRequest{
		DatastoreName:   storebackend.KeyFromNSN(targetKey).String(),
		IncludeDefaults: true,
	})
	if err != nil {
		return nil, err
	}

	if rsp == nil || rsp.ConfigTree == nil {
		return &TargetConfigBlame{
			ObjectMeta: metav1.ObjectMeta{
				Name:      r.Name,
				Namespace: r.Namespace,
			},
			Value: runtime.RawExtension{Raw: nil},
		}, nil
	}

	json, err := protojson.Marshal(rsp.ConfigTree)
	if err != nil {
		return nil, err
	}

	return &TargetConfigBlame{
		ObjectMeta: metav1.ObjectMeta{
			Name:      targetKey.Name,
			Namespace: targetKey.Namespace,
		},
		Value: runtime.RawExtension{Raw: json},
	}, nil
}

func (r *Target) ClearDeviations(ctx context.Context, c client.Client, req *TargetClearDeviation) (runtime.Object, error) {
	targetKey := r.GetNamespacedName()
	if !r.IsReady() {
		return nil, apierrors.NewServiceUnavailable(
			fmt.Sprintf("target %s is not ready: %s", targetKey,
				r.GetCondition(condition.ConditionTypeReady).Message))
	}

	spec := req.Spec
	if spec == nil {
		return nil, apierrors.NewBadRequest("spec is required")
	}

	// Use spec namespace if provided, otherwise target's namespace
	lookupNamespace := r.Namespace

	if configLister == nil {
		return nil, apierrors.NewInternalError(fmt.Errorf("config lister not initialized"))
	}

	// Fetch existing configs for the target
	configsByName, err := configLister(ctx, c, r.Namespace, r.Name, lookupNamespace)
	if err != nil {
		return nil, err
	}

	// any invalid entry in the batch will be rejected with 422 so the failure can't be masked by
	// a status-code-only client. If the batch is valid, the transaction will be executed.
	txReq, validationErrors := buildClearDeviationTxRequest(targetKey, spec, configsByName)
	if len(validationErrors) > 0 {
		return nil, apierrors.NewInvalid(
			schema.GroupKind{Group: GroupName, Kind: TargetClearDeviationKind},
			r.Name,
			validationErrorsToFieldErrors(validationErrors),
		)
	}
	if len(txReq.Intents) == 0 {
		return &TargetClearDeviation{
			ObjectMeta: metav1.ObjectMeta{Name: r.Name, Namespace: r.Namespace},
			Status: &TargetClearDeviationStatus{
				Message: "no configs to process",
			},
		}, nil
	}

	rsp, txErr := executeClearDeviationTx(ctx, txReq)
	status := buildClearDeviationStatus(spec.Config, configsByName, rsp, txErr)
	return &TargetClearDeviation{
		ObjectMeta: metav1.ObjectMeta{Name: r.Name, Namespace: r.Namespace},
		Status:     status,
	}, nil
}

// buildClearDeviationTxRequest validates each entry, builds the transaction
// intents for the valid ones, and returns the rejected entries separately
func buildClearDeviationTxRequest(
	targetKey types.NamespacedName,
	spec *TargetClearDeviationSpec,
	configsByName map[string]*Config,
) (*sdcpb.TransactionSetRequest, []TargetClearDeviationConfigResult) {

	var validationErrors []TargetClearDeviationConfigResult
	intents := make([]*sdcpb.TransactionIntent, 0, len(spec.Config))
	for _, clearCfg := range spec.Config {
		cfg, lookupErr := resolveClearDeviationConfig(clearCfg.Name, configsByName, targetKey)
		if lookupErr != nil {
			validationErrors = append(validationErrors, TargetClearDeviationConfigResult{
				Name:    clearCfg.Name,
				Success: false,
				Errors:  []string{lookupErr.Error()},
			})
			continue
		}

		if cfg.IsRevertive() {
			validationErrors = append(validationErrors, TargetClearDeviationConfigResult{
				Name:    clearCfg.Name,
				Success: false,
				Errors:  []string{fmt.Sprintf("config %q is revertive for target %s, not expecting clearDeviations", clearCfg.Name, targetKey.Name)},
			})
			continue
		}

		revertPaths := make([]*sdcpb.Path, 0, len(clearCfg.Paths))
		var pathErrors []string
		for _, p := range clearCfg.Paths {
			path, err := sdcpb.ParsePath(p)
			if err != nil {
				pathErrors = append(pathErrors, fmt.Sprintf("invalid path %q: %v", p, err))
				continue
			}
			revertPaths = append(revertPaths, path)
		}

		if len(pathErrors) > 0 {
			validationErrors = append(validationErrors, TargetClearDeviationConfigResult{
				Name:    clearCfg.Name,
				Success: false,
				Errors:  pathErrors,
			})
			continue
		}
		update, err := buildUpdates(cfg.Spec.Config)
		if err != nil {
			validationErrors = append(validationErrors, TargetClearDeviationConfigResult{
				Name:    clearCfg.Name,
				Success: false,
				Errors:  []string{fmt.Sprintf("failed to build intent update for config %q: %v", clearCfg.Name, err)},
			})
			continue
		}

		intents = append(intents, &sdcpb.TransactionIntent{
			Intent:       GetGVKNSN(cfg),
			Priority:     cfg.Spec.Priority,
			RevertPaths:  revertPaths,
			Update:       update,
			NonRevertive: !cfg.IsRevertive(),
		})
	}

	if len(intents) == 0 {
		return &sdcpb.TransactionSetRequest{}, validationErrors
	}

	return &sdcpb.TransactionSetRequest{
		TransactionId: uuid.New().String(),
		DatastoreName: storebackend.KeyFromNSN(targetKey).String(),
		DryRun:        false,
		Timeout:       ptr.To(int32(60)),
		Intents:       intents,
	}, validationErrors
}

// resolveClearDeviationConfig looks up a Config CR by either its own name
// or by a Deviation CR name produced by DeviationName. Target-typed
// deviation names are rejected because clearing target deviations is not
// implemented on this path.
func resolveClearDeviationConfig(
	name string,
	configsByName map[string]*Config,
	targetKey types.NamespacedName,
) (*Config, error) {
	if cfg, ok := configsByName[name]; ok {
		return cfg, nil
	}
	if typ, resource, ok := ParseDeviationName(name); ok {
		switch typ {
		case DeviationType_TARGET:
			return nil, fmt.Errorf(
				"deviation %q is target-scoped; clearing target deviations via cleardeviation is not supported",
				name)
		case DeviationType_CONFIG:
			if cfg, ok := configsByName[resource]; ok {
				return cfg, nil
			}
			return nil, fmt.Errorf(
				"config %q (from deviation name %q) not found for target %s",
				resource, name, targetKey.Name)
		}
	}
	return nil, fmt.Errorf("config %q not found for target %s", name, targetKey.Name)
}

// validationErrorsToFieldErrors converts per-entry validation results into
// the field.ErrorList shape that apierrors.NewInvalid expects.
func validationErrorsToFieldErrors(results []TargetClearDeviationConfigResult) field.ErrorList {
	errs := make(field.ErrorList, 0, len(results))
	for i, r := range results {
		msg := strings.Join(r.Errors, "; ")
		if msg == "" {
			msg = "validation failed"
		}
		errs = append(errs, field.Invalid(
			field.NewPath("spec", "config").Index(i).Child("name"),
			r.Name,
			msg,
		))
	}
	return errs
}

func GetGVKNSN(obj client.Object) string {
	return fmt.Sprintf("%s.%s", obj.GetNamespace(), obj.GetName())
}

// executeClearDeviationTx opens a connection to the dataserver,
// sends the TransactionSetRequest, and confirms on success.
func executeClearDeviationTx(
	ctx context.Context,
	txReq *sdcpb.TransactionSetRequest,
) (*sdcpb.TransactionSetResponse, error) {
	cfg := &dsclient.Config{
		Address:  dsclient.GetDataServerAddress(),
		Insecure: true,
	}
	dsClient, closeFn, err := dsclient.NewEphemeral(ctx, cfg)
	if err != nil {
		return nil, err
	}
	defer func() {
		if err := closeFn(); err != nil {
			log.FromContext(ctx).Error("failed to close connection", "error", err)
		}
	}()

	rsp, err := dsClient.TransactionSet(ctx, txReq)
	if err != nil {
		return rsp, err
	}

	// Confirm the transaction
	if _, err := dsClient.TransactionConfirm(ctx, &sdcpb.TransactionConfirmRequest{
		DatastoreName: txReq.DatastoreName,
		TransactionId: txReq.TransactionId,
	}); err != nil {
		return rsp, fmt.Errorf("transaction confirm failed: %w", err)
	}

	return rsp, nil
}

// buildClearDeviationStatus assembles the response status from the
// transaction response and any error.
func buildClearDeviationStatus(
	clearConfigs []TargetClearDeviationConfig,
	configsByName map[string]*Config,
	rsp *sdcpb.TransactionSetResponse,
	err error,
) *TargetClearDeviationStatus {
	status := &TargetClearDeviationStatus{}

	if err != nil {
		status.Message = err.Error()
	}

	if rsp == nil {
		for _, cfg := range clearConfigs {
			status.Results = append(status.Results, TargetClearDeviationConfigResult{
				Name:    cfg.Name,
				Success: false,
				Errors:  []string{status.Message},
			})
		}
		return status
	}

	status.Warnings = rsp.Warnings

	intentToName := make(map[string]string, len(configsByName))
	for name, cfg := range configsByName {
		intentToName[GetGVKNSN(cfg)] = name
	}

	responded := make(map[string]bool, len(rsp.Intents))
	for intentKey, intent := range rsp.Intents {
		name := intentKey
		if mapped, ok := intentToName[intentKey]; ok {
			name = mapped
		}
		responded[name] = true
		status.Results = append(status.Results, TargetClearDeviationConfigResult{
			Name:    name,
			Success: len(intent.Errors) == 0,
			Errors:  intent.Errors,
		})
	}

	// Inherit the overall transaction outcome for any config the
	// data-server didn't explicitly report on.
	for _, cfg := range clearConfigs {
		if responded[cfg.Name] {
			continue
		}
		status.Results = append(status.Results, TargetClearDeviationConfigResult{
			Name:    cfg.Name,
			Success: err == nil,
		})
	}

	return status
}

// GetIntentUpdateFromBlobs builds gRPC Update messages from the provided
// pre-resolved blobs instead of reading from cfg.Spec.Config.
// Used when blobs come from a decrypted SensitiveConfig payload.
func GetIntentUpdateFromBlobs(blobs []ConfigBlob) ([]*sdcpb.Update, error) {
    return buildUpdates(blobs)
}

// buildUpdates is the shared implementation used by both functions.
func buildUpdates(blobs []ConfigBlob) ([]*sdcpb.Update, error) {
    updates := make([]*sdcpb.Update, 0, len(blobs))
    for _, blob := range blobs {
        path, err := sdcpb.ParsePath(blob.Path)
        if err != nil {
            return nil, err
        }
        updates = append(updates, &sdcpb.Update{
            Path: path,
            Value: &sdcpb.TypedValue{
                Value: &sdcpb.TypedValue_JsonVal{
                    JsonVal: blob.Value.Raw,
                },
            },
        })
    }
    return updates, nil
}