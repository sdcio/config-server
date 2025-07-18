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

package all

import (
	//_ "github.com/sdcio/config-server/pkg/reconcilers/config"
	_ "github.com/sdcio/config-server/pkg/reconcilers/configset"
	_ "github.com/sdcio/config-server/pkg/reconcilers/discoveryrule"
	_ "github.com/sdcio/config-server/pkg/reconcilers/schema"
	_ "github.com/sdcio/config-server/pkg/reconcilers/target"
	_ "github.com/sdcio/config-server/pkg/reconcilers/targetrecovery"
	_ "github.com/sdcio/config-server/pkg/reconcilers/targetconfig"
	_ "github.com/sdcio/config-server/pkg/reconcilers/targetdatastore"
	_ "github.com/sdcio/config-server/pkg/reconcilers/subscription"
	_ "github.com/sdcio/config-server/pkg/reconcilers/workspace"
	_ "github.com/sdcio/config-server/pkg/reconcilers/rollout"
)
