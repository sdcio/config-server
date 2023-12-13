package iprange

import (
	"bytes"
	"net"
	"sort"
)

// getHosts gets all the IP address in a range
func getHosts(cidrs ...string) (map[string]struct{}, error) {
	ips := make(map[string]struct{})
	for _, cidr := range cidrs {
		ip, ipnet, err := net.ParseCIDR(cidr)
		if err != nil {
			return nil, err
		}

		for ip := ip.Mask(ipnet.Mask); ipnet.Contains(ip); incIP(ip) {
			ips[ip.String()] = struct{}{}
		}
	}
	return ips, nil
}

func incIP(ip net.IP) {
	for j := len(ip) - 1; j >= 0; j-- {
		ip[j]++
		if ip[j] > 0 {
			break
		}
	}
}

func sortIPs(hosts map[string]struct{}) []string {
	realIPs := make([]net.IP, 0, len(hosts))

	for ip := range hosts {
		realIPs = append(realIPs, net.ParseIP(ip))
	}

	sort.Slice(realIPs, func(i, j int) bool {
		return bytes.Compare(realIPs[i], realIPs[j]) < 0
	})

	ips := make([]string, 0, len(realIPs))
	for _, rip := range realIPs {
		ips = append(ips, rip.String())
	}
	return ips
}
