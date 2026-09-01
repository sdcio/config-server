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

package configread

import (
	"context"
	"encoding/json"

	configv1alpha1 "github.com/sdcio/config-server/apis/config/v1alpha1"
	"github.com/sdcio/sdc-protos/config_read"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// Get returns the last-applied value for a single intent, scoped to the
// requested target: the resolved state targetconfig's reconciler last
// confirmed was successfully pushed to the device, read off that target's
// TargetSnapshot. Looking the TargetSnapshot up by {target namespace, target
// name} makes the lookup key the target's identity, so there's no separate
// object that could belong to the wrong target.
func (s *Server) Get(ctx context.Context, req *config_read.GetConfigRequest) (*config_read.GetConfigResponse, error) {
	if req.GetTargetNamespace() == "" || req.GetTargetName() == "" || req.GetName() == "" {
		return nil, status.Error(codes.InvalidArgument, "target_namespace, target_name and name are required")
	}

	notFoundKey := types.NamespacedName{Namespace: req.GetTargetNamespace(), Name: req.GetName()}

	targetKey := types.NamespacedName{Namespace: req.GetTargetNamespace(), Name: req.GetTargetName()}
	snapshot := &configv1alpha1.TargetSnapshot{}
	if err := s.client.Get(ctx, targetKey, snapshot); err != nil {
		if apierrors.IsNotFound(err) {
			return nil, notFoundConfig(notFoundKey)
		}
		return nil, status.Errorf(codes.Internal, "get targetsnapshot %s/%s: %v", targetKey.Namespace, targetKey.Name, err)
	}

	spec, ok := snapshot.Spec.Configs[req.GetName()]
	if !ok {
		return nil, notFoundConfig(notFoundKey)
	}

	entry, err := toLastAppliedConfigEntry(req.GetName(), spec, s.keyRing)
	if err != nil {
		return nil, status.Errorf(codes.Internal, "map config entry %s/%s: %v", notFoundKey.Namespace, notFoundKey.Name, err)
	}
	entry.Namespace = req.GetTargetNamespace()
	return &config_read.GetConfigResponse{Config: entry}, nil
}

// List returns every last-applied intent for a single target, scoped to
// the requested target: everything targetconfig's reconciler last
// confirmed was successfully pushed to the device, read off that target's
// TargetSnapshot. Looking the TargetSnapshot up by {target namespace,
// target name} needs no label matching — mirrors
// targetconfig.reconciler.loadSnapshot's existing lookup. A TargetSnapshot
// that doesn't exist yet (target never had a successful transaction)
// returns an empty list, not an error — mirroring loadSnapshot's own
// not-found handling and preserving parity with Cache.Type: local's
// behavior for a target that's never transacted.
func (s *Server) List(ctx context.Context, req *config_read.ListConfigRequest) (*config_read.ListConfigResponse, error) {
	if req.GetTargetNamespace() == "" || req.GetTargetName() == "" {
		return nil, status.Error(codes.InvalidArgument, "target_namespace and target_name are required")
	}

	targetKey := types.NamespacedName{Namespace: req.GetTargetNamespace(), Name: req.GetTargetName()}
	snapshot := &configv1alpha1.TargetSnapshot{}
	if err := s.client.Get(ctx, targetKey, snapshot); err != nil {
		if apierrors.IsNotFound(err) {
			return &config_read.ListConfigResponse{}, nil
		}
		return nil, status.Errorf(codes.Internal, "get targetsnapshot %s/%s: %v", targetKey.Namespace, targetKey.Name, err)
	}

	entries := make([]*config_read.ConfigEntry, 0, len(snapshot.Spec.Configs))
	for name, spec := range snapshot.Spec.Configs {
		entry, err := toLastAppliedConfigEntry(name, spec, s.keyRing)
		if err != nil {
			return nil, status.Errorf(codes.Internal, "map config entry %s/%s: %v", req.GetTargetNamespace(), name, err)
		}
		entry.Namespace = req.GetTargetNamespace()
		entries = append(entries, entry)
	}
	return &config_read.ListConfigResponse{Config: entries}, nil
}

func notFoundConfig(key types.NamespacedName) error {
	return status.Errorf(codes.NotFound, "config %s/%s not found", key.Namespace, key.Name)
}

// Modify upserts the applied payload for a single Intent into
// TargetSnapshot.Spec.Configs[name] — the write-path counterpart to
// Get/List, called synchronously from lowlevelTransactionSet's apply loop
// when southbound apply succeeds. It writes via a targeted JSON merge-patch
// (RFC 7396) rather than get-then-replace, so this call and a concurrent
// backstop prune (post-Confirm saveSnapshot) can't stomp on each other, and
// a concurrent unrelated key in the map is left untouched.
//
// Revertive and Lifecycle are not trusted off the wire ConfigEntry (Modify's
// caller reports applied config content, not config-server-owned lifecycle
// policy) — they're read fresh off the target's current SensitiveConfig, by
// name, in the same namespace as the TargetSnapshot.
func (s *Server) Modify(ctx context.Context, req *config_read.ModifyConfigRequest) (*config_read.ModifyConfigResponse, error) {
	entry := req.GetConfig()
	if req.GetTargetNamespace() == "" || req.GetTargetName() == "" || entry.GetName() == "" {
		return nil, status.Error(codes.InvalidArgument, "target_namespace, target_name and config.name are required")
	}

	scKey := types.NamespacedName{Namespace: req.GetTargetNamespace(), Name: entry.GetName()}
	sc := &configv1alpha1.SensitiveConfig{}
	if err := s.client.Get(ctx, scKey, sc); err != nil {
		return nil, status.Errorf(codes.Internal, "get sensitiveconfig %s/%s: %v", scKey.Namespace, scKey.Name, err)
	}

	spec, err := fromConfigEntry(entry, sc.Spec.Revertive, sc.Spec.Lifecycle, s.keyRing)
	if err != nil {
		return nil, status.Errorf(codes.Internal, "map config entry %s/%s: %v", req.GetTargetNamespace(), entry.GetName(), err)
	}

	if err := s.upsertSnapshotEntry(ctx, req.GetTargetNamespace(), req.GetTargetName(), entry.GetName(), spec); err != nil {
		return nil, status.Errorf(codes.Internal, "modify targetsnapshot %s/%s: %v", req.GetTargetNamespace(), req.GetTargetName(), err)
	}
	return &config_read.ModifyConfigResponse{}, nil
}

// Delete removes a single Intent's entry from
// TargetSnapshot.Spec.Configs[name] via the same targeted merge-patch
// mechanism as Modify. Membership is a single map with no parallel deleted
// index or tombstone: a missing key — whether the snapshot itself doesn't
// exist yet, or just this one entry is already gone — is a no-op success,
// not an error, so retries and out-of-order delivery stay idempotent.
func (s *Server) Delete(ctx context.Context, req *config_read.DeleteConfigRequest) (*config_read.DeleteConfigResponse, error) {
	if req.GetTargetNamespace() == "" || req.GetTargetName() == "" || req.GetName() == "" {
		return nil, status.Error(codes.InvalidArgument, "target_namespace, target_name and name are required")
	}

	patch, err := json.Marshal(map[string]any{
		"spec": map[string]any{"configs": map[string]any{req.GetName(): nil}},
	})
	if err != nil {
		return nil, status.Errorf(codes.Internal, "marshal merge-patch for %s/%s: %v", req.GetTargetNamespace(), req.GetName(), err)
	}

	if err := s.patchSnapshotConfigs(ctx, req.GetTargetNamespace(), req.GetTargetName(), patch); err != nil {
		if apierrors.IsNotFound(err) {
			return &config_read.DeleteConfigResponse{}, nil
		}
		return nil, status.Errorf(codes.Internal, "delete config %s from targetsnapshot %s/%s: %v", req.GetName(), req.GetTargetNamespace(), req.GetTargetName(), err)
	}
	return &config_read.DeleteConfigResponse{}, nil
}

// upsertSnapshotEntry writes a single Configs[name] entry via merge-patch.
// A NotFound patch result means the target has never had a successful
// transaction yet — no TargetSnapshot exists — so this falls back to
// creating one carrying just this entry.
func (s *Server) upsertSnapshotEntry(ctx context.Context, targetNamespace, targetName, name string, spec configv1alpha1.SensitiveConfigSpec) error {
	patch, err := json.Marshal(map[string]any{
		"spec": map[string]any{"configs": map[string]any{name: spec}},
	})
	if err != nil {
		return err
	}

	if err := s.patchSnapshotConfigs(ctx, targetNamespace, targetName, patch); err != nil {
		if !apierrors.IsNotFound(err) {
			return err
		}
		snapshot := &configv1alpha1.TargetSnapshot{
			ObjectMeta: metav1.ObjectMeta{Namespace: targetNamespace, Name: targetName},
			Spec: configv1alpha1.TargetSnapshotSpec{
				Configs: map[string]configv1alpha1.SensitiveConfigSpec{name: spec},
			},
		}
		return s.client.Create(ctx, snapshot)
	}
	return nil
}

// patchSnapshotConfigs applies a JSON merge patch (RFC 7396) to the target's
// TargetSnapshot. A merge patch only ever touches the keys named in the
// patch body, leaving every other Spec.Configs entry untouched — the
// property both Modify and Delete depend on to race safely against each
// other and against the post-Confirm backstop prune.
func (s *Server) patchSnapshotConfigs(ctx context.Context, targetNamespace, targetName string, patch []byte) error {
	snapshot := &configv1alpha1.TargetSnapshot{
		ObjectMeta: metav1.ObjectMeta{Namespace: targetNamespace, Name: targetName},
	}
	return s.client.Patch(ctx, snapshot, client.RawPatch(types.MergePatchType, patch))
}
