// Package dataserverpin keeps the embedded data-server module in go.mod until
// the controller imports it directly.
package dataserverpin

import (
	_ "github.com/sdcio/data-server/pkg/core"
	_ "github.com/sdcio/data-server/pkg/grpcserver"
)
