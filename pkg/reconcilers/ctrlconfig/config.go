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
	"fmt"
	"strings"

	"github.com/henderiw/logger/log"
	"github.com/sdcio/config-server/pkg/keyring"
	dsmanager "github.com/sdcio/config-server/pkg/sdc/dataserver/manager"
	targetmanager "github.com/sdcio/config-server/pkg/sdc/target/manager"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/discovery"
	"sigs.k8s.io/controller-runtime/pkg/event"
	"sigs.k8s.io/controller-runtime/pkg/manager"
)

type ControllerConfig struct {
	SchemaDir         string
	WorkspaceDir      string
	DataServerManager *dsmanager.DSConnManager
	TargetManager     *targetmanager.TargetManager

	// keyRing is unexported on purpose. Which reconcilers need a keyring is
	// knowledge that belongs with those reconcilers, not with a predicate in
	// main() — gating on IsLocalDataServerEnabled() is correct only for the
	// reconcilers that also need a TargetManager, and would silently be wrong
	// for the Resolver, which needs a key and never touches the data-server.
	// Consumers call RequireKeyRing during SetupWithManager, so a
	// misconfiguration fails at startup with a message naming the reconciler
	// instead of nil-panicking on the first Encrypt.
	keyRing *keyring.KeyRing

	// KeyRotationEvents wakes re-encryption consumers when the keyring primary
	// changes. nil when no keyring is loaded. Fed by keyring.FileWatcher via
	// keyring.Load; consumed with source.Channel.
	KeyRotationEvents chan event.GenericEvent
}

// SetKeyRing installs the keyring and its rotation channel. Both may be nil,
// meaning no keyring is mounted.
func (c *ControllerConfig) SetKeyRing(kr *keyring.KeyRing, rotation chan event.GenericEvent) {
	c.keyRing = kr
	c.KeyRotationEvents = rotation
}

// HasKeyRing reports whether a keyring is available, for callers that can
// degrade rather than fail.
func (c *ControllerConfig) HasKeyRing() bool { return c.keyRing != nil }

// RequireKeyRing returns the keyring, or an error naming both the reconciler
// that needs it and the two ways to resolve the situation.
//
// Call this from SetupWithManager and return the error — main already exits on
// a failed reconciler setup.
func (c *ControllerConfig) RequireKeyRing(reconcilerName string) (*keyring.KeyRing, error) {
	if c.keyRing == nil {
		return nil, fmt.Errorf(
			"reconciler %q requires a keyring: mount the keyring secret (%s, default %s) or set ENABLE_%s=false",
			reconcilerName, keyring.PathEnvVar, keyring.DefaultPath, strings.ToUpper(reconcilerName),
		)
	}
	return c.keyRing, nil
}

func InitContext(ctx context.Context, controllerName string, req types.NamespacedName) context.Context {
	l := log.FromContext(ctx).With("controller", controllerName, "req", req)
	return log.IntoContext(ctx, l)
}

func GetDiscoveryClient(mgr manager.Manager) (*discovery.DiscoveryClient, error) {
	config := mgr.GetConfig() // Get REST config from manager
	return discovery.NewDiscoveryClientForConfig(config)
}