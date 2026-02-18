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
	"strings"
	"time"

	"github.com/henderiw/apiserver-store/pkg/storebackend"
	"github.com/henderiw/logger/log"

	//condv1alpha1 "github.com/sdcio/config-server/apis/condition/v1alpha1"
	invv1alpha1 "github.com/sdcio/config-server/apis/inv/v1alpha1"
	invv1alpha1apply "github.com/sdcio/config-server/pkg/generated/applyconfiguration/inv/v1alpha1"
	"github.com/sdcio/config-server/pkg/reconcilers/resource"
	"k8s.io/apimachinery/pkg/api/equality"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

const (
	reconcilerName = "DiscoveryController"
)

func (r *dr) createTarget(ctx context.Context, provider, address string, di *invv1alpha1.DiscoveryInfo) error {
	log := log.FromContext(ctx)
	if err := r.children.Create(ctx, storebackend.ToKey(getTargetName(di.HostName)), ""); err != nil {
		return err
	}

	newTarget, err := r.newTarget(
		ctx,
		provider,
		address,
		di,
	)
	if err != nil {
		return err
	}

	if err := r.applyTarget(ctx, newTarget); err != nil {
		log.Info("dynamic target creation failed", "error", err)
		return err
	}

	targetKey := newTarget.GetNamespacedName()
	target := &invv1alpha1.Target{}
	if err := r.client.Get(ctx, targetKey, target); err != nil {
		log.Info("cannot get target", "error", err)
		return err
	}

	
	if err := r.applyTargetDeviationCR(ctx, target); err != nil {
		return err
	}
	
	return nil
}

func (r *dr) newTarget(_ context.Context, providerName, address string, di *invv1alpha1.DiscoveryInfo) (*invv1alpha1.Target, error) {
	targetSpec := invv1alpha1.TargetSpec{
		Provider: providerName,
		Address:  address,
		TargetProfile: invv1alpha1.TargetProfile{
			Credentials: r.cfg.CR.GetDiscoveryParameters().TargetConnectionProfiles[0].Credentials,
			// TODO TLSSecret:
			ConnectionProfile: r.cfg.CR.GetDiscoveryParameters().TargetConnectionProfiles[0].ConnectionProfile,
			SyncProfile:       r.cfg.CR.GetDiscoveryParameters().TargetConnectionProfiles[0].SyncProfile,
		},
	}
	labels, err := r.cfg.CR.GetDiscoveryParameters().GetTargetLabels(r.cfg.CR.GetName())
	if err != nil {
		return nil, err
	}
	anno, err := r.cfg.CR.GetDiscoveryParameters().GetTargetAnnotations(r.cfg.CR.GetName())
	if err != nil {
		return nil, err
	}

	return invv1alpha1.BuildTarget(
		metav1.ObjectMeta{
			Name:        getTargetName(di.HostName),
			Namespace:   r.cfg.CR.GetNamespace(),
			Labels:      labels,
			Annotations: anno,
			OwnerReferences: []metav1.OwnerReference{
				{
					APIVersion: schema.GroupVersion{
						Group:   r.cfg.CR.GetObjectKind().GroupVersionKind().Group,
						Version: r.cfg.CR.GetObjectKind().GroupVersionKind().Version,
					}.String(),
					Kind:       r.cfg.CR.GetObjectKind().GroupVersionKind().Kind,
					Name:       r.cfg.CR.GetName(),
					UID:        r.cfg.CR.GetUID(),
					Controller: ptr.To[bool](true),
				}},
		},
		targetSpec,
		invv1alpha1.TargetStatus{
			DiscoveryInfo: di,
		},
	), nil
}

// w/o seperated discovery info

func (r *dr) applyTarget(ctx context.Context, newTarget *invv1alpha1.Target) error {
	//di := newTarget.Status.DiscoveryInfo.DeepCopy()
	log := log.FromContext(ctx).With("targetName", newTarget.Name, "address", newTarget.Spec.Address)

	// Check if the target already exists
	target := &invv1alpha1.Target{}
	if err := r.client.Get(ctx, types.NamespacedName{
		Namespace: newTarget.Namespace,
		Name:      newTarget.Name,
	}, target); err != nil {
		if resource.IgnoreNotFound(err) != nil {
			return err
		}
		log.Info("discovery target apply, target does not exist -> create")

		target := newTarget.DeepCopy()

		if err := r.client.Create(ctx, target, &client.CreateOptions{FieldManager: reconcilerName}); err != nil {
			return err
		}
		time.Sleep(500 * time.Millisecond)
	}

	newCond := invv1alpha1.DiscoveryReady()
	oldCond := target.GetCondition(invv1alpha1.ConditionTypeDiscoveryReady)

	if newCond.Equal(oldCond) &&
		equality.Semantic.DeepEqual(newTarget.Spec, target.Spec) &&
		equality.Semantic.DeepEqual(newTarget.Status.DiscoveryInfo, target.Status.DiscoveryInfo) {
		log.Info("handleSuccess -> no change")
		return nil
	}

	log.Info("handleSuccess",
		"condition change", !newCond.Equal(oldCond),
		"spec change", !equality.Semantic.DeepEqual(newTarget.Spec, target.Spec),
		"discovery info change", !equality.Semantic.DeepEqual(newTarget.Status.DiscoveryInfo, target.Status.DiscoveryInfo),
	)

	statusApply := invv1alpha1apply.TargetStatus().
		WithConditions(newCond)

	if newTarget.Status.DiscoveryInfo != nil {
		statusApply = statusApply.WithDiscoveryInfo(discoveryInfoToApply(newTarget.Status.DiscoveryInfo))
	}

	applyConfig := invv1alpha1apply.Target(newTarget.Name, newTarget.Namespace).
		WithStatus(statusApply)

	if err := r.client.Status().Apply(ctx, applyConfig, &client.SubResourceApplyOptions{
		ApplyOptions: client.ApplyOptions{
			FieldManager: reconcilerName,
		},
	}); err != nil {
		log.Error("failed to patch target status", "err", err)
		return err
	}
	return nil
}

func discoveryInfoToApply(di *invv1alpha1.DiscoveryInfo) *invv1alpha1apply.DiscoveryInfoApplyConfiguration {
	if di == nil {
		return nil
	}
	a := invv1alpha1apply.DiscoveryInfo()
	if di.Protocol != "" {
		a.WithProtocol(di.Protocol)
	}
	if di.Provider != "" {
		a.WithProvider(di.Provider)
	}
	if di.Version != "" {
		a.WithVersion(di.Version)
	}
	if di.HostName != "" {
		a.WithHostName(di.HostName)
	}
	if di.Platform != "" {
		a.WithPlatform(di.Platform)
	}
	if di.MacAddress != "" {
		a.WithMacAddress(di.MacAddress)
	}
	if di.SerialNumber != "" {
		a.WithSerialNumber(di.SerialNumber)
	}
	return a
}

func getTargetName(s string) string {
	targetName := strings.ReplaceAll(s, ":", "-")
	return strings.ToLower(targetName)
}
