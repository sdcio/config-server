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

package arista

/*

import (
	"context"

	"github.com/openconfig/gnmic/pkg/api"
	"github.com/openconfig/gnmic/pkg/api/target"
	"github.com/openconfig/gnmic/pkg/path"
	invv1alpha1 "github.com/sdcio/config-server/apis/inv/v1alpha1"
	"github.com/sdcio/config-server/pkg/discovery/discoverers"
)

const (
	Provider = "eos.arista.sdcio.dev"
	swVersionPath = "components/component[name=EOS]/state/software-version"
	chassisPath   = "components/component[name=Chassis]/state/part-no"
	hostnamePath  = "system/state/hostname"
	serialNumberPath = "components/component[name=Chassis]/state/serial-no"
	hWMacAddrPath    = "lldp/state/chassis-id"
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
		api.Path(swVersionPath),
		api.Path(chassisPath),
		api.Path(hostnamePath),
		api.Path(serialNumberPath),
		api.Path(hWMacAddrPath),
		api.EncodingASCII(),
		api.DataTypeSTATE(),
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
		//LastSeen: metav1.Time{
		//	Time: time.Now(),
		//},
		SupportedEncodings: make([]string, 0, len(capRsp.GetSupportedEncodings())),
	}
	for _, enc := range capRsp.GetSupportedEncodings() {
		di.SupportedEncodings = append(di.SupportedEncodings, enc.String())
	}
	for _, notif := range resp.GetNotification() {
		for _, upd := range notif.GetUpdate() {
			p := path.GnmiPathToXPath(upd.GetPath(), true)
			switch p {
			case swVersionPath:
				di.Version = upd.GetVal().GetStringVal()
			case chassisPath:
				di.Platform = upd.GetVal().GetStringVal()
			case serialNumberPath:
				di.SerialNumber = upd.GetVal().GetStringVal()
			case hWMacAddrPath:
				di.MacAddress = upd.GetVal().GetStringVal()
			case hostnamePath:
				di.HostName = upd.GetVal().GetStringVal()
			}
		}
	}
	return di, nil
}
*/