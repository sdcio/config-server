package nokia_srl

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
	NokiaSRLDiscovererName = "nokia-srl"
	// TODO: check if we need to differentiate slotA and slotB
	srlSwVersionPath = "platform/control/software-version"
	srlChassisPath   = "platform/chassis"
	srlHostnamePath  = "system/name/host-name"
	//
	srlChassisTypePath  = "platform/chassis/type"
	srlSerialNumberPath = "platform/chassis/serial-number"
	srlHWMacAddrPath    = "platform/chassis/hw-mac-address"
)

func init() {
	discoverers.Register(NokiaSRLDiscovererName, func() discoverers.Discoverer {
		return &srl{}
	})
}

type srl struct{}

func (s *srl) ProviderName() string {
	return "srl.nokia.com"
}

func (s *srl) Discover(ctx context.Context, dr *invv1alpha1.DiscoveryRuleContext, t *target.Target) (*invv1alpha1.DiscoveryInfo, error) {
	req, err := api.NewGetRequest(
		api.Path(srlSwVersionPath),
		api.Path(srlChassisPath),
		api.Path(srlHostnamePath),
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
		Type:   "srl",
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
			case srlSwVersionPath:
				di.Version = getVersion(upd.GetVal().GetStringVal())
			case srlChassisTypePath:
				di.Platform = upd.GetVal().GetStringVal()
			case srlSerialNumberPath:
				di.SerialNumber = upd.GetVal().GetStringVal()
			case srlHWMacAddrPath:
				di.MacAddress = upd.GetVal().GetStringVal()
			case srlHostnamePath:
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

