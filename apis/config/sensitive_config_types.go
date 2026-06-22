/*
Copyright 2025 Nokia.

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

package config

import (
	"reflect"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// SensitiveConfigSpec defines the desired state of SensitiveConfig
type SensitiveConfigSpec struct {
	// Generation is the Config CR generation this was resolved from.
	// Used during recovery to detect if the spec changed since last apply.
	Generation int64 `json:"generation,omitempty" protobuf:"varint,1,opt,name=generation"`
	// Lifecycle determines the lifecycle policies the resource e.g. delete is orphan or delete
	// will follow
	Lifecycle *Lifecycle `json:"lifecycle,omitempty" protobuf:"bytes,2,opt,name=lifecycle"`
	// Priority defines the priority of this SensitiveConfig
	Priority int64 `json:"priority,omitempty" protobuf:"varint,3,opt,name=priority"`
	// Revertive defines if this CR is enabled for revertive or non revertve operation
	Revertive *bool `json:"revertive,omitempty" protobuf:"varint,4,opt,name=revertive"`
	// ConfigHash is SHA-256 of the original (unresolved) cfg.Spec.Config blobs.
	ConfigHash string `json:"configHash,omitempty" protobuf:"varint,5,opt,name=configHash"`
	// SecretKeyHashes maps "secretName/keyName" → sha256(secret.Data[keyName]).
	// Tracks only the specific key value — not the whole Secret object.
	SecretKeyHashes map[string]string `json:"secretKeyHashes,omitempty" protobuf:"varint,6,opt,name=secretKeyHashes"`
	// Payload contains the encrypted resolved []ConfigBlob.
	// Plaintext is JSON-marshaled []ConfigBlob with all secret::name::key refs substituted.
	Payload EncryptedPayload `json:"payload" protobuf:"bytes,7,opt,name=payload"`
	// SensitivePaths holds keyless XPath strings (e.g. /interfaces/hash) for every
	// leaf resolved from a secret. Passed to the dataserver as
	// TransactionIntent.SensitivePaths so values are redacted northbound.
	// Not secret: derivable from cfg.Spec.Config, stored in clear for transact-time use.
	SensitivePaths []string `json:"sensitivePaths,omitempty" protobuf:"bytes,8,rep,name=sensitivePaths"`
}

// EncryptedPayload is the common encrypted structure used by both
// SensitiveConfig and TargetSnapshot.
type EncryptedPayload struct {
	// KeyID identifies which key in the keyring was used for encryption.
	// Used to detect key rotation without decryption.
	KeyID string `json:"keyID" protobuf:"bytes,1,opt,name=keyID"`
	// PlainHash is a SHA-256 hash of the plaintext before encryption.
	// Stored in clear for efficient change detection without decryption.
	PlainHash string `json:"plainHash" protobuf:"bytes,2,opt,name=plainHash"`
	// Data contains the AES-GCM encrypted, JSON-marshaled payload.
	Data []byte `json:"data" protobuf:"bytes,3,opt,name=data"`
}

// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object
// +kubebuilder:object:root=true
// +kubebuilder:storageversion
// +kubebuilder:resource:categories={sdc}

// SensitiveConfig defines the Schema for the SensitiveConfig API
type SensitiveConfig struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty" protobuf:"bytes,1,opt,name=metadata"`

	Spec SensitiveConfigSpec `json:"spec,omitempty" protobuf:"bytes,2,opt,name=spec"`
}

// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object
// +kubebuilder:object:root=true

// SensitiveConfigList contains a list of SensitiveConfigs
type SensitiveConfigList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty" protobuf:"bytes,1,opt,name=metadata"`
	Items           []SensitiveConfig `json:"items" protobuf:"bytes,2,rep,name=items"`
}

// SensitiveConfig type metadata.
var (
	SensitiveConfigKind = reflect.TypeOf(SensitiveConfig{}).Name()
)
