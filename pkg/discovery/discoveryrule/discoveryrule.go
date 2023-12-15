package discoveryrule

import (
	"context"
	"fmt"
	"strings"

	invv1alpha1 "github.com/iptecharch/config-server/apis/inv/v1alpha1"
	"github.com/iptecharch/config-server/pkg/reconcilers/resource"
	"github.com/openconfig/gnmic/pkg/target"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/utils/pointer"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/log"
)

// global var on which we store the supported discovery rules
// they get initialized
var DiscoveryRules = map[schema.GroupVersionKind]Initializer{}

type Initializer func(client client.Client) DiscoveryRule

func Register(gvk schema.GroupVersionKind, initFn Initializer) {
	DiscoveryRules[gvk] = initFn
}

type DiscoveryRule interface {
	Run(ctx context.Context, dr *invv1alpha1.DiscoveryRuleContext) error
	Stop(ctx context.Context)
	// GetDiscoveryRule gets the specific discovery Rule and return the resource version and error
	GetDiscoveryRule(ctx context.Context, key types.NamespacedName) (string, error)
}

func ApplyTarget(ctx context.Context,
	c client.Client,
	dr *invv1alpha1.DiscoveryRuleContext,
	di *invv1alpha1.DiscoveryInfo,
	t *target.Target,
	drLabels map[string]string,
	providerName string,
) error {
	log := log.FromContext(ctx)
	newTargetCR, err := newTargetCR(ctx, dr, di, t, drLabels, providerName)
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
		if err := c.Update(ctx, newTargetCR); err != nil {
			return err
		}
	} else {
		log.Info("discovery target apply, target exists -> no change")
	}
	newTargetCR.Status.SetConditions(invv1alpha1.Ready())
	newTargetCR.Status = curTargetCR.Status
	newTargetCR.Status.DiscoveryInfo = di // only updates last seen
	if err := c.Status().Update(ctx, newTargetCR); err != nil {
		return err
	}
	return nil
}

func newTargetCR(ctx context.Context,
	dr *invv1alpha1.DiscoveryRuleContext,
	di *invv1alpha1.DiscoveryInfo,
	t *target.Target,
	drLabels map[string]string,
	providerName string) (*invv1alpha1.Target, error) {

	namespace := dr.DiscoveryRule.GetNamespace()

	//targetName := fmt.Sprintf("%s.%s.%s", di.HostName, strings.Fields(di.SerialNumber)[0], di.MacAddress)
	targetName := di.HostName
	targetName = strings.ReplaceAll(targetName, ":", "-")
	targetName = strings.ToLower(targetName)

	targetSpec := invv1alpha1.TargetSpec{
		Provider:          providerName,
		Address:           t.Config.Address,
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
	log := log.FromContext(ctx)

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
		curTargetCR.Status.DiscoveryInfo.MacAddress != di.MacAddress ||
		curTargetCR.Status.DiscoveryInfo.Platform != di.Platform ||
		curTargetCR.Status.DiscoveryInfo.SerialNumber != di.SerialNumber ||
		curTargetCR.Status.DiscoveryInfo.Type != di.Type ||
		curTargetCR.Status.DiscoveryInfo.Vendor != di.Vendor ||
		curTargetCR.Status.DiscoveryInfo.Version != di.Version {
		return true
	}
	return false
}
