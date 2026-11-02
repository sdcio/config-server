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

	"github.com/henderiw/apiserver-store/pkg/storebackend"
	"github.com/henderiw/logger/log"

	configv1alpha1 "github.com/sdcio/config-server/apis/config/v1alpha1"
	invv1alpha1 "github.com/sdcio/config-server/apis/inv/v1alpha1"
	configv1alpha1apply "github.com/sdcio/config-server/pkg/generated/applyconfiguration/config/v1alpha1"
	"k8s.io/apimachinery/pkg/api/equality"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	v1 "k8s.io/client-go/applyconfigurations/meta/v1"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

const (
	specFieldManager   = "DiscoveryRuleController-spec"
	statusFieldManager = "DiscoveryRuleController-status"
)

func (r *dr) createTarget(ctx context.Context, provider, address string, di *configv1alpha1.DiscoveryInfo) error {
	log := log.FromContext(ctx)
	log.Info("createTarget start", "hostname", di.Hostname, "address", address)
	if err := r.children.Apply(ctx, storebackend.ToKey(getTargetName(di.Hostname)), ""); err != nil {
		log.Warn("children.apply", "error", err)
		return err
	}

	targetKey := types.NamespacedName{
		Namespace: r.cfg.CR.GetNamespace(),
		Name:      getTargetName(di.Hostname),
	}

	log.Info("applyTargetSpec start", "target", targetKey)
	if err := r.applyTargetSpec(ctx, targetKey, provider, address); err != nil {
		log.Error("dynamic target creation failed", "error", err)
		return err
	}

	log.Info("applyTargetSpec done", "target", targetKey)
	target, err := r.applyTargetStatus(ctx, targetKey, di)
	if err != nil {
		log.Error("dynamic target status apply failed", "error", err)
		return err
	}
	log.Info("applyTargetStatus done", "target", targetKey)

	if err := r.applyTargetDeviationCR(ctx, target); err != nil {
		log.Error("apply deviation failed", "error", err)
		return err
	}
	log.Info("createTarget done", "target", targetKey)
	return nil
}

func (r *dr) applyTargetSpec(ctx context.Context, targetKey types.NamespacedName, provider, address string) error {
	//di := newTarget.Status.DiscoveryInfo.DeepCopy()
	log := log.FromContext(ctx).With("target", targetKey)

	labels, err := r.cfg.CR.GetDiscoveryParameters().GetTargetLabels(r.cfg.CR.GetName())
	if err != nil {
		return err
	}
	anno, err := r.cfg.CR.GetDiscoveryParameters().GetTargetAnnotations(r.cfg.CR.GetName())
	if err != nil {
		return err
	}

	applyConfig := configv1alpha1apply.Target(targetKey.Name, targetKey.Namespace).
		WithLabels(labels).
		WithAnnotations(anno).
		WithOwnerReferences(ownerRefsToApply([]metav1.OwnerReference{
			{
				APIVersion: schema.GroupVersion{
					Group:   r.cfg.CR.GetObjectKind().GroupVersionKind().Group,
					Version: r.cfg.CR.GetObjectKind().GroupVersionKind().Version,
				}.String(),
				Kind:       r.cfg.CR.GetObjectKind().GroupVersionKind().Kind,
				Name:       r.cfg.CR.GetName(),
				UID:        r.cfg.CR.GetUID(),
				Controller: ptr.To[bool](true),
			}})...).
		WithSpec(specToApply(&configv1alpha1.TargetSpec{
			Provider: provider,
			Address:  address,
			TargetProfile: invv1alpha1.TargetProfile{
				Credentials: r.cfg.CR.GetDiscoveryParameters().TargetConnectionProfiles[0].Credentials,
				// TODO TLSSecret:
				ConnectionProfile: r.cfg.CR.GetDiscoveryParameters().TargetConnectionProfiles[0].ConnectionProfile,
				SyncProfile:       r.cfg.CR.GetDiscoveryParameters().TargetConnectionProfiles[0].SyncProfile,
			},
		}))

	if err := r.client.Apply(ctx, applyConfig, &client.ApplyOptions{
		FieldManager: specFieldManager,
	}); err != nil {
		log.Error("failed to apply target spec", "err", err)
		return err
	}
	return nil
}

