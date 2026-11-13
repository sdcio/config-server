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
	"context"

	"github.com/sdcio/data-server/pkg/api"
)

// DatastoreAdmin is the in-process data-server handle for datastore
// lifecycle. Unknown datastores are reported as api.ErrUnknownDatastore.
type DatastoreAdmin interface {
	GetDataStore(ctx context.Context, name string) (*api.DatastoreInfo, error)
	CreateDataStore(ctx context.Context, spec api.DatastoreSpec) error
	DeleteDataStore(ctx context.Context, name string) error
}

// TransactionClient is the in-process data-server handle for transactions.
// Errors are matched with api.IsRetryable.
type TransactionClient interface {
	TransactionSet(ctx context.Context, ref api.TxRef, opts api.SetOptions, intents []api.Intent) (*api.TransactionResult, error)
	TransactionConfirm(ctx context.Context, ref api.TxRef, origin api.Origin) error
	TransactionCancel(ctx context.Context, ref api.TxRef, origin api.Origin) error
}

// DeviationSource opens a deviation watch; the channel closes when ctx ends or
// the datastore goes away.
type DeviationSource interface {
	WatchDeviations(ctx context.Context, name string) (<-chan api.DeviationEvent, error)
}

// DataServer is everything the target manager needs from the data-server
// core. *core.Core satisfies it.
type DataServer interface {
	DatastoreAdmin
	TransactionClient
	DeviationSource
}

// origin names the controller towards the data-server in logs.
const origin api.Origin = "config-server"
