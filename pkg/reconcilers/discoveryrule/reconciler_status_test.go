package discoveryrule

import (
	"context"
	"testing"

	condv1alpha1 "github.com/sdcio/config-server/apis/condition/v1alpha1"
	invv1alpha1 "github.com/sdcio/config-server/apis/inv/v1alpha1"
	"github.com/stretchr/testify/assert"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/tools/events"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/controller-runtime/pkg/client/interceptor"
)

func newStatusTestReconciler(writes *int) *reconciler {
	c := fake.NewClientBuilder().WithInterceptorFuncs(interceptor.Funcs{
		SubResourceApply: func(ctx context.Context, cl client.Client, sub string, obj runtime.ApplyConfiguration, opts ...client.SubResourceApplyOption) error {
			*writes++
			return nil
		},
	}).Build()
	return &reconciler{client: c, recorder: events.NewFakeRecorder(10)}
}

// A failure that is already reported must not rewrite the status: the write
// itself re-triggers the reconcile, which would loop forever.
func TestHandleErrorSkipsWriteWhenConditionUnchanged(t *testing.T) {
	writes := 0
	r := newStatusTestReconciler(&writes)

	dr := &invv1alpha1.DiscoveryRule{ObjectMeta: metav1.ObjectMeta{Name: "dr", Namespace: "ns"}}
	assert.NoError(t, r.handleError(context.Background(), dr, "validation failed", nil))
	assert.Equal(t, 1, writes, "first failure is reported")

	dr.Status.SetConditions(condv1alpha1.Failed("validation failed"))
	assert.NoError(t, r.handleError(context.Background(), dr, "validation failed", nil))
	assert.Equal(t, 1, writes, "same failure must not write again")

	assert.NoError(t, r.handleError(context.Background(), dr, "another failure", nil))
	assert.Equal(t, 2, writes, "a different failure is reported")
}
