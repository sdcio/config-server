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

package targetdatastore

import (
	"testing"

	invv1alpha1 "github.com/sdcio/config-server/apis/inv/v1alpha1"
	sdcpb "github.com/sdcio/sdc-protos/sdcpb"
)

func TestToProtoDeviceProfile(t *testing.T) {
	tests := map[string]struct {
		in   invv1alpha1.DeviceProfile
		want sdcpb.DeviceProfile
	}{
		"None": {
			in:   invv1alpha1.DeviceProfileNone,
			want: sdcpb.DeviceProfile_DEVICE_PROFILE_GENERIC,
		},
		"Sonic": {
			in:   invv1alpha1.DeviceProfileSonic,
			want: sdcpb.DeviceProfile_DEVICE_PROFILE_SONIC,
		},
		"CiscoIOSXR": {
			in:   invv1alpha1.DeviceProfileCiscoIOSXR,
			want: sdcpb.DeviceProfile_DEVICE_PROFILE_CISCO_IOS_XR,
		},
		"Unknown": {
			in:   invv1alpha1.DeviceProfile("unknown"),
			want: sdcpb.DeviceProfile_DEVICE_PROFILE_GENERIC,
		},
	}

	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			got := toProtoDeviceProfile(tc.in)
			if got != tc.want {
				t.Errorf("toProtoDeviceProfile(%v) = %v, want %v", tc.in, got, tc.want)
			}
		})
	}
}
