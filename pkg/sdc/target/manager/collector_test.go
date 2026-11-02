/*
Copyright 2026 Nokia.

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

package targetmanager

import (
	"context"
	"fmt"
	"net"
	"testing"

	"github.com/henderiw/apiserver-store/pkg/storebackend"
	sdcpb "github.com/sdcio/sdc-protos/sdcpb"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestCollectorSubscriptionPort(t *testing.T) {
	for _, override := range []bool{false, true} {
		name := "connection profile fallback"
		if override {
			name = "subscription override"
		}
		t.Run(name, func(t *testing.T) {
			req := newCollectorTestRequest(t)
			wantPort := req.Target.Port
			collector := NewCollector(storebackend.Key{}, NewSubscriptions())
			if override {
				wantPort = newCollectorTestRequest(t).Target.Port
				collector.SetPort(uint(wantPort))
			}
			ctx := context.Background()
			t.Cleanup(func() { collector.Stop(ctx) })

			require.NoError(t, collector.Start(ctx, req))
			require.NotNil(t, collector.target)
			assert.Equal(t, net.JoinHostPort("127.0.0.1", fmt.Sprint(wantPort)), collector.target.Config.Address)
		})
	}
}

func newCollectorTestRequest(t *testing.T) *sdcpb.CreateDataStoreRequest {
	t.Helper()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	t.Cleanup(func() { _ = listener.Close() })
	return &sdcpb.CreateDataStoreRequest{
		Target: &sdcpb.Target{
			Type:        "netconf",
			Address:     "127.0.0.1",
			Port:        uint32(listener.Addr().(*net.TCPAddr).Port),
			Credentials: &sdcpb.Credentials{},
		},
	}
}
