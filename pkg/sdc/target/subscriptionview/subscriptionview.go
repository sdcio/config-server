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

package subscriptionview


import (
  "context"

  invv1alpha1 "github.com/sdcio/config-server/apis/inv/v1alpha1"
)

// Only what the reconciler needs.
type TargetSubscriptionManager interface {
  ApplySubscription(ctx context.Context, sub *invv1alpha1.Subscription) error
  RemoveSubscription(ctx context.Context, sub *invv1alpha1.Subscription) error
}