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

package lease

import (
	"context"
	"fmt"
	"math/rand"
	"time"

	"github.com/henderiw/logger/log"
	"github.com/sdcio/config-server/pkg/reconcilers/resource"
	coordinationv1 "k8s.io/api/coordination/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

const (
	defaultLeaseInterval = 1 * time.Second
	minInterval          = 1
	maxInterval          = 2
)

func GetRandaomRequeueTimout() time.Duration {
	// Seed the random number generator
	rand.Seed(time.Now().UnixNano())

	randomSeconds := minInterval + rand.Intn(maxInterval-minInterval+1)
	return time.Duration(randomSeconds) * time.Second
}

type Lease interface {
	AcquireLease(ctx context.Context, holderIdentity string) error
}

func New(c client.Client, obj client.Object) Lease {
	return &lease{
		Client: c,
		obj:    obj,
	}
}

type lease struct {
	client.Client

	obj client.Object
}

func (r *lease) getLease(holderIdentity string) *coordinationv1.Lease {
	now := metav1.NowMicro()
	return &coordinationv1.Lease{
		ObjectMeta: metav1.ObjectMeta{
			Name:      r.obj.GetName(),
			Namespace: r.obj.GetNamespace(),
			// by setting the owner reference the object will be deleted once the
			// parent object gets deleted (k8s garbage collection)
			OwnerReferences: []metav1.OwnerReference{
				{
					APIVersion: r.obj.GetObjectKind().GroupVersionKind().GroupVersion().String(),
					Kind:       r.obj.GetObjectKind().GroupVersionKind().Kind,
					Name:       r.obj.GetName(),
					UID:        r.obj.GetUID(),
					Controller: ptr.To[bool](true),
				},
			},
		},
		Spec: coordinationv1.LeaseSpec{
			HolderIdentity:       ptr.To[string](holderIdentity),
			LeaseDurationSeconds: ptr.To[int32](int32(defaultLeaseInterval / time.Second)),
			AcquireTime:          &now,
			RenewTime:            &now,
		},
	}
}

func (r *lease) AcquireLease(ctx context.Context, holderIdentity string) error {
	log := log.FromContext(ctx)
	log.Debug("attempting to acquire lease to update the resource", "lease", r.obj.GetName())
	interconnectLeaseNSN := types.NamespacedName{
		Name:      r.obj.GetName(),
		Namespace: r.obj.GetNamespace(),
	}

	lease := &coordinationv1.Lease{}
	if err := r.Get(ctx, interconnectLeaseNSN, lease); err != nil {
		if resource.IgnoreNotFound(err) != nil {
			return err
		}
		log.Debug("lease not found, creating it", "lease", r.obj.GetName())

		lease = r.getLease(holderIdentity)
		if err := r.Create(ctx, lease); err != nil {
			return err
		}
	}
	// get the lease again
	if err := r.Get(ctx, interconnectLeaseNSN, lease); err != nil {
		return err
	}

	if lease == nil || lease.Spec.HolderIdentity == nil {
		return fmt.Errorf("lease nil or holderidentity nil")
	}

	now := metav1.NowMicro()
	if *lease.Spec.HolderIdentity != holderIdentity {
		// lease is held by another identity
		log.Debug("lease held by another identity", "identity", *lease.Spec.HolderIdentity)
		if lease.Spec.RenewTime != nil {
			expectedRenewTime := lease.Spec.RenewTime.Add(time.Duration(*lease.Spec.LeaseDurationSeconds) * time.Second)
			if !expectedRenewTime.Before(now.Time) {
				log.Debug("cannot acquire lease, lease held by another identity", "identity", *lease.Spec.HolderIdentity)
				return fmt.Errorf("cannot acquire lease, lease held by another identity: %s", *lease.Spec.HolderIdentity)
			}
		}
	}

	// take over the lease or update the lease
	log.Debug("successfully acquired lease")
	lease.Spec = coordinationv1.LeaseSpec{
		HolderIdentity:       ptr.To[string](holderIdentity),
		LeaseDurationSeconds: ptr.To[int32](int32(defaultLeaseInterval / time.Second)),
		AcquireTime:          &now,
		RenewTime:            &now,
	}
	if err := r.Update(ctx, lease); err != nil {
		return err
	}
	return nil
}
