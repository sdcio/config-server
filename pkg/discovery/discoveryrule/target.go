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
	"strings"

	"github.com/henderiw/apiserver-store/pkg/storebackend"
	"github.com/henderiw/logger/log"
	invv1alpha1 "github.com/sdcio/config-server/apis/inv/v1alpha1"
	"github.com/sdcio/config-server/pkg/lease"
	"github.com/sdcio/config-server/pkg/reconcilers/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/utils/ptr"
)

func (r *dr) createTarget(ctx context.Context, provider, address string, di *invv1alpha1.DiscoveryInfo) error {
	log := log.FromContext(ctx)
	r.children.Create(ctx, storebackend.ToKey(getTargetName(di.HostName)), "") // this should be done here

	l := lease.New(r.client, types.NamespacedName{
		Namespace: r.cfg.CR.GetNamespace(),
		Name:      getTargetName(di.HostName),
	})
	if err := l.AcquireLease(ctx, "DiscoveryController"); err != nil {
		log.Debug("cannot acquire lease", "target", getTargetName(di.HostName), "error", err.Error())
		return err
	}

	newTargetCR, err := r.newTargetCR(
		ctx,
		provider,
		address,
		di,
	)
	if err != nil {
		return err
	}

	if err := r.applyTarget(ctx, newTargetCR); err != nil {
		// TODO reapply if update failed
		if strings.Contains(err.Error(), "the object has been modified; please apply your changes to the latest version") {
			// we will rety once, sometimes we get an error
			if err := r.applyTarget(ctx, newTargetCR); err != nil {
				log.Info("dynamic target creation retry failed", "error", err)
			}
		} else {
			log.Info("dynamic target creation failed", "error", err)
		}
	}
	if err := r.applyUnManagedConfigCR(ctx, newTargetCR.Name); err != nil {
		return err
	}
	return nil
}

