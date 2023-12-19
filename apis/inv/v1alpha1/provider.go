package v1alpha1

import "strings"

func GetVendorType(provider string) (string, string) {
	split := strings.Split(provider, ".")
	if len(split) < 2 {
		return "", ""
	}
	return split[0], split[1]
}
