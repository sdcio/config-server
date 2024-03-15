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

func (r *dr) GetPODDiscoveryAddresses(ctx context.Context) []invv1alpha1.DiscoveryRuleAddress {
	addresses := []invv1alpha1.DiscoveryRuleAddress{}
	log := log.FromContext(ctx)
	podList, err := r.getPods(ctx)
	if err != nil {
		log.Error("cannot get service list", "error", err)
		return addresses
	}
	for _, pod := range podList.Items {
		podIPs, err := getPodStatus(&pod)
		if err != nil {
			log.Info("pod not ready", "reason", err.Error())
			continue
		}
		addresses = append(addresses, invv1alpha1.DiscoveryRuleAddress{
			Address:  podIPs[0].String(),
			HostName: pod.Name,
		})
	}
	return addresses
}

func (r *dr) getPods(ctx context.Context) (*corev1.PodList, error) {
	if r.cfg.CR.GetPodSelector() == nil {
		return nil, fmt.Errorf("get pods w/o a labelselector is not supported")
	}
	labelsSelector, err := metav1.LabelSelectorAsSelector(r.cfg.CR.GetSvcSelector())
	if err != nil {
		return nil, err
	}

	listOpts := &client.ListOptions{}
	client.InNamespace(r.cfg.CR.GetNamespace()).ApplyToList(listOpts)
	client.MatchingLabelsSelector{Selector: labelsSelector}.ApplyToList(listOpts)

	podList := &corev1.PodList{}
	if err := r.client.List(ctx, podList, listOpts); err != nil {
		return nil, err
	}
	return podList, nil
}

func getPodStatus(pod *corev1.Pod) ([]corev1.PodIP, error) {
	if len(pod.Status.ContainerStatuses) == 0 {
		return nil, fmt.Errorf("pod conditions empty")
	}
	if !pod.Status.ContainerStatuses[0].Ready {
		return nil, fmt.Errorf("pod not ready")
	}
	if len(pod.Status.PodIPs) == 0 {
		return nil, fmt.Errorf("no pod ip(s) available")
	}
	return pod.Status.PodIPs, nil
}
