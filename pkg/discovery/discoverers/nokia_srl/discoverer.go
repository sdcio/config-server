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

package nokia_srl

/*

import (
	"context"
	"strings"

	"github.com/openconfig/gnmic/pkg/api"
	"github.com/openconfig/gnmic/pkg/api/target"
	"github.com/openconfig/gnmic/pkg/path"
	invv1alpha1 "github.com/sdcio/config-server/apis/inv/v1alpha1"
	"github.com/sdcio/config-server/pkg/discovery/discoverers"
)

const (
	Provider = "srl.nokia.sdcio.dev"
	swVersionPath = "platform/control/software-version"
	chassisPath   = "platform/chassis"
	hostnamePath  = "system/name/host-name"
	// paths
	chassisTypePath  = "platform/chassis/type"
	serialNumberPath = "platform/chassis/serial-number"
	hwMacAddrPath    = "platform/chassis/hw-mac-address"
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
				di.Version = getVersion(upd.GetVal().GetStringVal())
			case chassisTypePath:
				di.Platform = upd.GetVal().GetStringVal()
			case serialNumberPath:
				di.SerialNumber = upd.GetVal().GetStringVal()
			case hwMacAddrPath:
				di.MacAddress = upd.GetVal().GetStringVal()
			case hostnamePath:
				di.HostName = upd.GetVal().GetStringVal()
			}
		}
	}
	return di, nil
}

func getVersion(version string) string {
	split := strings.Split(version, ".")
	if len(split) < 3 {
		return version
	}
	version = strings.Join([]string{
		strings.ReplaceAll(split[0], "v", ""), // remove the v from the first slice
		split[1],                              // take the 2nd slice as is
		strings.Split(split[2], "-")[0],       // take the first part of the 3rd slice before -
	}, ".")
	return version
}
*/