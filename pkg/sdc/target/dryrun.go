package target

import (
	"context"
	"fmt"

	"github.com/sdcio/config-server/apis/condition"
	"github.com/sdcio/config-server/apis/config"
	invv1alpha1 "github.com/sdcio/config-server/apis/inv/v1alpha1"
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
	target *invv1alpha1.Target,
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
	defer closeFn()

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
