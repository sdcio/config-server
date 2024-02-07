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

func (r *fakeclient) Commit(ctx context.Context, in *sdcpb.CommitRequest, opts ...grpc.CallOption) (*sdcpb.CommitResponse, error) {
	return &sdcpb.CommitResponse{}, nil
}

func (r *fakeclient) Rebase(ctx context.Context, in *sdcpb.RebaseRequest, opts ...grpc.CallOption) (*sdcpb.RebaseResponse, error) {
	return &sdcpb.RebaseResponse{}, nil
}

func (r *fakeclient) Discard(ctx context.Context, in *sdcpb.DiscardRequest, opts ...grpc.CallOption) (*sdcpb.DiscardResponse, error) {
	return &sdcpb.DiscardResponse{}, nil
}

func (r *fakeclient) GetData(ctx context.Context, in *sdcpb.GetDataRequest, opts ...grpc.CallOption) (sdcpb.DataServer_GetDataClient, error) {
	return nil, nil
}

func (r *fakeclient) SetData(ctx context.Context, in *sdcpb.SetDataRequest, opts ...grpc.CallOption) (*sdcpb.SetDataResponse, error) {
	return &sdcpb.SetDataResponse{}, nil
}

func (r *fakeclient) Diff(ctx context.Context, in *sdcpb.DiffRequest, opts ...grpc.CallOption) (*sdcpb.DiffResponse, error) {
	return &sdcpb.DiffResponse{}, nil
}

func (r *fakeclient) Subscribe(ctx context.Context, in *sdcpb.SubscribeRequest, opts ...grpc.CallOption) (sdcpb.DataServer_SubscribeClient, error) {
	return nil, nil
}

func (r *fakeclient) Watch(ctx context.Context, in *sdcpb.WatchRequest, opts ...grpc.CallOption) (sdcpb.DataServer_WatchClient, error) {
	return nil, nil
}

func (r *fakeclient) GetIntent(ctx context.Context, in *sdcpb.GetIntentRequest, opts ...grpc.CallOption) (*sdcpb.GetIntentResponse, error) {
	return &sdcpb.GetIntentResponse{}, nil
}

func (r *fakeclient) SetIntent(ctx context.Context, in *sdcpb.SetIntentRequest, opts ...grpc.CallOption) (*sdcpb.SetIntentResponse, error) {
	return &sdcpb.SetIntentResponse{}, nil
}

func (r *fakeclient) ListIntent(ctx context.Context, in *sdcpb.ListIntentRequest, opts ...grpc.CallOption) (*sdcpb.ListIntentResponse, error) {
	return &sdcpb.ListIntentResponse{}, nil
}
