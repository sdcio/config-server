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
	"encoding/json"

	"github.com/henderiw/apiserver-store/pkg/storebackend"
	"github.com/henderiw/logger/log"
	"github.com/sdcio/config-server/apis/config"
	configv1alpha1 "github.com/sdcio/config-server/apis/config/v1alpha1"
	"github.com/sdcio/config-server/pkg/keyring"
	sdcpb "github.com/sdcio/sdc-protos/sdcpb"
	"k8s.io/utils/ptr"
)

// RecoverConfigs replays the last successfully applied resolved config to the
// datastore after a restart.
// The TargetSnapshot is the sole source of truth — it contains the exact
// resolved blobs that were last sent to the datastore.
// Configs not present in the snapshot were never successfully applied and
// are skipped; the normal reconcile loop will apply them once the target recovers.
func RecoverConfigs(
	ctx        context.Context,
	target     *configv1alpha1.Target,
	dsctx      *DatastoreHandle,
	transactor *Transactor,
	cfgMgr     *ConfigManager,
	kr         *keyring.KeyRing,
	snapshot   *configv1alpha1.TargetSnapshot,
) (*string, error) {
	log := log.FromContext(ctx)
	log.Debug("RecoverConfigs")

	// No snapshot entries — nothing was ever successfully applied, nothing to recover.
	if len(snapshot.Spec.Configs) == 0 {
		log.Info("no snapshot entries, skipping recovery")
		dsctx.MarkRecovered(true)
		return nil, nil
	}

	// Fetch live Config objects to get current metadata (IsRevertive, etc.).
	cfgList, err := cfgMgr.ListConfigsPerTarget(ctx, target)
	if err != nil {
		return nil, err
	}

	cfgByName := make(map[string]*config.Config, len(cfgList.Items))
	for i := range cfgList.Items {
		cfgByName[cfgList.Items[i].Name] = &cfgList.Items[i]
	}

	// Recover only configs present in the snapshot — those were last successfully applied.
	// Configs missing from the snapshot were never applied; they will be handled
	// by the normal reconcile loop after recovery completes.
	toRecover := make([]*config.Config, 0, len(snapshot.Spec.Configs))
	for name := range snapshot.Spec.Configs {
		if cfg, exists := cfgByName[name]; exists {
			toRecover = append(toRecover, cfg)
		} else {
			log.Info("snapshot entry has no matching Config, skipping", "config", name)
		}
	}

	log.Info("recovering target config", "count", len(toRecover))

	targetKey := storebackend.KeyFromNSN(target.GetNamespacedName())
	msg, err := recoverIntents(ctx, transactor, dsctx, targetKey, toRecover, snapshot, kr)
	if err != nil {
		if condErr := cfgMgr.SetConfigsTargetConditionForTarget(ctx, target,
			configv1alpha1.ConfigFailed(msg),
		); condErr != nil {
			return ptr.To("recovered, but failed to update config status"), condErr
		}
		return &msg, err
	}

	dsctx.MarkRecovered(true)
	log.Debug("recovered configs", "count", len(toRecover))

	if err := cfgMgr.SetConfigsTargetConditionForTarget(ctx, target,
		configv1alpha1.ConfigReady("target recovered"),
	); err != nil {
		return ptr.To("recovered, but failed to update config status"), err
	}
	return nil, nil
}

func recoverIntents(
	ctx        context.Context,
	transactor *Transactor,
	dsctx      *DatastoreHandle,
	key        storebackend.Key,
	configs    []*config.Config,
	snapshot   *configv1alpha1.TargetSnapshot,
	kr         *keyring.KeyRing,
) (string, error) {
	log := log.FromContext(ctx).With("target", key.String())

	if len(configs) == 0 {
		return "", nil
	}

	intents := make([]*sdcpb.TransactionIntent, 0, len(configs))

	for _, cfg := range configs {
		snapEntry := snapshot.Spec.Configs[cfg.Name] // always present — we built the list from snapshot

		var blobs []config.ConfigBlob

		if snapEntry.Payload.KeyID != "" && len(snapEntry.Payload.Data) > 0 {
			// Encrypted blobs — decrypt and unmarshal as internal type.
			plain, err := kr.Decrypt(snapEntry.Payload)
			if err != nil {
				log.Warn("cannot decrypt snapshot entry, skipping config", "config", cfg.Name, "err", err)
				continue
			}
			if err := json.Unmarshal(plain, &blobs); err != nil {
				log.Warn("cannot unmarshal snapshot blobs, skipping config", "config", cfg.Name, "err", err)
				continue
			}
		} else {
			// No-secret config (KeyID == "") — resolved blobs == original spec blobs.
			blobs = cfg.Spec.Config
		}

		update, err := config.GetIntentUpdateFromBlobs(blobs)
		if err != nil {
			return "", err
		}

		intents = append(intents, &sdcpb.TransactionIntent{
			Intent:            config.GetGVKNSN(cfg),
			Priority:          int32(snapEntry.Priority), // use priority from snapshot, not live spec
			Update:            update,
			NonRevertive:      cfg.Spec.Revertive != nil && !*cfg.Spec.Revertive,
			PreviouslyApplied: true,
		})
	}

	log.Debug("device intent recovery", "count", len(intents))

	return transactor.TransactionSet(ctx, dsctx, &sdcpb.TransactionSetRequest{
		TransactionId: "recovery",
		DatastoreName: key.String(),
		DryRun:        false,
		Timeout:       ptr.To(int32(120)),
		Intents:       intents,
	})
}