package v1alpha1

import (
	"testing"

	condv1alpha1 "github.com/sdcio/config-server/apis/condition/v1alpha1"
	"github.com/stretchr/testify/assert"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func TestGetOverallStatusReportsConfigNotRecovered(t *testing.T) {
	target := &Target{}
	target.Status.SetConditions(
		TargetDiscoveryReady(),
		TargetDatastoreReady(),
		TargetConnectionReady(),
		TargetConfigRecoveryFailed("config not recovered"),
	)

	cond := GetOverallStatus(target)
	assert.Equal(t, metav1.ConditionFalse, cond.Status)
	assert.Equal(t, "config not recovered", cond.Message)

	target.Status.SetConditions(TargetConfigRecoveryReady(""))
	assert.Equal(t, condv1alpha1.Ready().Status, GetOverallStatus(target).Status)
}
