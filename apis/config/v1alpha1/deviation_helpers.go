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

package v1alpha1

import (
	"github.com/sdcio/data-server/pkg/utils"
	sdcpb "github.com/sdcio/sdc-protos/sdcpb"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

func (r *Deviation) GetNamespacedName() types.NamespacedName {
	return types.NamespacedName{Namespace: r.Namespace, Name: r.Name}
}

func (r *Deviation) DeepObjectCopy() client.Object {
	return r.DeepCopy()
}

func (r Deviation) HasNotAppliedDeviation() bool {
	for _, dev := range r.Spec.Deviations {
		if dev.Reason == "NOT_APPLIED" {
			return true
		}
	}
	return false
}

func ConvertSdcpbDeviations2ConfigDeviations(devs []*sdcpb.WatchDeviationResponse) []ConfigDeviation {
	deviations := make([]ConfigDeviation, 0, len(devs))
	for _, dev := range devs {
		deviations = append(deviations, ConfigDeviation{
			Path:         utils.ToXPath(dev.GetPath(), false),
			DesiredValue: dev.GetExpectedValue().String(),
			CurrentValue: dev.GetCurrentValue().String(),
			Reason:       dev.GetReason().String(),
		})
	}
	return deviations
}

// BuildDeviation returns a reource from a client Object a Spec/Status
func BuildDeviation(meta metav1.ObjectMeta, spec *DeviationSpec, status *DeviationStatus) *Deviation {
	if spec == nil {
		spec = &DeviationSpec{}
	}
	if status == nil {
		status = &DeviationStatus{}
	}

	return &Deviation{
		TypeMeta: metav1.TypeMeta{
			APIVersion: SchemeGroupVersion.Identifier(),
			Kind:       DeviationKind,
		},
		ObjectMeta: meta,
		Spec:       *spec,
		Status:     *status,
	}
}
