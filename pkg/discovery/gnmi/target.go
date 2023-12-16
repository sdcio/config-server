package gnmi

import (
	"context"
	"errors"
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
)

func CreateTarget(ctx context.Context, address string, secret *corev1.Secret, connProfile *invv1alpha1.TargetConnectionProfile) (*target.Target, error) {

	tOpts := []api.TargetOption{
		//api.Name(req.NamespacedName.String()),
		api.Address(address),
		api.Username(string(secret.Data["username"])),
		api.Password(string(secret.Data["password"])),
		api.Timeout(5 * time.Second),
	}
	if connProfile.Spec.Insecure == true {
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
