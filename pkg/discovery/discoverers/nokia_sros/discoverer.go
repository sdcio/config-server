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

package nokia_sros

import (
	"context"
	"strings"
	"time"

	"github.com/openconfig/gnmic/pkg/api"
	"github.com/openconfig/gnmic/pkg/api/target"
	"github.com/openconfig/gnmic/pkg/path"
	invv1alpha1 "github.com/sdcio/config-server/apis/inv/v1alpha1"
	"github.com/sdcio/config-server/pkg/discovery/discoverers"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

const (
	Provider = "sros.nokia.sdcio.dev"
	// TODO: check if we need to differentiate slotA and slotB
	srosSWVersionPath    = "state/system/version/version-number"
	srosChassisPath      = "state/system/platform"
	srosHostnamePath     = "state/system/oper-name"
	srosHWMacAddressPath = "state/system/base-mac-address"
	srosSerialNumberPath = "state/chassis/hardware-data/serial-number"
)

func init() {
	discoverers.Register(Provider, func() discoverers.Discoverer {
		return &discoverer{}
	})
}

type discoverer struct{}

func (s *discoverer) GetProvider() string {
	return Provider
}

func (s *discoverer) Discover(ctx context.Context, t *target.Target) (*invv1alpha1.DiscoveryInfo, error) {
	req, err := api.NewGetRequest(
		api.Path(srosSWVersionPath),
		api.Path(srosChassisPath),
		api.Path(srosHWMacAddressPath),
		api.Path(srosHostnamePath),
		api.Path(srosSerialNumberPath),
		api.EncodingJSON(),
	)
	if err != nil {
		return nil, err
	}
	capRsp, err := t.Capabilities(ctx)
	if err != nil {
		return nil, err
	}
	resp, err := t.Get(ctx, req)
	if err != nil {
		return nil, err
	}
	di := &invv1alpha1.DiscoveryInfo{
		Protocol: string(invv1alpha1.Protocol_GNMI),
		Provider: Provider,
		LastSeen: metav1.Time{
			Time: time.Now(),
		},
		SupportedEncodings: make([]string, 0, len(capRsp.GetSupportedEncodings())),
	}
	for _, enc := range capRsp.GetSupportedEncodings() {
		di.SupportedEncodings = append(di.SupportedEncodings, enc.String())
	}
	for _, notif := range resp.GetNotification() {
		for _, upd := range notif.GetUpdate() {
			p := path.GnmiPathToXPath(upd.GetPath(), true)
			switch p {
			case srosSWVersionPath:
				val := string(upd.GetVal().GetJsonVal())
				val = strings.Trim(val, "\"")
				di.Version = val
			case srosChassisPath:
				val := string(upd.GetVal().GetJsonVal())
				val = strings.Trim(val, "\"")
				di.Platform = val
			case srosSerialNumberPath:
				val := string(upd.GetVal().GetJsonVal())
				val = strings.Trim(val, "\"")
				di.SerialNumber = val
			case srosHWMacAddressPath:
				val := string(upd.GetVal().GetJsonVal())
				val = strings.Trim(val, "\"")
				di.MacAddress = val
			case srosHostnamePath:
				val := string(upd.GetVal().GetJsonVal())
				val = strings.Trim(val, "\"")
				di.HostName = val
			}
		}
	}
	return di, nil
}
