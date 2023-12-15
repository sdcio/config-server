package target

import (
	"context"
	"fmt"
	"strings"

	"github.com/henderiw/logger/log"
	invv1alpha1 "github.com/iptecharch/config-server/apis/inv/v1alpha1"
	"github.com/iptecharch/config-server/pkg/reconcilers/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/utils/pointer"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

func ApplyTarget(ctx context.Context,
	c client.Client,
	dr *invv1alpha1.DiscoveryRuleContext,
	di *invv1alpha1.DiscoveryInfo,
	address string,
	drLabels map[string]string,
	providerName string,
) error {
	log := log.FromContext(ctx).With("target", di.HostName, "address", address)
	newTargetCR, err := newTargetCR(ctx, dr, di, address, drLabels, providerName)
	if err != nil {
		return err
	}

	// check if the target already exists
	curTargetCR := &invv1alpha1.Target{}
	err = c.Get(ctx, types.NamespacedName{
		Namespace: newTargetCR.Namespace,
		Name:      newTargetCR.Name,
	}, curTargetCR)
	if err != nil {
		if resource.IgnoreNotFound(err) != nil {
			return err
		}
		log.Info("discovery target apply, target does not exist -> create")

		if err := c.Create(ctx, newTargetCR); err != nil {
			return err
		}

		newTargetCR.Status.SetConditions(invv1alpha1.Ready())
		newTargetCR.Status.DiscoveryInfo = di
		if err := c.Status().Update(ctx, newTargetCR); err != nil {
			return err
		}
		return nil
	}
	// target already exists -> validate changes to avoid triggering a reconcile loop
	if hasChanged(ctx, curTargetCR, newTargetCR, di) {
		log.Info("discovery target apply, target exists -> changed")
		curTargetCR.Spec = newTargetCR.Spec
		if err := c.Update(ctx, curTargetCR); err != nil {
			return err
		}
	} else {
		log.Info("discovery target apply, target exists -> no change")
	}
	curTargetCR.Status.SetConditions(invv1alpha1.Ready())
	curTargetCR.Status.DiscoveryInfo = di // only updates last seen
	if err := c.Status().Update(ctx, curTargetCR); err != nil {
		return err
	}
	return nil
}

func newTargetCR(ctx context.Context,
	dr *invv1alpha1.DiscoveryRuleContext,
	di *invv1alpha1.DiscoveryInfo,
	address string,
	drLabels map[string]string,
	providerName string) (*invv1alpha1.Target, error) {

	namespace := dr.DiscoveryRule.GetNamespace()

	//targetName := fmt.Sprintf("%s.%s.%s", di.HostName, strings.Fields(di.SerialNumber)[0], di.MacAddress)
	targetName := di.HostName
	targetName = strings.ReplaceAll(targetName, ":", "-")
	targetName = strings.ToLower(targetName)

	targetSpec := invv1alpha1.TargetSpec{
		Provider:          providerName,
		Address:           address,
		Secret:            dr.DiscoveryRule.Spec.Secret,
		ConnectionProfile: dr.DiscoveryRule.Spec.ConnectionProfile,
		SyncProfile:       dr.DiscoveryRule.Spec.SyncProfile,
	}
	labels, err := dr.DiscoveryRule.GetTargetLabels(&targetSpec)
	if err != nil {
		return nil, err
	}
	// merge discovery rule implementation labels
	for k, v := range drLabels {
		labels[k] = v
	}

	anno, err := dr.DiscoveryRule.GetTargetAnnotations(&targetSpec)
	if err != nil {
		return nil, err
	}

	return &invv1alpha1.Target{
		ObjectMeta: metav1.ObjectMeta{
			Name:        targetName,
			Namespace:   namespace,
			Labels:      labels,
			Annotations: anno,
			OwnerReferences: []metav1.OwnerReference{
				{
					APIVersion: dr.DiscoveryRule.APIVersion,
					Kind:       dr.DiscoveryRule.Kind,
					Name:       dr.DiscoveryRule.Name,
					UID:        dr.DiscoveryRule.UID,
					Controller: pointer.Bool(true),
				}},
		},
		Spec: targetSpec,
	}, nil
}

func hasChanged(ctx context.Context, curTargetCR, newTargetCR *invv1alpha1.Target, di *invv1alpha1.DiscoveryInfo) bool {
	log := log.FromContext(ctx).With("target", di.HostName, "address", newTargetCR.Spec.Address)

	log.Info("validateDataStoreChanges", "current target status", curTargetCR.Status.GetCondition(invv1alpha1.ConditionTypeReady).Status)
	if curTargetCR.Status.GetCondition(invv1alpha1.ConditionTypeReady).Status == metav1.ConditionFalse {
		return true
	}

	log.Info("validateDataStoreChanges",
		"Address", fmt.Sprintf("%s/%s", curTargetCR.Spec.Address, newTargetCR.Spec.Address),
		"Provider", fmt.Sprintf("%s/%s", curTargetCR.Spec.Provider, newTargetCR.Spec.Provider),
		"connectionProfile", fmt.Sprintf("%s/%s", curTargetCR.Spec.ConnectionProfile, newTargetCR.Spec.ConnectionProfile),
		"SyncProfile", fmt.Sprintf("%s/%s", curTargetCR.Spec.SyncProfile, newTargetCR.Spec.SyncProfile),
	)

	if curTargetCR.Spec.Address != newTargetCR.Spec.Address ||
		curTargetCR.Spec.Provider != newTargetCR.Spec.Provider ||
		curTargetCR.Spec.ConnectionProfile != newTargetCR.Spec.ConnectionProfile ||
		curTargetCR.Spec.SyncProfile != newTargetCR.Spec.SyncProfile {
		return true
	}

	log.Info("validateDataStoreChanges", "DiscoveryInfo", "nil")
	if curTargetCR.Status.DiscoveryInfo == nil {
		return true
	}

	log.Info("validateDataStoreChanges",
		"HostName", fmt.Sprintf("%s/%s", curTargetCR.Status.DiscoveryInfo.HostName, di.HostName),
		"MacAddress", fmt.Sprintf("%s/%s", curTargetCR.Status.DiscoveryInfo.MacAddress, di.MacAddress),
		"Platform", fmt.Sprintf("%s/%s", curTargetCR.Status.DiscoveryInfo.Platform, di.Platform),
		"SerialNumber", fmt.Sprintf("%s/%s", curTargetCR.Status.DiscoveryInfo.SerialNumber, di.SerialNumber),
		"Type", fmt.Sprintf("%s/%s", curTargetCR.Status.DiscoveryInfo.Type, di.Type),
		"Vendor", fmt.Sprintf("%s/%s", curTargetCR.Status.DiscoveryInfo.Vendor, di.Vendor),
		"Version", fmt.Sprintf("%s/%s", curTargetCR.Status.DiscoveryInfo.Version, di.Version),
	)

	if curTargetCR.Status.DiscoveryInfo.HostName != di.HostName ||
		curTargetCR.Status.DiscoveryInfo.Platform != di.Platform ||
		curTargetCR.Status.DiscoveryInfo.Type != di.Type ||
		curTargetCR.Status.DiscoveryInfo.Vendor != di.Vendor ||
		curTargetCR.Status.DiscoveryInfo.Version != di.Version {
		return true
	}

	if di.SerialNumber != "" && (curTargetCR.Status.DiscoveryInfo.SerialNumber != di.SerialNumber) {
		return true
	}

	if di.MacAddress != "" && (curTargetCR.Status.DiscoveryInfo.MacAddress != di.MacAddress) {
		return true
	}

	if di.Platform != "" && (curTargetCR.Status.DiscoveryInfo.Platform != di.Platform) {
		return true
	}

	return false
}
