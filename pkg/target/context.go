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

package target

import (
	"context"
	errors "errors"
	"fmt"
	"strings"
	"sync"

	"github.com/henderiw/apiserver-store/pkg/storebackend"
	"github.com/henderiw/logger/log"
	"github.com/openconfig/gnmic/pkg/cache"
	"github.com/prometheus/prometheus/prompb"
	"github.com/sdcio/config-server/apis/config"
	invv1alpha1 "github.com/sdcio/config-server/apis/inv/v1alpha1"
	dsclient "github.com/sdcio/config-server/pkg/sdc/dataserver/client"
	"github.com/sdcio/data-server/pkg/utils"
	sdcpb "github.com/sdcio/sdc-protos/sdcpb"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/encoding/prototext"
	"google.golang.org/protobuf/proto"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

type Context struct {
	targetKey        storebackend.Key
	client           client.Client // k8s client
	dsclient         dsclient.Client
	deviationWatcher *DeviationWatcher
	// Subscription parameters
	collector     *Collector
	subscriptions *Subscriptions

	m            sync.RWMutex
	ready        bool
	datastoreReq *sdcpb.CreateDataStoreRequest
}

func New(targetKey storebackend.Key, client client.Client, dsclient dsclient.Client) *Context {
	subscriptions := NewSubscriptions()
	return &Context{
		targetKey:        targetKey,
		client:           client,
		dsclient:         dsclient,
		deviationWatcher: NewDeviationWatcher(targetKey, client, dsclient),
		collector:        NewCollector(targetKey, subscriptions),
		subscriptions:    subscriptions,
		//cache:            cache.New([]string{}, cache.WithLogging(logging.NewLogrLogger())),
	}
}

func (r *Context) getDatastoreReq() *sdcpb.CreateDataStoreRequest {
	r.m.RLock()
	defer r.m.RUnlock()
	if r.datastoreReq == nil {
		return nil
	}
	clone := proto.Clone(r.datastoreReq)
	return clone.(*sdcpb.CreateDataStoreRequest)
}

func (r *Context) setDatastoreReq(req *sdcpb.CreateDataStoreRequest) {
	r.m.Lock()
	defer r.m.Unlock()
	r.datastoreReq = req
}

func (r *Context) getReady() bool {
	r.m.RLock()
	defer r.m.RUnlock()
	return r.ready
}

func (r *Context) setReady(b bool) {
	r.m.Lock()
	defer r.m.Unlock()
	r.ready = b
}

func (r *Context) GetDSClient() dsclient.Client {
	return r.dsclient
}

func (r *Context) GetAddress() string {
	if r.dsclient != nil {
		return r.dsclient.GetAddress()
	}
	return ""
}

func (r *Context) GetSchema() *config.ConfigStatusLastKnownGoodSchema {
	req := r.getDatastoreReq()
	if req == nil {
		return &config.ConfigStatusLastKnownGoodSchema{}
	}
	return &config.ConfigStatusLastKnownGoodSchema{
		Type:    req.Schema.Name,
		Vendor:  req.Schema.Vendor,
		Version: req.Schema.Version,
	}
}

func (r *Context) DeleteDS(ctx context.Context) error {
	log := log.FromContext(ctx).With("targetKey", r.targetKey.String())
	r.SetNotReady(ctx)
	r.setDatastoreReq(nil)
	rsp, err := r.deleteDataStore(ctx, &sdcpb.DeleteDataStoreRequest{Name: r.targetKey.String()})
	if err != nil {
		log.Error("cannot delete datstore in dataserver", "error", err)
		return err
	}
	log.Debug("delete datastore succeeded", "resp", prototext.Format(rsp))
	return nil
}

func (r *Context) CreateDS(ctx context.Context, datastoreReq *sdcpb.CreateDataStoreRequest) error {
	log := log.FromContext(ctx).With("targetKey", r.targetKey.String())
	rsp, err := r.createDataStore(ctx, datastoreReq)
	if err != nil {
		log.Error("cannot create datastore in dataserver", "error", err)
		return err
	}
	r.setDatastoreReq(datastoreReq)
	// will also create the deviation watcher and collector
	r.SetReady(ctx)

	// The collector is not started when a datastore is created but when a subscription is received.
	log.Debug("create datastore succeeded", "resp", prototext.Format(rsp))
	return nil
}

func (r *Context) IsReady() bool {
	if r == nil {
		return false
	}
	if r.client != nil && r.getDatastoreReq() != nil && r.getReady() {
		return true
	}
	return false
}

func (r *Context) SetNotReady(ctx context.Context) {
	r.setReady(false)
	if r.deviationWatcher != nil {
		r.deviationWatcher.Stop(ctx)
	}
	if r.collector != nil {
		r.collector.Stop(ctx)
	}
}

func (r *Context) SetReady(ctx context.Context) {
	log := log.FromContext(ctx)
	r.setReady(true)
	if r.deviationWatcher != nil {
		r.deviationWatcher.Start(ctx)
	}
	if r.subscriptions.HasSubscriptions() && r.collector != nil && !r.collector.IsRunning() {
		req := r.getDatastoreReq()
		if r.IsReady() && req != nil && req.Target != nil {
			if err := r.collector.Start(ctx, r.getDatastoreReq()); err != nil {
				log.Error("setready starting collector failed", "err", err)
			}
		}
	}
}

func (r *Context) deleteDataStore(ctx context.Context, in *sdcpb.DeleteDataStoreRequest, opts ...grpc.CallOption) (*sdcpb.DeleteDataStoreResponse, error) {
	if r == nil {
		return nil, fmt.Errorf("datastore client not initialized")
	}
	if r.dsclient == nil {
		return nil, fmt.Errorf("datastore client not initialized")
	}
	return r.dsclient.DeleteDataStore(ctx, in, opts...)
}

func (r *Context) createDataStore(ctx context.Context, in *sdcpb.CreateDataStoreRequest, opts ...grpc.CallOption) (*sdcpb.CreateDataStoreResponse, error) {
	if r == nil {
		return nil, fmt.Errorf("datastore client not initialized")
	}
	if r.dsclient == nil {
		return nil, fmt.Errorf("datastore client not initialized")
	}
	return r.dsclient.CreateDataStore(ctx, in, opts...)
}

func (r *Context) GetDataStore(ctx context.Context, in *sdcpb.GetDataStoreRequest, opts ...grpc.CallOption) (*sdcpb.GetDataStoreResponse, error) {
	if r == nil {
		return nil, fmt.Errorf("datastore client not initialized")
	}
	if r.dsclient == nil {
		return nil, fmt.Errorf("datastore client not initialized")
	}
	return r.dsclient.GetDataStore(ctx, in, opts...)
}

// useSpec indicates to use the spec as the confifSpec, typically set to true; when set to false it means we are recovering
// the config
func (r *Context) getIntentUpdate(ctx context.Context, key storebackend.Key, config *config.Config, useSpec bool) ([]*sdcpb.Update, error) {
	log := log.FromContext(ctx)
	update := make([]*sdcpb.Update, 0, len(config.Spec.Config))
	configSpec := config.Spec.Config
	if !useSpec && config.Status.AppliedConfig != nil {
		update = make([]*sdcpb.Update, 0, len(config.Status.AppliedConfig.Config))
		configSpec = config.Status.AppliedConfig.Config
	}

	for _, config := range configSpec {
		path, err := utils.ParsePath(config.Path)
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

func (r *Context) TransactionSet(ctx context.Context, req *sdcpb.TransactionSetRequest) (string, error) {
	rsp, err := r.dsclient.TransactionSet(ctx, req)
	msg, err := r.processTransactionResponse(ctx, rsp, err)
	if err != nil {
		return msg, err
	}
	// Assumption: if no error this succeeded, if error this is providing the error code and the info can be
	// retrieved from the individual intents
	if _, err := r.dsclient.TransactionConfirm(ctx, &sdcpb.TransactionConfirmRequest{
		DatastoreName: req.DatastoreName,
		TransactionId: req.TransactionId,
	}); err != nil {
		return msg, err
	}
	return msg, nil
}

func (r *Context) SetIntent(ctx context.Context, key storebackend.Key, config *config.Config, dryRun bool) (string, error) {
	log := log.FromContext(ctx).With("target", key.String(), "intent", getGVKNSN(config))
	if !r.IsReady() {
		return "", fmt.Errorf("target context not ready")
	}

	update, err := r.getIntentUpdate(ctx, key, config, true)
	if err != nil {
		return "", err
	}
	log.Debug("SetIntent", "update", update)

	return r.TransactionSet(ctx, &sdcpb.TransactionSetRequest{
		TransactionId: getGVKNSN(config),
		DatastoreName: key.String(),
		DryRun:        dryRun,
		Timeout:       ptr.To(int32(60)),
		Intents: []*sdcpb.TransactionIntent{
			{
				Intent:   getGVKNSN(config),
				Priority: int32(config.Spec.Priority),
				Update:   update,
			},
		},
	})
}

func (r *Context) DeleteIntent(ctx context.Context, key storebackend.Key, config *config.Config, dryRun bool) (string, error) {
	log := log.FromContext(ctx).With("target", key.String(), "intent", getGVKNSN(config))
	if !r.IsReady() {
		return "", fmt.Errorf("target context not ready")
	}

	if config.Status.AppliedConfig == nil {
		log.Debug("delete intent was never applied")
		return "", nil
	}

	return r.TransactionSet(ctx, &sdcpb.TransactionSetRequest{
		TransactionId: getGVKNSN(config),
		DatastoreName: key.String(),
		DryRun:        dryRun,
		Timeout:       ptr.To(int32(60)),
		Intents: []*sdcpb.TransactionIntent{
			{
				Intent:   getGVKNSN(config),
				Priority: int32(config.Spec.Priority),
				Delete:   true,
				Orphan:   config.Orphan(),
			},
		},
	})
}

func (r *Context) RecoverIntents(ctx context.Context, key storebackend.Key, configs []*config.Config) (string, error) {
	log := log.FromContext(ctx).With("target", key.String())
	if !r.IsReady() {
		return "", fmt.Errorf("target context not ready")
	}

	if len(configs) == 0 {
		return "", nil
	}

	intents := make([]*sdcpb.TransactionIntent, 0, len(configs))
	for _, config := range configs {
		update, err := r.getIntentUpdate(ctx, key, config, false)
		if err != nil {
			return "", err
		}
		intents = append(intents, &sdcpb.TransactionIntent{
			Intent:   getGVKNSN(config),
			Priority: int32(config.Spec.Priority),
			Update:   update,
		})
	}

	log.Debug("device intent recovery")

	return r.TransactionSet(ctx, &sdcpb.TransactionSetRequest{
		TransactionId: "recovery",
		DatastoreName: key.String(),
		DryRun:        false,
		Timeout:       ptr.To(int32(120)),
		Intents:       intents,
	})
}

func (r *Context) SetIntents(ctx context.Context, key storebackend.Key, transactionID string, configs, deleteConfigs []*config.Config, dryRun bool) (string, error) {
	log := log.FromContext(ctx).With("target", key.String(), "transactionID", transactionID)
	if !r.IsReady() {
		return "", fmt.Errorf("target context not ready")
	}

	intents := make([]*sdcpb.TransactionIntent, len(configs)+len(deleteConfigs))
	for i, config := range configs {
		update, err := r.getIntentUpdate(ctx, key, config, true)
		if err != nil {
			return "", err
		}
		intents[i] = &sdcpb.TransactionIntent{
			Intent:   getGVKNSN(config),
			Priority: int32(config.Spec.Priority),
			Update:   update,
		}
	}
	for i, config := range deleteConfigs {
		intents[len(configs)+i] = &sdcpb.TransactionIntent{
			Intent:   getGVKNSN(config),
			Priority: int32(config.Spec.Priority),
			Delete:   true,
		}
	}

	log.Debug("delete intents", "total update", len(configs), "total delete", len(deleteConfigs))

	rsp, err := r.dsclient.TransactionSet(ctx, &sdcpb.TransactionSetRequest{
		TransactionId: transactionID,
		DatastoreName: key.String(),
		DryRun:        dryRun,
		Timeout:       ptr.To(int32(60)),
		Intents:       intents,
	})
	return r.processTransactionResponse(ctx, rsp, err)
}

func (r *Context) Confirm(ctx context.Context, key storebackend.Key, transactionID string) error {
	log := log.FromContext(ctx).With("target", key.String(), "transactionID", transactionID)
	if !r.IsReady() {
		return fmt.Errorf("target context not ready")
	}
	log.Debug("cancel transaction")
	_, err := r.dsclient.TransactionConfirm(ctx, &sdcpb.TransactionConfirmRequest{
		TransactionId: transactionID,
		DatastoreName: key.String(),
	})
	return err
}

func (r *Context) Cancel(ctx context.Context, key storebackend.Key, transactionID string) error {
	log := log.FromContext(ctx).With("target", key.String(), "transactionID", transactionID)
	if !r.IsReady() {
		return fmt.Errorf("target context not ready")
	}
	log.Debug("cancel transaction")
	_, err := r.dsclient.TransactionCancel(ctx, &sdcpb.TransactionCancelRequest{
		TransactionId: transactionID,
		DatastoreName: key.String(),
	})
	return err
}

// processTransactionResponse returns the warnings as a string and aggregates the errors in a single error and classifies them
// as recoverable or non recoverable.
func (r *Context) processTransactionResponse(ctx context.Context, rsp *sdcpb.TransactionSetResponse, rsperr error) (string, error) {
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

func (r *Context) GetData(ctx context.Context, key storebackend.Key) (*config.RunningConfig, error) {
	log := log.FromContext(ctx).With("target", key.String())
	if !r.IsReady() {
		return nil, fmt.Errorf("target context not ready")
	}

	rsp, err := r.dsclient.GetIntent(ctx, &sdcpb.GetIntentRequest{
		DatastoreName: key.String(),
		Intent:        "running",
		Format:        sdcpb.Format_Intent_Format_JSON,
	})
	if err != nil {
		log.Error("get data failed", "error", err.Error())
		return nil, err
	}

	return config.BuildRunningConfig(
		metav1.ObjectMeta{
			Name:      key.Name,
			Namespace: key.Namespace,
		},
		config.RunningConfigSpec{},
		config.RunningConfigStatus{
			Value: runtime.RawExtension{
				Raw: rsp.GetBlob(),
			},
		},
	), nil
}

func (r *Context) DeleteSubscription(ctx context.Context, sub *invv1alpha1.Subscription) error {
	log := log.FromContext(ctx).With("targetKey", r.targetKey.String())
	if err := r.subscriptions.DelSubscription(sub); err != nil {
		return err
	}
	log.Debug("deleteSubscription", "hasSubscriptions", r.subscriptions.HasSubscriptions(), "paths", r.subscriptions.GetPaths())
	// if we have no longer subscriptions we stop the collector
	if r.collector != nil && r.collector.IsRunning() && !r.subscriptions.HasSubscriptions() {
		r.collector.Stop(ctx)
		return nil
	}
	if r.collector != nil && r.collector.IsRunning() {
		r.updateSubscription()
	}
	return nil
}

func (r *Context) updateSubscription() {
	// other subscriptions exist, update the subscription
	subCh := r.collector.GetUpdateChan()
	subCh <- struct{}{}
}

func (r *Context) UpsertSubscription(ctx context.Context, sub *invv1alpha1.Subscription) error {
	// This should change to the target context and discovery
	if sub.Spec.Protocol != invv1alpha1.Protocol_GNMI {
		return fmt.Errorf("subscriptions only supported using gnmi, got %s", string(sub.Spec.Protocol))
	}
	r.collector.SetPort(uint(sub.Spec.Port))
	if err := r.subscriptions.AddSubscription(sub); err != nil {
		return err
	}
	// above should be changed to subscription profile in the target

	if r.collector != nil && !r.collector.IsRunning() {
		req := r.getDatastoreReq()
		if r.IsReady() && req != nil && req.Target != nil {
			// starting a collector also updates the subscriptions
			if err := r.collector.Start(ctx, req); err != nil {
				return err
			}
		}
		return nil
	}

	r.updateSubscription()
	return nil
}

func (r *Context) GetCache() cache.Cache {
	return r.collector.cache
}

func (r *Context) GetPrombLabels() []prompb.Label {
	labels := make([]prompb.Label, 0)
	labels = append(labels, prompb.Label{Name: "target_name", Value: r.targetKey.String()})

	req := r.getDatastoreReq()
	if req != nil {
		labels = append(labels, prompb.Label{Name: "vendor", Value: req.Schema.Vendor})
		labels = append(labels, prompb.Label{Name: "version", Value: req.Schema.Version})
		labels = append(labels, prompb.Label{Name: "address", Value: req.Target.Address})
	}
	return labels
}
