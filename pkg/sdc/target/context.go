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
	"fmt"
	"sync"
	"log/slog"

	"strconv"

	"github.com/henderiw/apiserver-store/pkg/storebackend"
	"github.com/henderiw/logger/log"
	"github.com/openconfig/gnmic/pkg/cache"
	"github.com/prometheus/prometheus/prompb"
	"github.com/sdcio/config-server/apis/config"
	invv1alpha1 "github.com/sdcio/config-server/apis/inv/v1alpha1"
	dsclient "github.com/sdcio/config-server/pkg/sdc/dataserver/client"
	sdcpb "github.com/sdcio/sdc-protos/sdcpb"
	"google.golang.org/grpc"
	"google.golang.org/protobuf/encoding/prototext"
	"google.golang.org/protobuf/proto"
	"k8s.io/apimachinery/pkg/util/sets"
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

	m                           sync.RWMutex
	readyState                  bool
	recoveredTargetConfigsState bool
	datastoreReq                *sdcpb.CreateDataStoreRequest
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
	return r.readyState
}

func (r *Context) setCtxReady(b bool) {
	r.m.Lock()
	defer r.m.Unlock()
	r.readyState = b
	if !b {
		r.recoveredTargetConfigsState = false
	}
}

func (r *Context) setRecoveredConfigsState() {
	r.m.Lock()
	defer r.m.Unlock()
	r.recoveredTargetConfigsState = true
}

