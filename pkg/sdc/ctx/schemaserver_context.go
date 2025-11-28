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
	"time"

	"github.com/henderiw/apiserver-store/pkg/storebackend"
	"github.com/henderiw/logger/log"
	invv1alpha1 "github.com/sdcio/config-server/apis/inv/v1alpha1"
	ssclient "github.com/sdcio/config-server/pkg/sdc/schemaserver/client"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

type SSContext struct {
	Config      *ssclient.Config
	SSClient    ssclient.Client // schemaserver client
	client      client.Client
	cancelWatch context.CancelFunc
}

const (
	localDataServerAddress = "localhost:56000"
	SchemaServerAddress = "data-server-0.schema-server.sdc-system.svc.cluster.local:56000"
)

func CreateSchemaServerClient(ctx context.Context, schemaServerStore storebackend.Storer[SSContext], client client.Client) error {
	log := log.FromContext(ctx)

	// For the schema server we first check if the SDC_SCHEMA_SERVER was et if not we could also use
	// the SDC_DATA_SERVER as fallback. If none are set it is the default address (localhost)
	schemaServerAddress := SchemaServerAddress
	if address, found := os.LookupEnv("SDC_SCHEMA_SERVER"); found {
		schemaServerAddress = address
	} else {
		if address, found := os.LookupEnv("SDC_DATA_SERVER"); found {
			schemaServerAddress = address
		}
	}

	ssConfig := &ssclient.Config{
		Address: schemaServerAddress,
	}
	ssClient, err := ssclient.New(ssConfig)
	if err != nil {
		log.Error("cannot initialize schemaserver client", "err", err)
		return err
	}
	if err := ssClient.Start(ctx); err != nil {
		log.Error("cannot start schemaserver client", "err", err)
		return err
	}

	watchCtx, cancelWatch := context.WithCancel(ctx)
	ssCtx := SSContext{
		Config:      ssConfig,
		SSClient:    ssClient,
		cancelWatch: cancelWatch,
		client:      client,
	}

	go ssCtx.watchSchemaServerConnection(watchCtx)

	if err := schemaServerStore.Create(ctx, storebackend.ToKey(schemaServerAddress), ssCtx); err != nil {
		log.Error("cannot store schema context in schemaserver", "err", err)
		return err
	}
	log.Info("schemaserver client created")
	return nil
}

func (r *SSContext) watchSchemaServerConnection(ctx context.Context) {
	log := log.FromContext(ctx)
	prevState := r.SSClient.IsConnectionReady()

	ticker := time.NewTicker(10 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			log.Info("Stopping schema server connection watcher")
			return
		case <-ticker.C:
			currentState := r.SSClient.IsConnectionReady()
			if currentState != prevState {
				log.Info("Schema server connection state changed", "ready", currentState)

				schemaList := &invv1alpha1.SchemaList{}
				if err := r.client.List(ctx, schemaList); err != nil {
					log.Error("cannot get schemas", "error", err)
				}

				for _, schema := range schemaList.Items {
					patch := client.MergeFrom(schema.DeepCopy())
					if currentState {
						schema.SetConditions(invv1alpha1.SchemaServerReady())
					} else {
						schema.SetConditions(invv1alpha1.SchemaServerFailed())
					}
					if err := r.client.Status().Patch(ctx, &schema, patch, &client.SubResourcePatchOptions{
						PatchOptions: client.PatchOptions{
							FieldManager: "schema server watcher",
						},
					}); err != nil {
						log.Error("cannot get schemas", "error", err)
					}
				}
				prevState = currentState
			}
		}
	}
}

func DeleteSchemaServerClient(ctx context.Context, schemaServerStore storebackend.Storer[SSContext], schemaServerAddress string) error {
	log := log.FromContext(ctx)

	ssCtx, err := schemaServerStore.Get(ctx, storebackend.ToKey(schemaServerAddress))
	if err != nil {
		log.Error("cannot retrieve schema context", "err", err)
		return err
	}

	ssCtx.cancelWatch() // Stop the watcher

	if err := schemaServerStore.Delete(ctx, storebackend.ToKey(schemaServerAddress)); err != nil {
		log.Error("cannot delete schema context", "err", err)
		return err
	}

	log.Info("schemaserver client deleted")
	return nil
}
