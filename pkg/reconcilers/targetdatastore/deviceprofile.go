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
	invv1alpha1 "github.com/sdcio/config-server/apis/inv/v1alpha1"
	sdcpb "github.com/sdcio/sdc-protos/sdcpb"
)

// toProtoDeviceProfile maps the KRM-layer DeviceProfile (TargetConnectionProfile.DeviceProfile(),
// which already defaults an absent Spec.DeviceProfile to DeviceProfileNone) to the sdcpb.DeviceProfile
// enum sent to data-server on CreateDataStore. Any value not recognized here falls back to the
// generic profile, so targets without an explicit deviceProfile keep today's behaviour unchanged.
func toProtoDeviceProfile(dp invv1alpha1.DeviceProfile) sdcpb.DeviceProfile {
	switch dp {
	case invv1alpha1.DeviceProfileSonic:
		return sdcpb.DeviceProfile_DEVICE_PROFILE_SONIC
	case invv1alpha1.DeviceProfileCiscoIOSXR:
		return sdcpb.DeviceProfile_DEVICE_PROFILE_CISCO_IOS_XR
	default:
		return sdcpb.DeviceProfile_DEVICE_PROFILE_GENERIC
	}
}
