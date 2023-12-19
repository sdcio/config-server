package discoveryrule

/*
import (
	"context"

	invv1alpha1 "github.com/iptecharch/config-server/apis/inv/v1alpha1"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// global var on which we store the supported discovery rules
// they get initialized
var DiscoveryRules = map[schema.GroupVersionKind]Initializer{}

type Initializer func(client client.Client) DiscoveryRule

func Register(gvk schema.GroupVersionKind, initFn Initializer) {
	DiscoveryRules[gvk] = initFn
}

type DiscoveryRule interface {
	Run(ctx context.Context, dr *invv1alpha1.DiscoveryRuleContext) error
	Stop(ctx context.Context)
	// Get gets the specific discovery Rule and return the resource version and error
	Get(ctx context.Context, key types.NamespacedName) (string, error)
}
*/
