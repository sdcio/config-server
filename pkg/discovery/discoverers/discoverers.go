package discoverers

import (
	"context"

	"github.com/openconfig/gnmic/pkg/api/target"
	invv1alpha1 "github.com/sdcio/config-server/apis/inv/v1alpha1"
)

/*
var Discoverers = map[string]Initializer{}

type Initializer func() Discoverer

func Register(name string, initFn Initializer) {
	Discoverers[name] = initFn
}
*/

// Discoverer discovers the target and returns discoveryInfo such as chassis type, SW version,
// SerialNumber, etc
type Discoverer interface {
	// Discover the target
	Discover(ctx context.Context, t *target.Target) (*invv1alpha1.DiscoveryInfo, error)
	// GetProvider gets the provider name
	GetProvider() string
}