func (r *dr) newTargetCR(_ context.Context, providerName, address string, di *invv1alpha1.DiscoveryInfo) (*invv1alpha1.Target, error) {
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

	return &invv1alpha1.Target{
		ObjectMeta: metav1.ObjectMeta{
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
		Spec: targetSpec,
		Status: invv1alpha1.TargetStatus{
			DiscoveryInfo: di,
		},
	}, nil
}

// w/o seperated discovery info

func (r *dr) applyTarget(ctx context.Context, newTargetCR *invv1alpha1.Target) error {
	di := newTargetCR.Status.DiscoveryInfo.DeepCopy()
	//urefs := newTargetCR.Status.UsedReferences.DeepCopy()

	log := log.FromContext(ctx).With("targetName", newTargetCR.Name, "address", newTargetCR.Spec.Address, "discovery info", di)

	// check if the target already exists
	curTargetCR := &invv1alpha1.Target{}
	if err := r.client.Get(ctx, types.NamespacedName{
		Namespace: newTargetCR.Namespace,
		Name:      newTargetCR.Name,
	}, curTargetCR); err != nil {
		if resource.IgnoreNotFound(err) != nil {
			return err
		}
		log.Info("discovery target apply, target does not exist -> create")

		if err := r.client.Create(ctx, newTargetCR); err != nil {
			return err
		}

		newTargetCR.Status.SetConditions(invv1alpha1.DiscoveryReady())
		newTargetCR.Status.DiscoveryInfo = di
		if err := r.client.Status().Update(ctx, newTargetCR); err != nil {
			return err
		}
		return nil
	}
	curTargetCR = curTargetCR.DeepCopy()
	newTargetCR = newTargetCR.DeepCopy()
	// target already exists -> validate changes to avoid triggering a reconcile loop
	if hasChanged(ctx, curTargetCR, newTargetCR) {
		log.Info("discovery target apply, target exists -> changed")
		curTargetCR.Spec = newTargetCR.Spec
		if err := r.client.Update(ctx, curTargetCR); err != nil {
			return err
		}
	} else {
		log.Info("discovery target apply, target exists -> no change")
	}
	curTargetCR.Status.SetConditions(invv1alpha1.DiscoveryReady())
	curTargetCR.SetOverallStatus()
	curTargetCR.Status.DiscoveryInfo = di
	log.Info("discovery target apply",
		"Ready", curTargetCR.GetCondition(invv1alpha1.ConditionTypeReady).Status,
		"DSReady", curTargetCR.GetCondition(invv1alpha1.ConditionTypeDatastoreReady).Status,
		"ConfigReady", curTargetCR.GetCondition(invv1alpha1.ConditionTypeConfigReady).Status)
	if err := r.client.Status().Update(ctx, curTargetCR); err != nil {
		return err
	}
	return nil
}

func hasChanged(ctx context.Context, curTargetCR, newTargetCR *invv1alpha1.Target) bool {
	log := log.FromContext(ctx).With("target", newTargetCR.GetName(), "address", newTargetCR.Spec.Address)

	log.Info("validateDataStoreChanges", "current target status", curTargetCR.Status.GetCondition(invv1alpha1.ConditionTypeReady).Status)
	if curTargetCR.Status.GetCondition(invv1alpha1.ConditionTypeReady).Status == metav1.ConditionFalse {
		return true
	}

	if curTargetCR.Spec.SyncProfile != nil && newTargetCR.Spec.SyncProfile != nil {
		log.Info("validateDataStoreChanges",
			"Provider", fmt.Sprintf("%s/%s", curTargetCR.Spec.Provider, newTargetCR.Spec.Provider),
			"Address", fmt.Sprintf("%s/%s", curTargetCR.Spec.Address, newTargetCR.Spec.Address),
			"connectionProfile", fmt.Sprintf("%s/%s", curTargetCR.Spec.ConnectionProfile, newTargetCR.Spec.ConnectionProfile),
			"SyncProfile", fmt.Sprintf("%s/%s", *curTargetCR.Spec.SyncProfile, *newTargetCR.Spec.SyncProfile),
			"Secret", fmt.Sprintf("%s/%s", curTargetCR.Spec.Credentials, newTargetCR.Spec.Credentials),
			//"TLSSecret", fmt.Sprintf("%s/%s", *curTargetCR.Spec.TLSSecret, *newTargetCR.Spec.TLSSecret),
		)
		if curTargetCR.Spec.Address != newTargetCR.Spec.Address ||
			curTargetCR.Spec.Provider != newTargetCR.Spec.Provider ||
			curTargetCR.Spec.ConnectionProfile != newTargetCR.Spec.ConnectionProfile ||
			*curTargetCR.Spec.SyncProfile != *newTargetCR.Spec.SyncProfile ||
			curTargetCR.Spec.Credentials != newTargetCR.Spec.Credentials { // TODO TLS Secret
			return true
		}
	} else {
		log.Info("validateDataStoreChanges",
			"Provider", fmt.Sprintf("%s/%s", curTargetCR.Spec.Provider, newTargetCR.Spec.Provider),
			"Address", fmt.Sprintf("%s/%s", curTargetCR.Spec.Address, newTargetCR.Spec.Address),
			"connectionProfile", fmt.Sprintf("%s/%s", curTargetCR.Spec.ConnectionProfile, newTargetCR.Spec.ConnectionProfile),
			"Secret", fmt.Sprintf("%s/%s", curTargetCR.Spec.Credentials, newTargetCR.Spec.Credentials),
			//"TLSSecret", fmt.Sprintf("%s/%s", *curTargetCR.Spec.TLSSecret, *newTargetCR.Spec.TLSSecret),
		)

		if curTargetCR.Spec.Address != newTargetCR.Spec.Address ||
			curTargetCR.Spec.Provider != newTargetCR.Spec.Provider ||
			curTargetCR.Spec.ConnectionProfile != newTargetCR.Spec.ConnectionProfile ||
			curTargetCR.Spec.Credentials != newTargetCR.Spec.Credentials { // TODO TLS Secret
			return true
		}
	}

	if curTargetCR.Status.DiscoveryInfo == nil {
		log.Info("validateDataStoreChanges", "DiscoveryInfo", "nil")
		return true
	}

	log.Info("validateDataStoreChanges",
		"Protocol", fmt.Sprintf("%s/%s", curTargetCR.Status.DiscoveryInfo.Protocol, newTargetCR.Status.DiscoveryInfo.Protocol),
		"Provider", fmt.Sprintf("%s/%s", curTargetCR.Status.DiscoveryInfo.Provider, newTargetCR.Status.DiscoveryInfo.Provider),
		"Version", fmt.Sprintf("%s/%s", curTargetCR.Status.DiscoveryInfo.Version, newTargetCR.Status.DiscoveryInfo.Version),
		"HostName", fmt.Sprintf("%s/%s", curTargetCR.Status.DiscoveryInfo.HostName, newTargetCR.Status.DiscoveryInfo.HostName),
		"Platform", fmt.Sprintf("%s/%s", curTargetCR.Status.DiscoveryInfo.Platform, newTargetCR.Status.DiscoveryInfo.Platform),
		"MacAddress", fmt.Sprintf("%s/%s", curTargetCR.Status.DiscoveryInfo.MacAddress, newTargetCR.Status.DiscoveryInfo.MacAddress),
		"SerialNumber", fmt.Sprintf("%s/%s", curTargetCR.Status.DiscoveryInfo.SerialNumber, newTargetCR.Status.DiscoveryInfo.SerialNumber),
	)

	if curTargetCR.Status.DiscoveryInfo.Protocol != newTargetCR.Status.DiscoveryInfo.Protocol ||
		curTargetCR.Status.DiscoveryInfo.Provider != newTargetCR.Status.DiscoveryInfo.Provider ||
		curTargetCR.Status.DiscoveryInfo.Version != newTargetCR.Status.DiscoveryInfo.Version ||
		curTargetCR.Status.DiscoveryInfo.HostName != newTargetCR.Status.DiscoveryInfo.HostName ||
		curTargetCR.Status.DiscoveryInfo.Platform != newTargetCR.Status.DiscoveryInfo.Platform {
		return true
	}

	if newTargetCR.Status.DiscoveryInfo.SerialNumber != "" && (curTargetCR.Status.DiscoveryInfo.SerialNumber != newTargetCR.Status.DiscoveryInfo.SerialNumber) {
		return true
	}

	if newTargetCR.Status.DiscoveryInfo.MacAddress != "" && (curTargetCR.Status.DiscoveryInfo.MacAddress != newTargetCR.Status.DiscoveryInfo.MacAddress) {
		return true
	}

	if newTargetCR.Status.DiscoveryInfo.Platform != "" && (curTargetCR.Status.DiscoveryInfo.Platform != newTargetCR.Status.DiscoveryInfo.Platform) {
		return true
	}

	return false
}

func getTargetName(s string) string {
	targetName := strings.ReplaceAll(s, ":", "-")
	return strings.ToLower(targetName)
}
