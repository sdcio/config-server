package sdcctx

import (
	dsclient "github.com/iptecharch/config-server/pkg/sdc/dataserver/client"
	ssclient "github.com/iptecharch/config-server/pkg/sdc/schemaserver/client"
	"k8s.io/apimachinery/pkg/util/sets"
)

type SSContext struct {
	Config   *ssclient.Config
	SSClient ssclient.Client // schemaserver client
}

type DSContext struct {
	Config   *dsclient.Config
	Targets  sets.Set[string]
	DSClient dsclient.Client // dataserver client
}
