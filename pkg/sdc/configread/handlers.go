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

	"github.com/sdcio/config-server/apis/config"
	configv1alpha1 "github.com/sdcio/config-server/apis/config/v1alpha1"
	targetmanager "github.com/sdcio/config-server/pkg/sdc/target/manager"
	"github.com/sdcio/sdc-protos/config_read"
	sdcpb "github.com/sdcio/sdc-protos/sdcpb"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
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

// List returns every Config (joined with its SensitiveConfig, if any) for a
// single target, scoped by the same TargetNamespaceKey/TargetNameKey labels
// ConfigManager.ListConfigsPerTarget already uses.
func (s *Server) List(ctx context.Context, req *config_read.ListConfigRequest) (*config_read.ListConfigResponse, error) {
	if req.GetTargetNamespace() == "" || req.GetTargetName() == "" {
		return nil, status.Error(codes.InvalidArgument, "target_namespace and target_name are required")
	}

	cfgList := &configv1alpha1.ConfigList{}
	if err := s.client.List(ctx, cfgList,
		client.InNamespace(req.GetTargetNamespace()),
		client.MatchingLabels{
			config.TargetNamespaceKey: req.GetTargetNamespace(),
			config.TargetNameKey:      req.GetTargetName(),
		},
	); err != nil {
		return nil, status.Errorf(codes.Internal, "list configs for %s/%s: %v", req.GetTargetNamespace(), req.GetTargetName(), err)
	}

	entries := make([]*config_read.ConfigEntry, 0, len(cfgList.Items))
	for i := range cfgList.Items {
		cfg := &cfgList.Items[i]
		key := types.NamespacedName{Namespace: cfg.Namespace, Name: cfg.Name}

		sc, err := s.getSensitiveConfig(ctx, key)
		if err != nil {
			return nil, status.Errorf(codes.Internal, "get sensitiveconfig %s/%s: %v", key.Namespace, key.Name, err)
		}

		entry, err := toConfigEntry(cfg, sc)
		if err != nil {
			return nil, status.Errorf(codes.Internal, "map config entry %s/%s: %v", key.Namespace, key.Name, err)
		}
		entries = append(entries, entry)
	}
	return &config_read.ListConfigResponse{Config: entries}, nil
}

// getSensitiveConfig fetches the SensitiveConfig joined by name to the given
// Config's namespaced name. A missing SensitiveConfig is not an error — most
// Configs never resolve one.
func (s *Server) getSensitiveConfig(ctx context.Context, key types.NamespacedName) (*configv1alpha1.SensitiveConfig, error) {
	sc := &configv1alpha1.SensitiveConfig{}
	if err := s.client.Get(ctx, key, sc); err != nil {
		if apierrors.IsNotFound(err) {
			return nil, nil
		}
		return nil, err
	}
	return sc, nil
}

func notFoundConfig(key types.NamespacedName) error {
	return status.Errorf(codes.NotFound, "config %s/%s not found", key.Namespace, key.Name)
}

// toConfigEntry maps a Config (+ its optional joined SensitiveConfig) onto
// the wire ConfigEntry shape, per the ADR's field-mapping table. GetDeletes()
// has no source on this backend (see the ADR's Known limitations) and is
// deliberately absent from the wire shape — nothing to map here.
func toConfigEntry(cfg *configv1alpha1.Config, sc *configv1alpha1.SensitiveConfig) (*config_read.ConfigEntry, error) {
	blobs := make([]*config_read.ConfigBlob, 0, len(cfg.Spec.Config))
	for _, b := range cfg.Spec.Config {
		blobs = append(blobs, &config_read.ConfigBlob{Path: b.Path, Value: b.Value.Raw})
	}

	var sensitivePaths []*sdcpb.Path
	if sc != nil {
		sp, err := targetmanager.ParseSensitivePaths(sc.Spec.SensitivePaths)
		if err != nil {
			return nil, err
		}
		sensitivePaths = sp
	}

	return &config_read.ConfigEntry{
		Name:           cfg.Name,
		Namespace:      cfg.Namespace,
		NonRevertive:   !cfg.IsRevertive(),
		Orphan:         isOrphan(cfg),
		Priority:       cfg.Spec.Priority,
		SensitivePaths: sensitivePaths,
		Config:         blobs,
	}, nil
}

func isOrphan(cfg *configv1alpha1.Config) bool {
	return cfg.Spec.Lifecycle != nil && cfg.Spec.Lifecycle.DeletionPolicy == configv1alpha1.DeletionOrphan
}
