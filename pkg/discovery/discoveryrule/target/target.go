package target

/*
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

type DRClient struct {
	Client client.Client
	DR     *invv1alpha1.DiscoveryRule
}

func (r *DRClient) NewTargetCR(
	ctx context.Context,
	address string,
	profile *invv1alpha1.DiscoveryRuleSpecProfile,
	di *invv1alpha1.DiscoveryInfo,
	drLabels map[string]string,
	providerName string) (*invv1alpha1.Target, error) {

	//targetName := fmt.Sprintf("%s.%s.%s", di.HostName, strings.Fields(di.SerialNumber)[0], di.MacAddress)
	targetName := di.HostName
	targetName = strings.ReplaceAll(targetName, ":", "-")
	targetName = strings.ToLower(targetName)

	targetSpec := invv1alpha1.TargetSpec{
		Provider:          providerName,
		Address:           address,
		Secret:            r.DR.Spec.Secret,
		ConnectionProfile: profile.ConnectionProfile,
		SyncProfile:       profile.SyncProfile,
	}
	labels, err := r.DR.GetTargetLabels(&targetSpec)
	if err != nil {
		return nil, err
	}
	// merge discovery rule implementation labels
	for k, v := range drLabels {
		labels[k] = v
	}

	anno, err := r.DR.GetTargetAnnotations(&targetSpec)
	if err != nil {
		return nil, err
	}

	return &invv1alpha1.Target{
		ObjectMeta: metav1.ObjectMeta{
			Name:        targetName,
			Namespace:   r.DR.GetNamespace(),
			Labels:      labels,
			Annotations: anno,
			OwnerReferences: []metav1.OwnerReference{
				{
					APIVersion: r.DR.APIVersion,
					Kind:       r.DR.Kind,
					Name:       r.DR.Name,
					UID:        r.DR.UID,
					Controller: pointer.Bool(true),
				}},
		},
		Spec: targetSpec,
	}, nil
}

func (r *DRClient) ApplyTarget(
	ctx context.Context,
	newTargetCR *invv1alpha1.Target,
	di *invv1alpha1.DiscoveryInfo,
) error {
	log := log.FromContext(ctx).With("targetName", newTargetCR.Name, "address", newTargetCR.Spec.Address)

	// check if the target already exists
	curTargetCR := &invv1alpha1.Target{}
	if err := r.Client.Get(ctx, types.NamespacedName{
		Namespace: newTargetCR.Namespace,
		Name:      newTargetCR.Name,
	}, curTargetCR); err != nil {
		if resource.IgnoreNotFound(err) != nil {
			return err
		}
		log.Info("discovery target apply, target does not exist -> create")

		if err := r.Client.Create(ctx, newTargetCR); err != nil {
			return err
		}

		newTargetCR.Status.SetConditions(invv1alpha1.Ready())
		newTargetCR.Status.DiscoveryInfo = di
		if err := r.Client.Status().Update(ctx, newTargetCR); err != nil {
			return err
		}
		return nil
	}
	// target already exists -> validate changes to avoid triggering a reconcile loop
	if hasChanged(ctx, curTargetCR, newTargetCR, di) {
		log.Info("discovery target apply, target exists -> changed")
		curTargetCR.Spec = newTargetCR.Spec
		if err := r.Client.Update(ctx, curTargetCR); err != nil {
			return err
		}
	} else {
		log.Info("discovery target apply, target exists -> no change")
	}
	curTargetCR.Status.SetConditions(invv1alpha1.Ready())
	curTargetCR.Status.DiscoveryInfo = di // only updates last seen
	if err := r.Client.Status().Update(ctx, curTargetCR); err != nil {
		return err
	}
	return nil
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

func (r *DRClient) ListTargets(ctx context.Context) (map[types.NamespacedName]client.Object, error) {
	opts := []client.ListOption{
		client.MatchingLabels{invv1alpha1.LabelKeyDiscoveryRule: r.DR.GetName()},
		client.InNamespace(r.DR.GetNamespace()),
	}

	targets := &invv1alpha1.TargetList{}
	if err := r.Client.List(ctx, targets, opts...); err != nil {
		return nil, err
	}
	existingTargets := map[types.NamespacedName]client.Object{}
	for _, target := range targets.Items {
		target := target
		for _, ref := range target.GetOwnerReferences() {
			if ref.UID == r.DR.GetUID() {
				existingTargets[types.NamespacedName{
					Namespace: target.Namespace,
					Name:      target.Name}] = &target
			}
		}
	}
	return existingTargets, nil
}

func (r *DRClient) Delete(ctx context.Context, obj client.Object) error {
	if err := r.Client.Delete(ctx, obj); err != nil {
		return err
	}
	return nil
}
*/