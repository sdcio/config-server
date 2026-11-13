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
	"github.com/sdcio/data-server/pkg/api"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func (r *TargetSyncProfileSync) GetEncoding() Encoding {
	if r.Encoding == nil {
		return Encoding_JSON_IETF
	}
	return *r.Encoding
}

// GetSyncProfile builds the datastore sync from the profile; sync targets
// reuse the address and TLS/credentials of target.
func GetSyncProfile(syncProfile *TargetSyncProfile, target api.Target) api.Sync {
	sync := api.Sync{
		Configs: make([]api.SyncConfig, 0, len(syncProfile.Spec.Sync)),
	}
	for _, syncConfig := range syncProfile.Spec.Sync {
		synccfg := api.SyncConfig{
			Name: syncConfig.Name,
			Target: &api.Target{
				Type:        api.TargetType(syncConfig.Protocol),
				Address:     target.Address,
				Port:        uint32(syncConfig.Port),
				TLS:         target.TLS,
				Credentials: target.Credentials,
			},
			Paths:    syncConfig.Paths,
			Mode:     getSyncMode(syncConfig.Mode),
			Interval: uint64(syncConfig.Interval.Nanoseconds()),
		}
		if syncConfig.Protocol == Protocol_GNMI {
			synccfg.Target.ProtocolOptions.GNMI = &api.GNMIOptions{
				Encoding: getEncoding(syncConfig.GetEncoding()),
			}
		}
		sync.Configs = append(sync.Configs, synccfg)
	}
	return sync
}

// getEncoding maps the profile encoding to the api name; CONFIG is
// api.GNMIEncodingConfig.
func getEncoding(e Encoding) api.GNMIEncoding {
	return api.GNMIEncoding(e)
}

func getSyncMode(mode SyncMode) api.SyncMode {
	switch mode {
	case SyncMode_Sample:
		return api.SyncModeSample
	case SyncMode_Once:
		return api.SyncModeOnce
	case SyncMode_Get:
		return api.SyncModeGet
	default:
		return api.SyncModeOnChange
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
					Mode:     SyncMode_Get,
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
