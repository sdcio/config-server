// Copyright 2023 The xxx Authors.
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//    http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package configserver

import (
	"context"

	configv1alpha1 "github.com/iptecharch/config-server/apis/config/v1alpha1"
	memstore "github.com/iptecharch/config-server/pkg/store/memory"
	"github.com/iptecharch/config-server/pkg/target"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	v1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/apiserver-runtime/pkg/builder/resource"
)

var _ = Describe("ConfigServer Testing", func() {
	var obj resource.Object
	obj = &configv1alpha1.Config{}

	configServer := &configCommon{
		// client TODO
		configStore:    memstore.NewStore[runtime.Object](),
		configSetStore: memstore.NewStore[runtime.Object](),
		targetStore:    memstore.NewStore[target.Context](),
		gr:             configv1alpha1.Resource("config"),
		isNamespaced:   true,
		newFunc:        func() runtime.Object { return obj.New() },
		newListFunc:    func() runtime.Object { return obj.NewList() },
	}

	ctx := context.Background()

	_, err := configServer.get(ctx, "dummy", &v1.GetOptions{})
	Ω(err).Should((Succeed()), "Failed to create ipam backend")

})
