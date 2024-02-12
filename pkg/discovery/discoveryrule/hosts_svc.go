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

package discoveryrule

import (
	"context"
	"fmt"

	"github.com/henderiw/logger/log"
	invv1alpha1 "github.com/sdcio/config-server/apis/inv/v1alpha1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

func (r *dr) GetSVCDiscoveryAddresses(ctx context.Context) []invv1alpha1.DiscoveryRuleAddress {
	addresses := []invv1alpha1.DiscoveryRuleAddress{}
	log := log.FromContext(ctx)
	svcList, err := r.getServices(ctx)
	if err != nil {
		log.Error("cannot get service list", "error", err)
		return addresses
	}
	domainName := "cluster.local"
	if r.cfg.CR.GetServiceDomain() != "" {
		domainName = r.cfg.CR.GetServiceDomain()
	}
	for _, svc := range svcList.Items {
		addresses = append(addresses, invv1alpha1.DiscoveryRuleAddress{
			Address:  fmt.Sprintf("%s.%s.svc.%s", svc.Name, svc.Namespace, domainName),
			HostName: svc.Name,
		})
	}
	return addresses
}

func (r *dr) getServices(ctx context.Context) (*corev1.ServiceList, error) {
	if r.cfg.CR.GetSvcSelector() == nil {
		return nil, fmt.Errorf("get services w/o a labelselector is not supported")
	}
	labelsSelector, err := metav1.LabelSelectorAsSelector(r.cfg.CR.GetSvcSelector())
	if err != nil {
		return nil, err
	}

	listOpts := &client.ListOptions{}
	client.InNamespace(r.cfg.CR.GetNamespace()).ApplyToList(listOpts)
	client.MatchingLabelsSelector{Selector: labelsSelector}.ApplyToList(listOpts)

	svcList := &corev1.ServiceList{}
	if err := r.client.List(ctx, svcList, listOpts); err != nil {
		return nil, err
	}
	return svcList, nil
}
