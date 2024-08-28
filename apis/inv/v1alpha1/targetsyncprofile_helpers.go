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

package v1alpha1

import (
	"reflect"

	"github.com/sdcio/config-server/pkg/testhelper"
	sdcpb "github.com/sdcio/sdc-protos/sdcpb"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func GetSyncProfile(syncProfile *TargetSyncProfile) *sdcpb.Sync {
	sync := &sdcpb.Sync{
		Validate:     false,
		Buffer:       10000.,
		WriteWorkers: 10,
	}
	sync.Config = make([]*sdcpb.SyncConfig, 0, len(syncProfile.Spec.Sync))
	for _, syncConfig := range syncProfile.Spec.Sync {
		sync.Config = append(sync.Config, &sdcpb.SyncConfig{
			Name:     syncConfig.Name,
			Protocol: string(syncConfig.Protocol),
			Path:     syncConfig.Paths,
			Mode:     getSyncMode(syncConfig.Mode),
			Encoding: getEncoding(syncConfig.Encoding),
			Interval: uint64(syncConfig.Interval.Nanoseconds()),
		})
	}
	return sync
}

func getEncoding(e Encoding) string {
	switch e {
	case Encoding_CONFIG:
		return "45"
	default:
		return string(e)
	}
}

func getSyncMode(mode SyncMode) sdcpb.SyncMode {
	switch mode {
	case SyncMode_OnChange:
		return sdcpb.SyncMode_SM_ON_CHANGE
	case SyncMode_Sample:
		return sdcpb.SyncMode_SM_SAMPLE
	case SyncMode_Once:
		return sdcpb.SyncMode_SM_ONCE
	case SyncMode_Get:
		return sdcpb.SyncMode_SM_GET
	default:
		return sdcpb.SyncMode_SM_ON_CHANGE
	}
}

// DefaultTargetSyncProfile returns a default TargetSyncProfile
func DefaultTargetSyncProfile() *TargetSyncProfile {
	return BuildTargetSyncProfile(
		metav1.ObjectMeta{
			Name:      "default",
			Namespace: "default",
		},
		TargetSyncProfileSpec{
			Validate: true,
			Sync: []TargetSyncProfileSync{
				{
					Name:     "config",
					Protocol: Protocol_GNMI,
					Paths:    []string{"/"},
					Mode:     SyncMode_OnChange,
				},
			},
		},
	)
}

// BuildTargetSyncProfile returns a TargetSyncProfile from a client Object a crName and
// an TargetSyncProfile Spec/Status
func BuildTargetSyncProfile(meta metav1.ObjectMeta, spec TargetSyncProfileSpec) *TargetSyncProfile {
	return &TargetSyncProfile{
		TypeMeta: metav1.TypeMeta{
			APIVersion: localSchemeBuilder.GroupVersion.Identifier(),
			Kind:       TargetSyncProfileKind,
		},
		ObjectMeta: meta,
		Spec:       spec,
	}
}

// GetTargetSyncProfileFromFile is a helper for tests to use the
// examples and validate them in unit tests
func GetTargetSyncProfileFromFile(path string) (*DiscoveryRule, error) {
	addToScheme := AddToScheme
	obj := &DiscoveryRule{}
	gvk := SchemeGroupVersion.WithKind(reflect.TypeOf(obj).Name())
	// build object from file
	if err := testhelper.GetKRMResource(path, obj, gvk, addToScheme); err != nil {
		return nil, err
	}
	return obj, nil
}
