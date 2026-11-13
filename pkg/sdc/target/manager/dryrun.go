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
	"errors"
	"fmt"
	"strings"

	"github.com/henderiw/apiserver-store/pkg/storebackend"
	"github.com/sdcio/config-server/apis/condition"
	"github.com/sdcio/config-server/apis/config"
	configv1alpha1 "github.com/sdcio/config-server/apis/config/v1alpha1"
	dsclient "github.com/sdcio/config-server/pkg/sdc/dataserver/client"
	sdcpb "github.com/sdcio/sdc-protos/sdcpb"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/utils/ptr"
)

// RunDryRunTransaction opens a short-lived data-server client, executes a
// TransactionSet dry-run with the provided intents, processes the result,
// and updates the Config status/conditions.
//
// It is used by the api-server, a separate deployment that reaches the
// data-server over its :56000 listener; the controller never calls it.
func RunDryRunTransaction(
	ctx context.Context,
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

	targetKey := storebackend.KeyFromNSN(target.GetNamespacedName())

	req := &sdcpb.TransactionSetRequest{
		TransactionId: config.GetGVKNSN(c),
		DatastoreName: targetKey.String(),
		DryRun:        dryrun,
		Timeout:       ptr.To(int32(60)),
		Intents:       intents,
	}

	rsp, rpcErr := dsClient.TransactionSet(ctx, req)

	warnings, aggErr := processWireTransactionResponse(rsp, rpcErr)
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

// processWireTransactionResponse aggregates the warnings and errors of a wire
// TransactionSetResponse. The api-server only reports them, so the error
// carries no recoverability.
func processWireTransactionResponse(rsp *sdcpb.TransactionSetResponse, rsperr error) (string, error) {
	var errs error
	var warnings []string
	if rsperr != nil {
		errs = errors.Join(errs, fmt.Errorf("error: %s", rsperr.Error()))
	}
	if rsp != nil {
		for _, warning := range rsp.Warnings {
			warnings = append(warnings, fmt.Sprintf("global warning: %q", warning))
		}
		for key, intent := range rsp.Intents {
			for _, intentError := range intent.Errors {
				errs = errors.Join(errs, fmt.Errorf("intent %q error: %q", key, intentError))
			}
			for _, intentWarning := range intent.Warnings {
				warnings = append(warnings, fmt.Sprintf("intent %q warning: %q", key, intentWarning))
			}
		}
	}
	var err error
	if errs != nil {
		err = NewTransactionError(errs, false)
	}
	return strings.Join(warnings, "; "), err
}
