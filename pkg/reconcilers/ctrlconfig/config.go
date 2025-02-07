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

package ctrlconfig

import (
	"context"

	"github.com/henderiw/apiserver-store/pkg/storebackend"
	"github.com/henderiw/logger/log"
	sdcctx "github.com/sdcio/config-server/pkg/sdc/ctx"
	"github.com/sdcio/config-server/pkg/target"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/discovery"
	"sigs.k8s.io/controller-runtime/pkg/manager"
)

type ControllerConfig struct {
	TargetStore       storebackend.Storer[*target.Context]
	DataServerStore   storebackend.Storer[sdcctx.DSContext]
	SchemaServerStore storebackend.Storer[sdcctx.SSContext]
	SchemaDir         string
	WorkspaceDir      string
	TargetHandler     *target.TargetHandler
}

func InitContext(ctx context.Context, controllerName string, req types.NamespacedName) context.Context {
	l := log.FromContext(ctx).With("controller", controllerName, "req", req)
	return log.IntoContext(ctx, l)
}

func GetDiscoveryClient(mgr manager.Manager) (*discovery.DiscoveryClient, error) {
	config := mgr.GetConfig() // Get REST config from manager
	return discovery.NewDiscoveryClientForConfig(config)
}
