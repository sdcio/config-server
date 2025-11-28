package handlers

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/henderiw/logger/log"
	"github.com/sdcio/config-server/apis/condition"
	"github.com/sdcio/config-server/apis/config"
	invv1alpha1 "github.com/sdcio/config-server/apis/inv/v1alpha1"
	dsclient "github.com/sdcio/config-server/pkg/sdc/dataserver/client"
	sdcpb "github.com/sdcio/sdc-protos/sdcpb"
	"google.golang.org/protobuf/encoding/prototext"
	"k8s.io/apimachinery/pkg/api/meta"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

const DataServerAddress = "data-server-0.schema-server.sdc-system.svc.cluster.local:56000"

type ConfigStoreHandler struct {
	Client      client.Client
}

func GetGVKNSN(obj client.Object) string {
	return fmt.Sprintf("%s.%s", obj.GetNamespace(), obj.GetName())
}


func (r *ConfigStoreHandler) DryRunCreateFn(ctx context.Context, key types.NamespacedName, obj runtime.Object, dryrun bool) (runtime.Object, error) {
	accessor, err := meta.Accessor(obj)
	if err != nil {
		return obj, err
	}
	if _, err := config.GetTargetKey(accessor.GetLabels()); err != nil {
		return obj, err
	}
	c := obj.(*config.Config)

	target := &invv1alpha1.Target{}
	if err := r.Client.Get(ctx, key, target); err != nil {
		return nil, err
	}

	if !target.IsReady() {
		return nil, fmt.Errorf("target not ready %s", key)
	}

	cfg := &dsclient.Config{
		Address:  DataServerAddress,
		Insecure: true,
	}

	dsclient, closeFn, err := dsclient.NewEphemeral(ctx, cfg)
	if err != nil {
		return nil, err
	}
	defer closeFn()

	intent_summary := map[string]bool{}
	intents := []*sdcpb.TransactionIntent{}
	update, err := getIntentUpdate(ctx, key, c)
	if err != nil {
		return nil, err
	}
	intent_summary[GetGVKNSN(c)] = true
	intents = append(intents, &sdcpb.TransactionIntent{
		Intent:   GetGVKNSN(c),
		Priority: int32(c.Spec.Priority),
		Update:   update,
	})

	// check if the schema exists; this is == nil check; in case of err it does not exist
	rsp, err := dsclient.TransactionSet(ctx, &sdcpb.TransactionSetRequest{
		TransactionId: GetGVKNSN(c),
		DatastoreName: key.String(),
		DryRun:        true,
		Timeout:       ptr.To(int32(60)),
		Intents:       intents,
	})
	if err == nil {
		return nil, err
	}

	warnings, err := processTransactionResponse(ctx, rsp, err)
	if err != nil {
		msg := fmt.Sprintf("%s err %s", warnings, err.Error())
		c.SetConditions(condition.Failed(msg))
		return c, err
	}
	
	c.SetConditions(condition.ReadyWithMsg(warnings))
	c.Status.LastKnownGoodSchema = &config.ConfigStatusLastKnownGoodSchema{
		Vendor: target.Status.DiscoveryInfo.Provider,
		Version: target.Status.DiscoveryInfo.Version,
	}
	c.Status.AppliedConfig = &c.Spec
	return c, nil
}
func (r *ConfigStoreHandler) DryRunUpdateFn(ctx context.Context, key types.NamespacedName, obj, old runtime.Object, dryrun bool) (runtime.Object, error) {
	accessor, err := meta.Accessor(obj)
	if err != nil {
		return obj, err
	}
	if _, err := config.GetTargetKey(accessor.GetLabels());  err != nil {
		return obj, err
	}
	c := obj.(*config.Config)
	target := &invv1alpha1.Target{}
	if err := r.Client.Get(ctx, key, target); err != nil {
		return nil, err
	}

	if !target.IsReady() {
		return nil, fmt.Errorf("target not ready %s", key)
	}

	cfg := &dsclient.Config{
		Address:  DataServerAddress,
		Insecure: true,
	}

	dsclient, closeFn, err := dsclient.NewEphemeral(ctx, cfg)
	if err != nil {
		return nil, err
	}
	defer closeFn()

	intent_summary := map[string]bool{}
	intents := []*sdcpb.TransactionIntent{}
	update, err := getIntentUpdate(ctx, key, c)
	if err != nil {
		return nil, err
	}
	intent_summary[GetGVKNSN(c)] = true
	intents = append(intents, &sdcpb.TransactionIntent{
		Intent:   GetGVKNSN(c),
		Priority: int32(c.Spec.Priority),
		Update:   update,
	})

	// check if the schema exists; this is == nil check; in case of err it does not exist
	rsp, err := dsclient.TransactionSet(ctx, &sdcpb.TransactionSetRequest{
		TransactionId: GetGVKNSN(c),
		DatastoreName: key.String(),
		DryRun:        true,
		Timeout:       ptr.To(int32(60)),
		Intents:       intents,
	})
	if err == nil {
		return nil, err
	}

	warnings, err := processTransactionResponse(ctx, rsp, err)
	if err != nil {
		msg := fmt.Sprintf("%s err %s", warnings, err.Error())
		c.SetConditions(condition.Failed(msg))
		return c, err
	}
	
	c.SetConditions(condition.ReadyWithMsg(warnings))
	c.Status.LastKnownGoodSchema = &config.ConfigStatusLastKnownGoodSchema{
		Vendor: target.Status.DiscoveryInfo.Provider,
		Version: target.Status.DiscoveryInfo.Version,
	}
	c.Status.AppliedConfig = &c.Spec
	return c, nil
}
func (r *ConfigStoreHandler) DryRunDeleteFn(ctx context.Context, key types.NamespacedName, obj runtime.Object, dryrun bool) (runtime.Object, error) {
	accessor, err := meta.Accessor(obj)
	if err != nil {
		return obj, err
	}
	if _, err := config.GetTargetKey(accessor.GetLabels()); err != nil {
		return obj, err
	}
	c := obj.(*config.Config)
	target := &invv1alpha1.Target{}
	if err := r.Client.Get(ctx, key, target); err != nil {
		return nil, err
	}

	if !target.IsReady() {
		return nil, fmt.Errorf("target not ready %s", key)
	}

	cfg := &dsclient.Config{
		Address:  DataServerAddress,
		Insecure: true,
	}

	dsclient, closeFn, err := dsclient.NewEphemeral(ctx, cfg)
	if err != nil {
		return nil, err
	}
	defer closeFn()

	intent_summary := map[string]bool{}
	intents := []*sdcpb.TransactionIntent{}
	intent_summary[GetGVKNSN(c)] = true
	intents = append(intents, &sdcpb.TransactionIntent{
		Intent:   GetGVKNSN(c),
		Delete:   true,
	})

	// check if the schema exists; this is == nil check; in case of err it does not exist
	rsp, err := dsclient.TransactionSet(ctx, &sdcpb.TransactionSetRequest{
		TransactionId: GetGVKNSN(c),
		DatastoreName: key.String(),
		DryRun:        true,
		Timeout:       ptr.To(int32(60)),
		Intents:       intents,
	})
	if err == nil {
		return nil, err
	}

	warnings, err := processTransactionResponse(ctx, rsp, err)
	if err != nil {
		msg := fmt.Sprintf("%s err %s", warnings, err.Error())
		c.SetConditions(condition.Failed(msg))
		return c, err
	}
	
	c.SetConditions(condition.ReadyWithMsg(warnings))
	c.Status.LastKnownGoodSchema = &config.ConfigStatusLastKnownGoodSchema{
		Vendor: target.Status.DiscoveryInfo.Provider,
		Version: target.Status.DiscoveryInfo.Version,
	}
	c.Status.AppliedConfig = &c.Spec
	return c, nil
}



func getIntentUpdate(ctx context.Context, key types.NamespacedName, config *config.Config) ([]*sdcpb.Update, error) {
	log := log.FromContext(ctx)
	update := make([]*sdcpb.Update, 0, len(config.Spec.Config))
	configSpec := config.Spec.Config

	for _, config := range configSpec {
		path, err := sdcpb.ParsePath(config.Path)
		if err != nil {
			return nil, fmt.Errorf("create data failed for target %s, path %s invalid", key.String(), config.Path)
		}
		log.Debug("setIntent", "configSpec", string(config.Value.Raw))
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
	if rsperr != nil {
		errs = errors.Join(errs, fmt.Errorf("error: %s", rsperr.Error()))
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
	var msg string	
	if len(collectedWarnings) > 0 {
		msg = strings.Join(collectedWarnings, "; ")
	}
	log.Debug("transaction response", "rsp", prototext.Format(rsp), "msg", msg, "error", errs)
	return msg, errs
}
