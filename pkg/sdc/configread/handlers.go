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

	configv1alpha1 "github.com/sdcio/config-server/apis/config/v1alpha1"
	"github.com/sdcio/sdc-protos/config_read"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/types"
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
