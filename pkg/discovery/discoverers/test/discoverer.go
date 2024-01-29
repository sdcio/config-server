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

package test

import (
	"context"

	invv1alpha1 "github.com/iptecharch/config-server/apis/inv/v1alpha1"
	"github.com/iptecharch/config-server/pkg/discovery/discoverers"
	"github.com/openconfig/gnmic/pkg/target"
)

const (
	Provider = "test.example.sdcio.dev"
)

func init() {
	discoverers.Register(Provider, func() discoverers.Discoverer {
		return &discoverer{}
	})
}

type discoverer struct{}

func (s *discoverer) GetProvider() string {
	return Provider
}

func (s *discoverer) Discover(ctx context.Context, t *target.Target) (*invv1alpha1.DiscoveryInfo, error) {
	return &invv1alpha1.DiscoveryInfo{}, nil
}