// applyTargetStatus applies status (condition + discoveryInfo) via SSA on the status subresource
func (r *dr) applyTargetStatus(ctx context.Context, targetKey types.NamespacedName, di *configv1alpha1.DiscoveryInfo) (*configv1alpha1.Target, error) {
	log := log.FromContext(ctx).With("targetKey", targetKey)

	// Check current state to avoid unnecessary updates
	target := &configv1alpha1.Target{}
	targetExists := true
	if err := r.client.Get(ctx, targetKey, target); err != nil {
		if client.IgnoreNotFound(err) != nil {
			return nil, err
		}
		// Object just created — cache hasn't synced yet, skip optimization
		targetExists = false
	}

	if targetExists {
		newCond := configv1alpha1.TargetDiscoveryReady()
		oldCond := target.GetCondition(configv1alpha1.ConditionTypeTargetDiscoveryReady)
		if newCond.Equal(oldCond) &&
			equality.Semantic.DeepEqual(di, target.Status.DiscoveryInfo) {
			log.Info("applyTargetStatus -> no change")
			return target, nil
		}
		log.Info("applyTargetStatus",
			"condition change", !newCond.Equal(oldCond),
			"discovery info change", !equality.Semantic.DeepEqual(di, target.Status.DiscoveryInfo),
		)
	}

	statusApply := configv1alpha1apply.TargetStatus().
		WithConditions(configv1alpha1.TargetDiscoveryReady())
	if di != nil {
		statusApply = statusApply.WithDiscoveryInfo(discoveryInfoToApply(di))
	}

	applyConfig := configv1alpha1apply.Target(targetKey.Name, targetKey.Namespace).
		WithStatus(statusApply)

	if err := r.client.Status().Apply(ctx, applyConfig, &client.SubResourceApplyOptions{
		ApplyOptions: client.ApplyOptions{
			FieldManager: statusFieldManager,
		},
	}); err != nil {
		log.Error("failed to apply target status", "err", err)
		return nil, err
	}

	// Re-fetch to return fresh object
	if err := r.client.Get(ctx, targetKey, target); err != nil {
		return nil, err
	}
	return target, nil
}

func discoveryInfoToApply(di *configv1alpha1.DiscoveryInfo) *configv1alpha1apply.DiscoveryInfoApplyConfiguration {
	if di == nil {
		return nil
	}
	a := configv1alpha1apply.DiscoveryInfo()
	if di.Protocol != "" {
		a.WithProtocol(di.Protocol)
	}
	if di.Provider != "" {
		a.WithProvider(di.Provider)
	}
	if di.Version != "" {
		a.WithVersion(di.Version)
	}
	if di.Hostname != "" {
		a.WithHostname(di.Hostname)
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

func specToApply(spec *configv1alpha1.TargetSpec) *configv1alpha1apply.TargetSpecApplyConfiguration {
	a := configv1alpha1apply.TargetSpec().
		WithProvider(spec.Provider).
		WithAddress(spec.Address).
		WithCredentials(spec.Credentials).
		WithConnectionProfile(spec.ConnectionProfile)
	if spec.SyncProfile != nil {
		a.WithSyncProfile(*spec.SyncProfile)
	}
	if spec.TLSSecret != nil {
		a.WithTLSSecret(*spec.TLSSecret)
	}
	return a
}

func ownerRefsToApply(refs []metav1.OwnerReference) []*v1.OwnerReferenceApplyConfiguration {
	result := make([]*v1.OwnerReferenceApplyConfiguration, 0, len(refs))
	for _, ref := range refs {
		r := v1.OwnerReference().
			WithAPIVersion(ref.APIVersion).
			WithKind(ref.Kind).
			WithName(ref.Name).
			WithUID(ref.UID)
		if ref.Controller != nil {
			r.WithController(*ref.Controller)
		}
		result = append(result, r)
	}
	return result
}
