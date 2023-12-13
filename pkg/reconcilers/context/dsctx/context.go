package dsctx

import (
	dsclient "github.com/iptecharch/config-server/pkg/dataserver/client"
	"k8s.io/apimachinery/pkg/util/sets"
)

type Context struct {
	Config  *dsclient.Config
	Targets sets.Set[string]
	Client  dsclient.Client
}
