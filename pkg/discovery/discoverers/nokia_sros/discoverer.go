package nokia_sros

import (
	"context"
	"strings"
	"time"

	invv1alpha1 "github.com/iptecharch/config-server/apis/inv/v1alpha1"
	"github.com/iptecharch/config-server/pkg/discovery/discoverers"
	"github.com/openconfig/gnmic/pkg/api"
	"github.com/openconfig/gnmic/pkg/path"
	"github.com/openconfig/gnmic/pkg/target"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

const (
	NokiaSROSDiscovererName = "nokia-sros"
	// TODO: check if we need to differentiate slotA and slotB
	srosSWVersionPath    = "state/system/version/version-number"
	srosChassisPath      = "state/system/platform"
	srosHostnamePath     = "state/system/oper-name"
	srosHWMacAddressPath = "state/system/base-mac-address"
	srosSerialNumberPath = "state/chassis/hardware-data/serial-number"
)

func init() {
	discoverers.Register(NokiaSROSDiscovererName, func() discoverers.Discoverer {
		return &sros{}
	})
}

type sros struct{}

func (s *sros) ProviderName() string {
	return "sros.nokia.com"
}

func (s *sros) Discover(ctx context.Context, dr *invv1alpha1.DiscoveryRuleContext, t *target.Target) (*invv1alpha1.DiscoveryInfo, error) {
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
		Type:   "sros",
		Vendor: "Nokia",
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
