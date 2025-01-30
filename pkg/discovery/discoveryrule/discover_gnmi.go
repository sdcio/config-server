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
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/henderiw/logger/log"
	"github.com/openconfig/gnmi/proto/gnmi"
	"github.com/openconfig/gnmic/pkg/api"
	"github.com/openconfig/gnmic/pkg/api/target"
	invv1alpha1 "github.com/sdcio/config-server/apis/inv/v1alpha1"
	"github.com/sdcio/config-server/pkg/discovery/discoverers"
	"github.com/sdcio/config-server/pkg/discovery/discoverers/nokia_srl"
	"github.com/sdcio/config-server/pkg/discovery/discoverers/nokia_sros"
	"github.com/sdcio/config-server/pkg/discovery/discoverers/arista"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/types"
)

func (r *dr) discoverWithGNMI(ctx context.Context, h *hostInfo, connProfile *invv1alpha1.TargetConnectionProfile) error {
	log := log.FromContext(ctx)
	secret := &corev1.Secret{}
	err := r.client.Get(ctx, types.NamespacedName{
		Namespace: r.cfg.CR.GetNamespace(),
		Name:      r.cfg.DiscoveryProfile.Secret,
	}, secret)
	if err != nil {
		return err
	}
	address := fmt.Sprintf("%s:%d", h.Address, connProfile.Spec.Port)

	t, err := createGNMITarget(ctx, address, secret, connProfile)
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
	log.Debug("discovery info", "info", string(b))

	return r.createTarget(ctx, discoverer.GetProvider(), h.Address, di)
}

func createGNMITarget(_ context.Context, address string, secret *corev1.Secret, connProfile *invv1alpha1.TargetConnectionProfile) (*target.Target, error) {
	tOpts := []api.TargetOption{
		//api.Name(req.NamespacedName.String()),
		api.Address(address),
		api.Username(string(secret.Data["username"])),
		api.Password(string(secret.Data["password"])),
		api.Timeout(5 * time.Second),
	}
	if connProfile.Spec.Insecure != nil && *connProfile.Spec.Insecure {
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
		case "Arista":
			init := discoverers.Discoverers[arista.Provider]
			discoverer = init()
		}
	}
	if discoverer == nil {
		return nil, errors.New("unknown target vendor")
	}
	return discoverer, nil
}
