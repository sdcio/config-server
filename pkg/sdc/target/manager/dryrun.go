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

	"github.com/sdcio/config-server/apis/condition"
	"github.com/sdcio/config-server/apis/config"
	configv1alpha1 "github.com/sdcio/config-server/apis/config/v1alpha1"
	dsclient "github.com/sdcio/config-server/pkg/sdc/dataserver/client"
	sdcpb "github.com/sdcio/sdc-protos/sdcpb"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/utils/ptr"
)

// runDryRunTransaction opens a short-lived data-server client, executes a
// TransactionSet dry-run with the provided intents, processes the result,
// and updates the Config status/conditions.
func RunDryRunTransaction(
	ctx context.Context,
	key types.NamespacedName,
	c *config.Config,
	target *configv1alpha1.Target,
	intents []*sdcpb.TransactionIntent,
	dryrun bool,
) (runtime.Object, error) {
	cfg := &dsclient.Config{
		Address:  dsclient.GetDataServerAddress(),
		Insecure: true,
	}

	dsClient, closeFn, err := dsclient.NewEphemeral(ctx, cfg)
	if err != nil {
		return nil, err
	}
	defer func() {
		if err := closeFn(); err != nil {
			// You can use your preferred logging framework here
			fmt.Printf("failed to close connection: %v\n", err)
		}
	}()

	req := &sdcpb.TransactionSetRequest{
		TransactionId: GetGVKNSN(c),
		DatastoreName: key.String(),
		DryRun:        dryrun,
		Timeout:       ptr.To(int32(60)),
		Intents:       intents,
	}

	rsp, rpcErr := dsClient.TransactionSet(ctx, req)

	warnings, aggErr := processTransactionResponse(ctx, rsp, rpcErr)
	if aggErr != nil {
		msg := fmt.Sprintf("%s err %s", warnings, aggErr.Error())
		c.SetConditions(condition.Failed(msg))
		return c, aggErr
	}

	c.SetConditions(condition.ReadyWithMsg(warnings))
	c.Status.LastKnownGoodSchema = &config.ConfigStatusLastKnownGoodSchema{
		Vendor:  target.Status.DiscoveryInfo.Provider,
		Version: target.Status.DiscoveryInfo.Version,
	}
	c.Status.AppliedConfig = &c.Spec
	return c, nil
}
