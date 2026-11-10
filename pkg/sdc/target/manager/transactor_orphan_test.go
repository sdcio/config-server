/*
Copyright 2026 Nokia.

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

package targetmanager

import (
	"context"
	"testing"

	internalconfig "github.com/sdcio/config-server/apis/config"
	configv1alpha1 "github.com/sdcio/config-server/apis/config/v1alpha1"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
)

const (
	orphanTestNamespace = "default"
	orphanTestTarget    = "dev1"
)

func newTestConfig(name, targetName string, policy configv1alpha1.DeletionPolicy, deleting bool) *configv1alpha1.Config {
	cfg := &configv1alpha1.Config{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: orphanTestNamespace,
			Labels: map[string]string{
				internalconfig.TargetNamespaceKey: orphanTestNamespace,
				internalconfig.TargetNameKey:      targetName,
			},
			Finalizers: []string{finalizer},
		},
		Spec: configv1alpha1.ConfigSpec{
			Lifecycle: &configv1alpha1.Lifecycle{DeletionPolicy: policy},
		},
	}
	if deleting {
		now := metav1.Now()
		cfg.DeletionTimestamp = &now
	}
	return cfg
}

func newOrphanTestClient(t *testing.T, objs ...client.Object) client.Client {
	t.Helper()
	scheme := runtime.NewScheme()
	require.NoError(t, configv1alpha1.AddToScheme(scheme))
	return fake.NewClientBuilder().WithScheme(scheme).WithObjects(objs...).Build()
}

func getConfig(t *testing.T, c client.Client, name string) (*configv1alpha1.Config, bool) {
	t.Helper()
	cfg := &configv1alpha1.Config{}
	err := c.Get(context.Background(), types.NamespacedName{Namespace: orphanTestNamespace, Name: name}, cfg)
	if apierrors.IsNotFound(err) {
		return nil, false
	}
	require.NoError(t, err)
	return cfg, true
}

// A Config being deleted with the orphan policy is released when its Target
// does not exist; every other Config keeps its finalizer.
func TestReleaseOrphanConfigs(t *testing.T) {
	c := newOrphanTestClient(t,
		newTestConfig("orphan-deleting", orphanTestTarget, configv1alpha1.DeletionOrphan, true),
		newTestConfig("delete-deleting", orphanTestTarget, configv1alpha1.DeletionDelete, true),
		newTestConfig("orphan-active", orphanTestTarget, configv1alpha1.DeletionOrphan, false),
		newTestConfig("orphan-other-target", "dev2", configv1alpha1.DeletionOrphan, true),
	)
	tr := NewTransactor(c, "test")

	missing := &configv1alpha1.Target{ObjectMeta: metav1.ObjectMeta{
		Namespace: orphanTestNamespace,
		Name:      orphanTestTarget,
	}}
	require.NoError(t, tr.ReleaseOrphanConfigs(context.Background(), missing))

	_, exists := getConfig(t, c, "orphan-deleting")
	assert.False(t, exists, "orphan config of the missing target must be released")

	for _, name := range []string{"delete-deleting", "orphan-active", "orphan-other-target"} {
		cfg, exists := getConfig(t, c, name)
		require.True(t, exists, "config %s must be kept", name)
		assert.Equal(t, []string{finalizer}, cfg.GetFinalizers(), "config %s must keep its finalizer", name)
	}
}
