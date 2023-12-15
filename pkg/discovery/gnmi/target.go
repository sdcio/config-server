package gnmi

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	invv1alpha1 "github.com/iptecharch/config-server/apis/inv/v1alpha1"
	"github.com/iptecharch/config-server/pkg/discovery/discoverers"
	"github.com/iptecharch/config-server/pkg/discovery/discoverers/nokia_srl"
	"github.com/iptecharch/config-server/pkg/discovery/discoverers/nokia_sros"
	"github.com/openconfig/gnmi/proto/gnmi"
	"github.com/openconfig/gnmic/pkg/api"
	"github.com/openconfig/gnmic/pkg/target"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

func CreateTarget(ctx context.Context, c client.Client, dr *invv1alpha1.DiscoveryRuleContext, ip string) (*target.Target, error) {
	secret := &corev1.Secret{}
	err := c.Get(ctx, types.NamespacedName{
		Namespace: dr.DiscoveryRule.GetNamespace(),
		Name:      dr.DiscoveryRule.Spec.Secret,
	}, secret)
	if err != nil {
		return nil, err
	}

	tOpts := []api.TargetOption{
		//api.Name(req.NamespacedName.String()),
		api.Address(fmt.Sprintf("%s:%d", ip, dr.DiscoveryRule.Spec.Port)),
		api.Username(string(secret.Data["username"])),
		api.Password(string(secret.Data["password"])),
		api.Timeout(5 * time.Second),
	}
	if dr.ConnectionProfile.Spec.Insecure == true {
		tOpts = append(tOpts, api.Insecure(true))
	} else {
		tOpts = append(tOpts, api.SkipVerify(true))
	}
	// TODO: query certificate, its secret and use it
	return api.NewTarget(tOpts...)
}

func GetDiscovererGNMI(capRsp *gnmi.CapabilityResponse) (discoverers.Discoverer, error) {
	var discoverer discoverers.Discoverer
OUTER:
	for _, m := range capRsp.SupportedModels {
		switch m.Organization {
		case "Nokia":
			if strings.Contains(m.Name, "srl_nokia") {
				// SRL
				init := discoverers.Discoverers[nokia_srl.ProviderName]
				discoverer = init()
			} else {
				// SROS
				init := discoverers.Discoverers[nokia_sros.ProviderName]
				discoverer = init()
			}
			break OUTER
		}
	}
	if discoverer == nil {
		return nil, errors.New("unknown target vendor")
	}
	return discoverer, nil
}