func (r *Context) getRecoveredTargetConfigsState() bool {
	r.m.RLock()
	defer r.m.RUnlock()
	return r.recoveredTargetConfigsState
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

func (r *Context) ds() (dsclient.Client, error) {
	if r == nil || r.dsclient == nil {
		return nil, fmt.Errorf("datastore client not initialized")
	}
	return r.dsclient, nil
}

func (r *Context) IsReady() bool {
	if r == nil {
		return false
	}
	return r.client != nil && r.getDatastoreReq() != nil && r.getReady()
}

func (r *Context) SetNotReady(ctx context.Context) {
	log := log.FromContext(ctx)
	log.Info("SetNotReady", "ready", r.getReady(), "recoveredConfigs", r.getRecoveredTargetConfigsState())
	r.setCtxReady(false)
	if r.deviationWatcher != nil {
		r.deviationWatcher.Stop(ctx)
	}
	if r.collector != nil {
		r.collector.Stop(ctx)
	}
}

func (r *Context) SetReady(ctx context.Context) {
	log := log.FromContext(ctx)
	log.Info("SetReady", "ready", r.getReady(), "recoveredConfigs", r.getRecoveredTargetConfigsState())

	if !r.getReady() {
		r.setCtxReady(true)
	}

	if r.deviationWatcher != nil {
		r.deviationWatcher.Start(ctx)
	}

	if r.collector == nil || r.collector.IsRunning() {
		return
	}

	if !r.subscriptions.HasSubscriptions() {
		return
	}

	req := r.getDatastoreReq()
	if req == nil || req.Target == nil {
		return
	}

	if err := r.collector.Start(ctx, req); err != nil {
		log.Error("setready starting collector failed", "err", err)
	}
}

func (r *Context) SetRecoveredConfigsState(ctx context.Context) {
	log := log.FromContext(ctx)
	r.setRecoveredConfigsState()

	if !r.IsReady() {
		log.Error("setting resource version and generation w/o target being ready")
		r.SetReady(ctx)
	}
}

func (r *Context) IsTargetConfigRecovered(ctx context.Context) bool {
	return r.getRecoveredTargetConfigsState()
}

func (r *Context) deleteDataStore(ctx context.Context, in *sdcpb.DeleteDataStoreRequest, opts ...grpc.CallOption) (*sdcpb.DeleteDataStoreResponse, error) {
	ds, err := r.ds()
	if err != nil {
		return nil, err
	}
	return ds.DeleteDataStore(ctx, in, opts...)
}

func (r *Context) createDataStore(ctx context.Context, in *sdcpb.CreateDataStoreRequest, opts ...grpc.CallOption) (*sdcpb.CreateDataStoreResponse, error) {
	ds, err := r.ds()
	if err != nil {
		return nil, err
	}
	return ds.CreateDataStore(ctx, in, opts...)
}

func (r *Context) GetDataStore(ctx context.Context, in *sdcpb.GetDataStoreRequest, opts ...grpc.CallOption) (*sdcpb.GetDataStoreResponse, error) {
	ds, err := r.ds()
	if err != nil {
		return nil, err
	}
	return ds.GetDataStore(ctx, in, opts...)
}


func (r *Context) TransactionSet(ctx context.Context, req *sdcpb.TransactionSetRequest) (string, error) {
	rsp, err := r.dsclient.TransactionSet(ctx, req)
	msg, err := processTransactionResponse(ctx, rsp, err)
	if err != nil {
		return msg, err
	}
	// Assumption: if no error this succeeded, if error this is providing the error code and the info can be
	// retrieved from the individual intents

	// For dryRun we don't have to confirm the transaction as the dataserver does not lock things.
	if req.DryRun {
		return msg, nil
	}

	if err := r.TransactionConfirm(ctx, req.DatastoreName, req.TransactionId); err != nil {
		return msg, err
	}
	return msg, nil
}

func (r *Context) RecoverIntents(
	ctx context.Context, 
	key storebackend.Key, configs []*config.Config, deviations []*config.Deviation) (string, error) {
	log := log.FromContext(ctx).With("target", key.String())
	if !r.IsReady() {
		return "", fmt.Errorf("target context not ready")
	}

	if len(configs) == 0 {
		return "", nil
	}

	intents := make([]*sdcpb.TransactionIntent, 0, len(configs)+len(deviations))
	// we only include deviations that have
	for _, deviation := range deviations {
		intent, err := buildDeviationIntent(ctx, log, key, deviation)
		if err != nil {
			return "", err
		}
		if intent != nil {
			intents = append(intents, intent)
		}
	}
	for _, config := range configs {
		update, err := GetIntentUpdate(ctx, key, config, false)
		if err != nil {
			return "", err
		}
		intents = append(intents, &sdcpb.TransactionIntent{
			Intent:   GetGVKNSN(config),
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

func (r *Context) SetIntents(
	ctx context.Context,
	targetKey storebackend.Key,
	transactionID string,
	configsToUpdate, configsToDelete map[string]*config.Config,
	deviationsToUpdate, deviationsToDelete map[string]*config.Deviation,
	dryRun bool,
) (*sdcpb.TransactionSetResponse, error) {
	log := log.FromContext(ctx).With("target", targetKey.String(), "transactionID", transactionID)
	log.Info("Transaction", "Ready", r.IsReady())
	if !r.IsReady() {
		return nil, fmt.Errorf("target context not ready")
	}

	configsToUpdateSet := sets.New[string]()
	configsToDeleteSet := sets.New[string]()
	deviationsToUpdateSet := sets.New[string]()
	deviationsToDeleteSet := sets.New[string]()

	intents := make([]*sdcpb.TransactionIntent, 0)
	for key, deviation := range deviationsToUpdate {
		deviationsToUpdateSet.Insert(key)
		intent, err := buildDeviationIntent(ctx, log, targetKey, deviation)
		if err != nil {
			log.Error("Transaction getDeviationUpdate deviation", "error", err.Error())
			return nil, err
		}
		if intent != nil {
			intents = append(intents, intent)
		}
	}

	for key, deviation := range deviationsToDelete {
		deviationsToDeleteSet.Insert(key)
		// only include items for which deviations exist
		intents = append(intents, &sdcpb.TransactionIntent{
			Intent: fmt.Sprintf("deviation:%s", GetGVKNSN(deviation)),
			//Priority: int32(config.Spec.Priority),
			Delete:              true,
			DeleteIgnoreNoExist: true,
		})
	}
	for key, config := range configsToUpdate {
		configsToUpdateSet.Insert(key)
		update, err := GetIntentUpdate(ctx, targetKey, config, true)
		if err != nil {
			log.Error("Transaction getIntentUpdate config", "error", err)
			return nil, err
		}
		intents = append(intents, &sdcpb.TransactionIntent{
			Intent:   GetGVKNSN(config),
			Priority: int32(config.Spec.Priority),
			Update:   update,
		})
	}
	for key, config := range configsToDelete {
		configsToDeleteSet.Insert(key)
		intents = append(intents, &sdcpb.TransactionIntent{
			Intent: GetGVKNSN(config),
			//Priority: int32(config.Spec.Priority),
			Delete:              true,
			DeleteIgnoreNoExist: true,
			Orphan:              config.Orphan(),
		})
	}

	log.Info("Transaction",
		"configsToUpdate total", len(configsToUpdate),
		"configsToUpdate names", configsToUpdateSet.UnsortedList(),
		"configsToDelete total", len(configsToDelete),
		"configsToDelete names", configsToDeleteSet.UnsortedList(),
		"deviationsToUpdate total", len(deviationsToUpdate),
		"deviationsToUpdate names", deviationsToUpdateSet.UnsortedList(),
		"deviationsToDelete total", len(deviationsToDelete),
		"deviationsToDelete names", deviationsToDeleteSet.UnsortedList(),
	)

	rsp, err := r.dsclient.TransactionSet(ctx, &sdcpb.TransactionSetRequest{
		TransactionId: transactionID,
		DatastoreName: targetKey.String(),
		DryRun:        dryRun,
		Timeout:       ptr.To(int32(60)),
		Intents:       intents,
	})
	if rsp != nil {
		log.Info("Transaction rsp", "rsp", prototext.Format(rsp))
	}
	return rsp, err
}

func buildDeviationIntent(
	ctx context.Context,
	log *slog.Logger,
	key storebackend.Key,
	deviation *config.Deviation,
) (*sdcpb.TransactionIntent, error) {
	updates, deletes, err := getDeviationUpdate(ctx, key, deviation)
	if err != nil {
		return nil, err
	}

	priorityStr := deviation.GetLabels()["priority"]
	if priorityStr == "" {
		return nil, fmt.Errorf("deviation %s has no priority label", GetGVKNSN(deviation))
	}

	newPriority, err := strconv.Atoi(priorityStr)
	if err != nil {
		return nil, fmt.Errorf("cannot convert priority to int: %w", err)
	}
	if newPriority > 0 {
		newPriority--
	}

	// Only include items for which deviations exist
	if len(updates) == 0 && len(deletes) == 0 {
		return nil, nil
	}

	return &sdcpb.TransactionIntent{
		Deviation: true,
		Intent:    fmt.Sprintf("deviation:%s", GetGVKNSN(deviation)),
		Priority:  int32(newPriority),
		Update:    updates,
		Deletes:   deletes,
	}, nil
}

func (r *Context) Confirm(ctx context.Context, key storebackend.Key, transactionID string) error {
	log := log.FromContext(ctx).With("target", key.String(), "transactionID", transactionID)
	if !r.IsReady() {
		return fmt.Errorf("target context not ready")
	}
	log.Debug("cancel transaction")
	return r.TransactionConfirm(ctx, key.String(), transactionID)
}

func (r *Context) TransactionConfirm(ctx context.Context, datastoreName, transactionID string) error {
	_, err := r.dsclient.TransactionConfirm(ctx, &sdcpb.TransactionConfirmRequest{
		DatastoreName: datastoreName,
		TransactionId: transactionID,
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
    labels := make([]prompb.Label, 0, 4)
    labels = append(labels, prompb.Label{Name: "target_name", Value: r.targetKey.String()})

    if req := r.getDatastoreReq(); req != nil && req.Target != nil {
        labels = append(labels,
            prompb.Label{Name: "vendor", Value: req.Schema.Vendor},
            prompb.Label{Name: "version", Value: req.Schema.Version},
            prompb.Label{Name: "address", Value: req.Target.Address},
        )
    }
    return labels
}
