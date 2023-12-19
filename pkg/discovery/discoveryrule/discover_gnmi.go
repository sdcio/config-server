package discoveryrule

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/henderiw/logger/log"
	invv1alpha1 "github.com/iptecharch/config-server/apis/inv/v1alpha1"
	"github.com/iptecharch/config-server/pkg/discovery/discoverers"
	"github.com/iptecharch/config-server/pkg/discovery/discoverers/nokia_srl"
	"github.com/iptecharch/config-server/pkg/discovery/discoverers/nokia_sros"
	"github.com/openconfig/gnmi/proto/gnmi"
	"github.com/openconfig/gnmic/pkg/api"
	"github.com/openconfig/gnmic/pkg/target"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/types"
)

func (r *dr) discoverWithGNMI(ctx context.Context, ip string, connProfile *invv1alpha1.TargetConnectionProfile) error {
	log := log.FromContext(ctx)
	secret := &corev1.Secret{}
	err := r.client.Get(ctx, types.NamespacedName{
		Namespace: r.cfg.CR.GetNamespace(),
		Name:      r.cfg.DiscoveryProfile.Secret,
	}, secret)
	if err != nil {
		return err
	}
	address := fmt.Sprintf("%s:%d", ip, connProfile.Spec.Port)

	t, err := CreateTarget(ctx, address, secret, connProfile)
	if err != nil {
		return err
	}
	log.Info("Creating gNMI client")
	err = t.CreateGNMIClient(ctx)
	if err != nil {
		return err
	}
	defer t.Close()
	capRsp, err := t.Capabilities(ctx)
	if err != nil {
		return err
	}
	discoverer, err := GetDiscovererGNMI(capRsp)
	if err != nil {
		return err
	}
	di, err := discoverer.Discover(ctx, t)
	if err != nil {
		return err
	}
	b, _ := json.Marshal(di)
	log.Info("discovery info", "info", string(b))

	newTargetCr, err := r.newTargetCR(
		ctx,
		discoverer.GetProvider(),
		t.Config.Address,
		di,
	)
	if err != nil {
		return err
	}

	return r.applyTarget(ctx, newTargetCr)
}

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
				init := discoverers.Discoverers[nokia_srl.Provider]
				discoverer = init()
			} else {
				// SROS
				init := discoverers.Discoverers[nokia_sros.Provider]
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
