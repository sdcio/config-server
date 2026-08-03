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
	"github.com/sdcio/config-server/apis/config"
)

// IntentInput carries everything needed to build one gRPC TransactionIntent.
// Built by the reconciler after decrypting SensitiveConfig payloads.
// The Transactor never touches secrets — it only sees resolved blobs.
type IntentInput struct {
	Config        *config.Config
	ResolvedBlobs []config.ConfigBlob
	Priority      int32
	NonRevertive  bool
	Delete        bool
	SensitivePaths []string // keyless XPath strings from sc.Spec.SensitivePaths
}

// TransactionResult holds the analysed outcome of a TransactionSet call.
type TransactionResult struct {
	GlobalError    error
	IntentErrors   error
	GlobalWarnings []string
	Recoverable    bool
}

// HasErrors returns true when the transaction produced any error.
func (r TransactionResult) HasErrors() bool {
	return r.GlobalError != nil || r.IntentErrors != nil
}
