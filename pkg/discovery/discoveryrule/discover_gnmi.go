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

	"github.com/henderiw/apiserver-store/pkg/storebackend"
	"github.com/henderiw/logger/log"
	"github.com/openconfig/gnmi/proto/gnmi"
	"github.com/openconfig/gnmic/pkg/api"
	"github.com/openconfig/gnmic/pkg/api/target"
	invv1alpha1 "github.com/sdcio/config-server/apis/inv/v1alpha1"
	"github.com/sdcio/config-server/pkg/discovery/discoverers"
	"github.com/sdcio/config-server/pkg/discovery/discoverers/nokia_srl"
	"github.com/sdcio/config-server/pkg/discovery/discoverers/nokia_sros"
	"github.com/sdcio/config-server/pkg/lease"
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

	r.children.Create(ctx, storebackend.ToKey(getTargetName(di.HostName)), "") // this should be done here

	l := lease.New(r.client, types.NamespacedName{
		Namespace: r.cfg.CR.GetNamespace(),
		Name:      getTargetName(di.HostName),
	})
	if err := l.AcquireLease(ctx, "DiscoveryController"); err != nil {
		log.Debug("cannot acquire lease", "target", getTargetName(di.HostName), "error", err.Error())
		return err
	}

	newTargetCR, err := r.newTargetCR(
		ctx,
		discoverer.GetProvider(),
		t.Config.Address,
		di,
	)
	if err != nil {
		return err
	}

	if err := r.applyTarget(ctx, newTargetCR); err != nil {
		// TODO reapply if update failed
		if strings.Contains(err.Error(), "the object has been modified; please apply your changes to the latest version") {
			// we will rety once, sometimes we get an error
			if err := r.applyTarget(ctx, newTargetCR); err != nil {
				log.Info("dynamic target creation retry failed", "error", err)
			}
		} else {
			log.Info("dynamic target creation failed", "error", err)
		}
	}
	if err := r.applyUnManagedConfigCR(ctx, newTargetCR.Name); err != nil {
		return err
	}
	return nil
}

func CreateTarget(ctx context.Context, address string, secret *corev1.Secret, connProfile *invv1alpha1.TargetConnectionProfile) (*target.Target, error) {

	tOpts := []api.TargetOption{
		//api.Name(req.NamespacedName.String()),
		api.Address(address),
		api.Username(string(secret.Data["username"])),
		api.Password(string(secret.Data["password"])),
		api.Timeout(5 * time.Second),
	}
	if connProfile.Spec.Insecure {
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
