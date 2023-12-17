package dsctx

import (
	dsclient "github.com/iptecharch/config-server/pkg/sdc/dataserver/client"
	ssclient "github.com/iptecharch/config-server/pkg/sdc/schemaserver/client"
	"k8s.io/apimachinery/pkg/util/sets"
)

type Context struct {
	Config   *dsclient.Config
	Targets  sets.Set[string]
	DSClient dsclient.Client // dataserver client
	SSClient ssclient.Client // schemaserver client
}
