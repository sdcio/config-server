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
	"context"
	"os"

	"github.com/henderiw/apiserver-store/pkg/storebackend"
	"github.com/henderiw/logger/log"
	dsclient "github.com/sdcio/config-server/pkg/sdc/dataserver/client"
	"k8s.io/apimachinery/pkg/util/sets"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

type DSContext struct {
	Config   *dsclient.Config
	Targets  sets.Set[string]
	DSClient dsclient.Client // dataserver client
	client   client.Client
}

func CreateDataServerClient(ctx context.Context, dataServerStore storebackend.Storer[DSContext], client client.Client) error {
	log := log.FromContext(ctx)

	dataServerAddress := localDataServerAddress
	if address, found := os.LookupEnv("SDC_DATA_SERVER"); found {
		dataServerAddress = address
	}

	// TODO the population of the dataservers in the store should become dynamic, through a controller
	// right now it is static since all of this happens in the same pod and we dont have scaled out dataservers
	dsConfig := &dsclient.Config{
		Address: dataServerAddress,
	}
	dsClient, err := dsclient.New(dsConfig)
	if err != nil {
		log.Error("cannot initialize dataserver client", "err", err)
		return err
	}
	if err := dsClient.Start(ctx); err != nil {
		log.Error("cannot start dataserver client", "err", err)
		return err
	}
	dsCtx := DSContext{
		Config:   dsConfig,
		Targets:  sets.New[string](),
		DSClient: dsClient,
	}
	if err := dataServerStore.Create(ctx, storebackend.ToKey(dataServerAddress), dsCtx); err != nil {
		log.Error("cannot store datastore context in dataserver", "err", err)
		return err
	}
	log.Info("dataserver client created")
	return nil
}
