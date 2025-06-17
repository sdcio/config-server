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

package client

import (
	"context"

	sdcpb "github.com/sdcio/sdc-protos/sdcpb"
	"google.golang.org/grpc"
)

func NewFakeClient() Client {
	return &fakeclient{}
}

type fakeclient struct{}

func (r *fakeclient) IsConnectionReady() bool { return true }

func (r *fakeclient) IsConnected() bool { return true }

func (r *fakeclient) Start(ctx context.Context) error { return nil }

func (r *fakeclient) Stop(ctx context.Context) {}

func (r *fakeclient) GetAddress() string { return "" }

func (r *fakeclient) ListDataStore(ctx context.Context, in *sdcpb.ListDataStoreRequest, opts ...grpc.CallOption) (*sdcpb.ListDataStoreResponse, error) {
	return &sdcpb.ListDataStoreResponse{}, nil
}

func (r *fakeclient) GetDataStore(ctx context.Context, in *sdcpb.GetDataStoreRequest, opts ...grpc.CallOption) (*sdcpb.GetDataStoreResponse, error) {
	return &sdcpb.GetDataStoreResponse{}, nil
}

func (r *fakeclient) CreateDataStore(ctx context.Context, in *sdcpb.CreateDataStoreRequest, opts ...grpc.CallOption) (*sdcpb.CreateDataStoreResponse, error) {
	return &sdcpb.CreateDataStoreResponse{}, nil
}

func (r *fakeclient) DeleteDataStore(ctx context.Context, in *sdcpb.DeleteDataStoreRequest, opts ...grpc.CallOption) (*sdcpb.DeleteDataStoreResponse, error) {
	return &sdcpb.DeleteDataStoreResponse{}, nil
}

func (r *fakeclient) GetIntent(ctx context.Context, in *sdcpb.GetIntentRequest, opts ...grpc.CallOption) (*sdcpb.GetIntentResponse, error) {
	return &sdcpb.GetIntentResponse{}, nil
}

func (r *fakeclient) TransactionSet(ctx context.Context, in *sdcpb.TransactionSetRequest, opts ...grpc.CallOption) (*sdcpb.TransactionSetResponse, error) {
	return &sdcpb.TransactionSetResponse{}, nil
}

func (r *fakeclient) TransactionConfirm(ctx context.Context, in *sdcpb.TransactionConfirmRequest, opts ...grpc.CallOption) (*sdcpb.TransactionConfirmResponse, error) {
	return &sdcpb.TransactionConfirmResponse{}, nil
}

func (r *fakeclient) TransactionCancel(ctx context.Context, in *sdcpb.TransactionCancelRequest, opts ...grpc.CallOption) (*sdcpb.TransactionCancelResponse, error) {
	return &sdcpb.TransactionCancelResponse{}, nil
}

func (r *fakeclient) ListIntent(ctx context.Context, in *sdcpb.ListIntentRequest, opts ...grpc.CallOption) (*sdcpb.ListIntentResponse, error) {
	return &sdcpb.ListIntentResponse{}, nil
}

func (r *fakeclient) WatchDeviations(ctx context.Context, in *sdcpb.WatchDeviationRequest, opts ...grpc.CallOption) (sdcpb.DataServer_WatchDeviationsClient, error) {
	return nil, nil
}

func (r *fakeclient) BlameConfig(ctx context.Context, in *sdcpb.BlameConfigRequest, opts ...grpc.CallOption) (*sdcpb.BlameConfigResponse, error) {
	return &sdcpb.BlameConfigResponse{}, nil
}
