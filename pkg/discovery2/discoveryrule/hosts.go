package discoveryrule

import (
	"context"
	"fmt"
	"net/netip"

	"github.com/henderiw/iputil"
	invv1alpha1 "github.com/iptecharch/config-server/apis/inv/v1alpha1"
)

func (r *dr) getHosts(ctx context.Context) (*hostIterator, error) {
	switch r.cfg.CR.GetDiscoveryKind() {
	case invv1alpha1.DiscoveryRuleSpecKindIP:

		prefixes, err := getPrefixes(r.cfg.CR.GetPrefixes())
		if err != nil {
			return nil, err
		}
		return newHostIterator(prefixes), nil
	case invv1alpha1.DiscoveryRuleSpecKindPOD:
		// TODO list svc based on selector
		// add the prefixes
		// return host selector
	case invv1alpha1.DiscoveryRuleSpecKindSVC:
		// TODO list svc based on selector
		// add the prefixes
		// return host selector
	default:
		return nil, fmt.Errorf("unsupported discovery rule kind, supporting %v, got %s",
			[]string{
				string(invv1alpha1.DiscoveryRuleSpecKindIP),
				string(invv1alpha1.DiscoveryRuleSpecKindSVC),
				string(invv1alpha1.DiscoveryRuleSpecKindPOD),
			},
			r.cfg.CR.GetObjectKind().GroupVersionKind().Kind,
		)
	}
	return nil, nil
}

func getHosts(drPrefixes []invv1alpha1.DiscoveryRulePrefix) ([]string, error) {
	prefixes, err := getPrefixes(drPrefixes)
	if err != nil {
		return nil, err
	}
	hi := newHostIterator(prefixes)
	hosts := []string{}
	for {
		a, ok := hi.Next()
		if !ok {
			break
		}
		hosts = append(hosts, a.String())
	}
	return hosts, nil
}

func newHostIterator(prefixes []prefix) *hostIterator {
	hi := &hostIterator{
		prefixes: make([]prefixIterator, 0, len(prefixes)),
		idx:      0,
	}
	for _, prefix := range prefixes {
		hi.prefixes = append(hi.prefixes, prefixIterator{
			prefix:  prefix,
			curAddr: netip.Addr{},
			init:    true,
		})
	}
	return hi
}

type prefix struct {
	*iputil.Prefix                  // src
	excludes       []*iputil.Prefix // excludes
	hostName       string
}

type hostIterator struct {
	prefixes []prefixIterator
	idx      int
}

type prefixIterator struct {
	prefix
	//
	init    bool
	curAddr netip.Addr
}

type hostInfo struct {
	netip.Addr
	hostName string
}

func (r *hostIterator) Next() (*hostInfo, bool) {
	if r.idx < len(r.prefixes) {
		h, ok := r.prefixes[r.idx].Next()
		if !ok { // no valid address is returned -> go to the next idx
			r.idx++
			return r.Next()
		}
		return h, true
	}
	return &hostInfo{}, false
}

func (r *prefixIterator) Next() (*hostInfo, bool) {
	if r.init {
		r.curAddr = r.GetFirstIPAddress()
		r.init = false
	} else {
		r.curAddr = r.curAddr.Next()
	}
	if r.Contains(r.curAddr) {
		if isExcluded(r.curAddr, r.excludes) {
			return r.Next()
		}
		return &hostInfo{
			Addr:     r.curAddr,
			hostName: r.hostName}, true
	}
	return &hostInfo{}, false
}

func isExcluded(a netip.Addr, excludes []*iputil.Prefix) bool {
	for _, excludedPrefix := range excludes {
		if excludedPrefix.Contains(a) {
			return true
		}
	}
	return false
}

func getPrefixes(drPrefixes []invv1alpha1.DiscoveryRulePrefix) ([]prefix, error) {
	prefixes := make([]prefix, 0, len(drPrefixes))
	for _, drPrefix := range drPrefixes {
		pi, err := iputil.New(drPrefix.Prefix)
		if err != nil {
			return prefixes, err
		}
		excludes := make([]*iputil.Prefix, 0, len(drPrefix.Excludes))
		for _, exclPrefix := range drPrefix.Excludes {
			pi, err := iputil.New(exclPrefix)
			if err != nil {
				return prefixes, err
			}
			excludes = append(excludes, pi)
		}
		prefixes = append(prefixes, prefix{
			Prefix:   pi,
			excludes: excludes,
			hostName: drPrefix.HostName,
		})
	}
	return prefixes, nil
}

/*
type IP struct {
	DRPrefixes []invv1alpha1.DiscoveryRuleIPPrefix
}

func (r *IP) getHosts() (Hosts, error) {
	for _, drPrefix := range r.DRPrefixes {
		p, err := iputil.New(drPrefix.Prefix)
		if err != nil {
			return nil, err
		}

	}

}
*/
