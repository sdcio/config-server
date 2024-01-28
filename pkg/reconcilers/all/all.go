package all

import (
	_ "github.com/iptecharch/config-server/pkg/reconcilers/discoveryrule"
	_ "github.com/iptecharch/config-server/pkg/reconcilers/schema"
	_ "github.com/iptecharch/config-server/pkg/reconcilers/targetdatastore"
	_ "github.com/iptecharch/config-server/pkg/reconcilers/targetconfigserver"
	_ "github.com/iptecharch/config-server/pkg/reconcilers/targetconfigsetserver"
)
