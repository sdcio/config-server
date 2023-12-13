package discoveryrule

import (
	"context"
	"strings"

	invv1alpha1 "github.com/iptecharch/config-server/apis/inv/v1alpha1"
	"github.com/iptecharch/config-server/pkg/reconcilers/resource"
	"github.com/openconfig/gnmic/pkg/target"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/utils/pointer"
	"sigs.k8s.io/controller-runtime/pkg/client"
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

	// check if the target already exists
	targetCR := &invv1alpha1.Target{}
	err := c.Get(ctx, types.NamespacedName{
		Namespace: namespace,
		Name:      targetName,
	}, targetCR)
	if err != nil {
		if resource.IgnoreNotFound(err) != nil {
			return err
		}
		labels, err := dr.DiscoveryRule.GetTargetLabels(&targetSpec)
		if err != nil {
			return err
		}
		// merge discovery rule implementation labels
		for k, v := range drLabels {
			labels[k] = v
		}

		anno, err := dr.DiscoveryRule.GetTargetAnnotations(&targetSpec)
		if err != nil {
			return err
		}
		targetCR = &invv1alpha1.Target{
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
		}
		if err := c.Create(ctx, targetCR); err != nil {
			return err
		}

		targetCR.Status.SetConditions(invv1alpha1.Ready())
		targetCR.Status.DiscoveryInfo = di
		if err := c.Status().Update(ctx, targetCR); err != nil {
			return err
		}
		return nil
	}
	// target already exists
	//targetCR.Spec.DiscoveryInfo = di
	if err := c.Update(ctx, targetCR); err != nil {
		return err
	}
	targetCR.Status.SetConditions(invv1alpha1.Ready())
	targetCR.Status.DiscoveryInfo = di
	if err := c.Status().Update(ctx, targetCR); err != nil {
		return err
	}
	return nil
}
