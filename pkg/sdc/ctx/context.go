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
