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
	"errors"
	"fmt"

	invv1alpha1 "github.com/sdcio/config-server/apis/inv/v1alpha1"
	"github.com/sdcio/config-server/pkg/discovery/discoveryrule"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/types"
)

func (r *reconciler) getDRConfig(ctx context.Context, cr *invv1alpha1.DiscoveryRule) (*discoveryrule.DiscoveryRuleConfig, error) {
	var errm error

	var discProfile *discoveryrule.DiscoveryProfile
	if cr.Discovery() {
		var err error
		discProfile, err = r.getDiscoveryProfile(ctx, cr)
		if err != nil {
			errm = errors.Join(errm, err)
		}
	}

	targetConnProfiles, err := r.getTargetConnectionProfile(ctx, cr)
	if err != nil {
		errm = errors.Join(errm, err)
	}
	if errm != nil {
		return nil, errm
	}

	return &discoveryrule.DiscoveryRuleConfig{
		CR:                       cr,
		Discovery:                cr.Discovery(),
		DefaultSchema:            cr.Spec.DefaultSchema,
		DiscoveryProfile:         discProfile,
		TargetConnectionProfiles: targetConnProfiles,
		TargetTemplate:           cr.Spec.TargetTemplate.DeepCopy(),
	}, nil
}

func (r *reconciler) getDiscoveryProfile(ctx context.Context, cr *invv1alpha1.DiscoveryRule) (*discoveryrule.DiscoveryProfile, error) {
	if cr.Spec.DiscoveryProfile == nil {
		return nil, fmt.Errorf("no discovery profile provided")
	}

	var errm error
	secret, err := r.getSecret(ctx, types.NamespacedName{Namespace: cr.GetNamespace(), Name: cr.Spec.DiscoveryProfile.Credentials})
	if err != nil {
		errm = errors.Join(errm, err)
	}
	connProfiles := make([]*invv1alpha1.TargetConnectionProfile, 0, len(cr.Spec.DiscoveryProfile.ConnectionProfiles))
	for _, connProfile := range cr.Spec.DiscoveryProfile.ConnectionProfiles {
		connProfile, err := r.getConnProfile(ctx, types.NamespacedName{Namespace: cr.GetNamespace(), Name: connProfile})
		if err != nil {
			errm = errors.Join(errm, err)
			continue
		}
		connProfiles = append(connProfiles, connProfile)
	}
	if errm != nil {
		return nil, errm
	}
	return &discoveryrule.DiscoveryProfile{
		Secret:                cr.Spec.DiscoveryProfile.Credentials,
		SecretResourceVersion: secret.GetResourceVersion(),
		// TODO TLS secret
		Connectionprofiles: connProfiles,
	}, nil
}

func (r *reconciler) getTargetConnectionProfile(ctx context.Context, cr *invv1alpha1.DiscoveryRule) ([]discoveryrule.TargetConnectionProfile, error) {
	var errm error
	targetConnProfiles := make([]discoveryrule.TargetConnectionProfile, len(cr.Spec.TargetConnectionProfiles))
	for i, targetConnProfile := range cr.Spec.TargetConnectionProfiles {
		secret, err := r.getSecret(ctx, types.NamespacedName{Namespace: cr.GetNamespace(), Name: targetConnProfile.Credentials})
		if err != nil {
			errm = errors.Join(errm, err)
		}

		connProfile, err := r.getConnProfile(ctx, types.NamespacedName{Namespace: cr.GetNamespace(), Name: targetConnProfile.ConnectionProfile})
		if err != nil {
			errm = errors.Join(errm, err)
		}
		connProfile = connProfile.DeepCopy()
		var syncProfile *invv1alpha1.TargetSyncProfile
		if targetConnProfile.SyncProfile != nil {
			syncProfile, err = r.getSyncProfile(ctx, types.NamespacedName{Namespace: cr.GetNamespace(), Name: *targetConnProfile.SyncProfile})
			if err != nil {
				errm = errors.Join(errm, err)
			}
			syncProfile = syncProfile.DeepCopy()
		}

		if errm != nil {
			continue
		}
		targetConnProfiles[i] = discoveryrule.TargetConnectionProfile{
			Secret:                targetConnProfile.Credentials,
			SecretResourceVersion: secret.GetResourceVersion(),
			// TODO TLS secret
			Connectionprofile: connProfile,
			Syncprofile:       syncProfile,
		}
	}
	if errm != nil {
		return nil, errm
	}
	return targetConnProfiles, nil
}

func (r *reconciler) getSecret(ctx context.Context, key types.NamespacedName) (*corev1.Secret, error) {
	obj := &corev1.Secret{}
	if err := r.Get(ctx, key, obj); err != nil {
		return nil, err
	}
	return obj, nil
}

func (r *reconciler) getConnProfile(ctx context.Context, key types.NamespacedName) (*invv1alpha1.TargetConnectionProfile, error) {
	obj := &invv1alpha1.TargetConnectionProfile{}
	if err := r.Get(ctx, key, obj); err != nil {
		return nil, err
	}
	return obj, nil
}

func (r *reconciler) getSyncProfile(ctx context.Context, key types.NamespacedName) (*invv1alpha1.TargetSyncProfile, error) {
	obj := &invv1alpha1.TargetSyncProfile{}
	if err := r.Get(ctx, key, obj); err != nil {
		return nil, err
	}
	return obj, nil
}
