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

package discoveryrule

import (
	invv1alpha1 "github.com/iptecharch/config-server/apis/inv/v1alpha1"
)

type DiscoveryRuleConfig struct {
	// Discovery defines if discovery is enabled or disabled
	Discovery bool
	// Default Schema is the default schema
	DefaultSchema *invv1alpha1.SchemaKey
	// CR that owns the discovery Rule
	CR invv1alpha1.DiscoveryObject
	// Selector for Pod/SVC
	//Selector labels.Selector
	// Prefixes used to discover/connect to the target
	//Prefixes []invv1alpha1.DiscoveryRulePrefix
	// DiscoveryProfile contains the profile data from the k8s api-server
	DiscoveryProfile *DiscoveryProfile
	// ConnectivityProfile contains the profile data from the k8s api-server
	TargetConnectionProfiles []TargetConnectionProfile
	// TargetTemplate defines the template to expand the target
	TargetTemplate *invv1alpha1.TargetTemplate
}

type DiscoveryProfile struct {
	Secret                   string
	SecretResourceVersion    string // used to validate a profile change
	TLSSecret                string
	TLSSecretResourceVersion string // used to validate a profile change
	Connectionprofiles       []*invv1alpha1.TargetConnectionProfile
}

type TargetConnectionProfile struct {
	Secret                   string
	SecretResourceVersion    string // used to validate a profile change + provide the version to the target if provisioned
	TLSSecret                string
	TLSSecretResourceVersion string // used to validate a profile change + provide the version to the target if provisioned
	Connectionprofile        *invv1alpha1.TargetConnectionProfile
	Syncprofile              *invv1alpha1.TargetSyncProfile
}
