package discoveryrule

import (
	"fmt"
	"strings"
)

type Key struct {
	APIVersion string
	Kind       string
	Namespace  string
	Name       string
}

func (r Key) String() string {
	return fmt.Sprintf("%s#%s#%s#%s",
		r.APIVersion,
		r.Kind,
		r.Namespace,
		r.Name,
	)
}

func GetKey(s string) Key {
	split := strings.Split(s, "#")
	if len(split) != 5 {
		return Key{}
	}
	return Key{
		APIVersion: split[0],
		Kind:       split[1],
		Namespace:  split[2],
		Name:       split[3],
	}
}
