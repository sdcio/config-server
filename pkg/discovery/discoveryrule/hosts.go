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
	"fmt"
	"net/netip"

	"github.com/henderiw/iputil"
	invv1alpha1 "github.com/iptecharch/config-server/apis/inv/v1alpha1"
)

type Iterator interface {
	Next() (*hostInfo, bool)
}

func (r *dr) getHosts(ctx context.Context) (Iterator, error) {
	kind, err := r.cfg.CR.GetDiscoveryKind()
	if err != nil {
		return nil, err
	}
	switch kind {
	case invv1alpha1.DiscoveryRuleSpecKindPrefix:
		prefixes, err := getPrefixes(r.cfg.CR.GetPrefixes())
		if err != nil {
			return nil, err
		}
		return newHostIterator(prefixes), nil
	case invv1alpha1.DiscoveryRuleSpecKindAddress:
		return newIterator(r.cfg.CR.GetAddresses(), r.cfg.CR.GetDefaultSchema()), nil
	case invv1alpha1.DiscoveryRuleSpecKindPod:
		return newIterator(r.GetSVCDiscoveryAddresses(ctx), r.cfg.CR.GetDefaultSchema()), nil
	case invv1alpha1.DiscoveryRuleSpecKindSvc:
		return newIterator(r.GetPODDiscoveryAddresses(ctx), r.cfg.CR.GetDefaultSchema()), nil
	default:
		return nil, fmt.Errorf("unsupported discovery rule kind, supporting %v, got %s",
			[]string{
				string(invv1alpha1.DiscoveryRuleSpecKindPrefix),
				string(invv1alpha1.DiscoveryRuleSpecKindAddress),
				string(invv1alpha1.DiscoveryRuleSpecKindSvc),
				string(invv1alpha1.DiscoveryRuleSpecKindPod),
			},
			r.cfg.CR.GetObjectKind().GroupVersionKind().Kind,
		)
	}
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
		hosts = append(hosts, a.Address)
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
	defaultSchema *invv1alpha1.SchemaKey
	Address       string
	hostName      string
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
			Address:  r.curAddr.String(),
			hostName: r.hostName,
		}, true
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
		})
	}
	return prefixes, nil
}

type iterator struct {
	defaultSchema *invv1alpha1.SchemaKey
	addresses     []invv1alpha1.DiscoveryRuleAddress
	index         int
}

func newIterator(addresses []invv1alpha1.DiscoveryRuleAddress, defaultSchema *invv1alpha1.SchemaKey) Iterator {
	return &iterator{
		defaultSchema: defaultSchema,
		addresses:     addresses,
		index:         -1, // start before the first element
	}
}

func (r *iterator) Next() (*hostInfo, bool) {
	r.index++
	if r.index < len(r.addresses) {
		hostName := r.addresses[r.index].HostName
		if hostName == "" {
			if _, err := netip.ParseAddr(hostName); err != nil {
				hostName = r.addresses[r.index].Address // dns
			} else {
				// TODO transform IP to a k8s compatible name
				hostName = r.addresses[r.index].Address
			}
		}
		return &hostInfo{
			defaultSchema: r.defaultSchema,
			Address:       r.addresses[r.index].Address,
			hostName:      hostName,
		}, true
	}
	return nil, false
}
