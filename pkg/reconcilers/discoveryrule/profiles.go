package discoveryrule

import (
	"context"
	"errors"
	"fmt"

	invv1alpha1 "github.com/iptecharch/config-server/apis/inv/v1alpha1"
	"github.com/iptecharch/config-server/pkg/discovery/discoveryrule"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/types"
)

func (r *reconciler) getDRConfig(ctx context.Context, cr *invv1alpha1.DiscoveryRule) (*discoveryrule.DiscoveryRuleConfig, error) {
	var errm error

	var discProfile *discoveryrule.DiscoveryProfile
	if cr.GetDiscoveryParameters().Discover {
		var err error
		discProfile, err = r.getDiscoveryProfile(ctx, cr)
		if err != nil {
			errm = errors.Join(errm, err)
		}
	}

	connProfile, err := r.getConnectivityProfile(ctx, cr)
	if err != nil {
		errm = errors.Join(errm, err)
	}
	if errm != nil {
		return nil, errm
	}

	return &discoveryrule.DiscoveryRuleConfig{
		CR:                  cr,
		Discovery:           cr.GetDiscoveryParameters().Discover,
		Prefixes:            cr.Spec.Prefixes,
		DiscoveryProfile:    discProfile,
		ConnectivityProfile: connProfile,
		TargetTemplate:      cr.Spec.TargetTemplate.DeepCopy(),
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

func (r *reconciler) getConnectivityProfile(ctx context.Context, cr *invv1alpha1.DiscoveryRule) (*discoveryrule.ConnectivityProfile, error) {
	var errm error
	secret, err := r.getSecret(ctx, types.NamespacedName{Namespace: cr.GetNamespace(), Name: cr.Spec.TargetConnectionProfiles[0].Credentials})
	if err != nil {
		errm = errors.Join(errm, err)
	}

	connProfile, err := r.getConnProfile(ctx, types.NamespacedName{Namespace: cr.GetNamespace(), Name: cr.Spec.TargetConnectionProfiles[0].ConnectionProfile})
	if err != nil {
		errm = errors.Join(errm, err)
	}
	syncProfile, err := r.getSyncProfile(ctx, types.NamespacedName{Namespace: cr.GetNamespace(), Name: *cr.Spec.TargetConnectionProfiles[0].SyncProfile})
	if err != nil {
		errm = errors.Join(errm, err)
	}
	if errm != nil {
		return nil, errm
	}

	return &discoveryrule.ConnectivityProfile{
		Secret:                cr.Spec.TargetConnectionProfiles[0].Credentials,
		SecretResourceVersion: secret.GetResourceVersion(),
		// TODO TLS secret
		Connectionprofile: connProfile.DeepCopy(),
		Syncprofile:       syncProfile.DeepCopy(),
		DefaultSchema:     cr.Spec.TargetConnectionProfiles[0].DefaultSchema.DeepCopy(),
	}, nil
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
