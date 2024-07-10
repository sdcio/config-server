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

package config
/*
import (
	"github.com/henderiw/apiserver-store/pkg/generic/registry"
	configv1alpha1 "github.com/sdcio/config-server/apis/config/v1alpha1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
)

func NewTableConvertor(gr schema.GroupResource) registry.TableConvertor {
	return registry.TableConvertor{
		Resource: gr,
		Cells: func(obj runtime.Object) []interface{} {
			config, ok := obj.(*configv1alpha1.Config)
			if !ok {
				return nil
			}
			return []interface{}{
				config.Name,
				config.GetCondition(configv1alpha1.ConditionTypeReady).Status,
				config.GetCondition(configv1alpha1.ConditionTypeReady).Reason,
				config.GetTarget(),
				config.GetLastKnownGoodSchema().FileString(),
			}
		},
		Columns: []metav1.TableColumnDefinition{
			{Name: "Name", Type: "string"},
			{Name: "Ready", Type: "string"},
			{Name: "Reason", Type: "string"},
			{Name: "Target", Type: "string"},
			{Name: "Schema", Type: "string"},
		},
	}
}
*/