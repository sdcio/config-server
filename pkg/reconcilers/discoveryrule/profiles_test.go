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
	"testing"

	"github.com/sdcio/config-server/apis/generated/clientset/versioned/scheme"
	invv1alpha1 "github.com/sdcio/config-server/apis/inv/v1alpha1"
	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	"k8s.io/utils/pointer"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
)

func createFakeClient(secret, targetConnProfile, targetSyncProfile string) client.Client {
	ctx := context.Background()
	runScheme := runtime.NewScheme()
	scheme.AddToScheme(runScheme)         // ignoring errors
	clientgoscheme.AddToScheme(runScheme) // ignoring errors
	invv1alpha1.AddToScheme(runScheme)    // ignoring errors
	client := fake.NewClientBuilder().WithScheme(runScheme).Build()
	client.Create(ctx, &v1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name: secret,
		},
	})
	client.Create(ctx, &invv1alpha1.TargetConnectionProfile{
		ObjectMeta: metav1.ObjectMeta{
			Name: targetConnProfile,
		},
	})
	client.Create(ctx, &invv1alpha1.TargetSyncProfile{
		ObjectMeta: metav1.ObjectMeta{
			Name: targetSyncProfile,
		},
	})
	return client
}

func TestGetDRConfig(t *testing.T) {
	secretName := "a"
	targetConnProfileName := "b"
	targetSyncProfileName := "c"

	cases := map[string]struct {
		dr          *invv1alpha1.DiscoveryRule
		expectedErr error
	}{
		"Static": {
			dr: &invv1alpha1.DiscoveryRule{
				Spec: invv1alpha1.DiscoveryRuleSpec{
					Addresses: []invv1alpha1.DiscoveryRuleAddress{
						{Address: "1.1.1.1", HostName: "dev1"},
						{Address: "1.1.1.2", HostName: "dev2"},
					},
					DiscoveryParameters: invv1alpha1.DiscoveryParameters{
						DefaultSchema: &invv1alpha1.SchemaKey{
							Provider: "x.y.z",
							Version:  "v1",
						},
						TargetConnectionProfiles: []invv1alpha1.TargetProfile{
							{
								Credentials:       secretName,
								ConnectionProfile: targetConnProfileName,
								SyncProfile:       pointer.String(targetSyncProfileName),
							},
						},
					},
				},
			},
			expectedErr: nil,
		},
		"Dynamic": {
			dr: &invv1alpha1.DiscoveryRule{
				Spec: invv1alpha1.DiscoveryRuleSpec{
					Prefixes: []invv1alpha1.DiscoveryRulePrefix{
						{Prefix: "1.1.1.0/24"},
					},
					DiscoveryParameters: invv1alpha1.DiscoveryParameters{
						DiscoveryProfile: &invv1alpha1.DiscoveryProfile{
							Credentials:        secretName,
							ConnectionProfiles: []string{targetConnProfileName},
						},
						TargetConnectionProfiles: []invv1alpha1.TargetProfile{
							{
								Credentials:       secretName,
								ConnectionProfile: targetConnProfileName,
								SyncProfile:       pointer.String(targetSyncProfileName),
							},
						},
					},
				},
			},
			expectedErr: nil,
		},
	}

	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			ctx := context.Background()

			r := &reconciler{
				Client: createFakeClient(secretName, targetConnProfileName, targetSyncProfileName),
			}
			_, err := r.getDRConfig(ctx, tc.dr)
			if err != nil {
				if tc.expectedErr == nil {
					t.Errorf("%s unexpected error\n%s", name, err.Error())
				}

			}
			if tc.expectedErr != nil {
				t.Errorf("%s expecting an error, got nil", name)
			}
		})
	}
}
