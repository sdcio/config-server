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

	configv1alpha1 "github.com/sdcio/config-server/apis/config/v1alpha1"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
)

// deleteDeviation deletes the Deviation of the Config by its canonical
// name, and leaves a Target Deviation that has the Config name alone.
func TestDeleteDeviation(t *testing.T) {
	const namespace = "default"
	// a Config named like the Deviation of Target "dev1"
	cfg := &configv1alpha1.Config{ObjectMeta: metav1.ObjectMeta{Name: "target-dev1", Namespace: namespace}}
	configDeviation := configv1alpha1.DeviationName(configv1alpha1.DeviationType_CONFIG, cfg.Name)
	targetDeviation := configv1alpha1.DeviationName(configv1alpha1.DeviationType_TARGET, "dev1")

	scheme := runtime.NewScheme()
	require.NoError(t, configv1alpha1.AddToScheme(scheme))
	c := fake.NewClientBuilder().WithScheme(scheme).WithObjects(
		&configv1alpha1.Deviation{ObjectMeta: metav1.ObjectMeta{Name: configDeviation, Namespace: namespace}},
		&configv1alpha1.Deviation{ObjectMeta: metav1.ObjectMeta{Name: targetDeviation, Namespace: namespace}},
	).Build()
	tr := NewTransactor(c, "test")

	require.NoError(t, tr.deleteDeviation(context.Background(), cfg))

	err := c.Get(context.Background(), types.NamespacedName{Namespace: namespace, Name: configDeviation}, &configv1alpha1.Deviation{})
	assert.True(t, apierrors.IsNotFound(err), "config deviation %s must be deleted, got err: %v", configDeviation, err)
	assert.NoError(t, c.Get(context.Background(), types.NamespacedName{Namespace: namespace, Name: targetDeviation}, &configv1alpha1.Deviation{}),
		"target deviation %s must be kept", targetDeviation)

	// the Deviation is gone: deleting it again is not an error
	assert.NoError(t, tr.deleteDeviation(context.Background(), cfg))
}
