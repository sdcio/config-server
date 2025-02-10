/*
Copyright 2025 Nokia.

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

package workspace

import (
	"context"

	"github.com/henderiw/logger/log"
	condv1alpha1 "github.com/sdcio/config-server/apis/condition/v1alpha1"
	invv1alpha1 "github.com/sdcio/config-server/apis/inv/v1alpha1"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/utils/ptr"
)

func (r *reconciler) getRolloutStatus(ctx context.Context, workspace *invv1alpha1.Workspace) (*invv1alpha1.Rollout, condv1alpha1.Condition) {
	log := log.FromContext(ctx)
	rollout := &invv1alpha1.Rollout{}
	if err := r.Client.Get(ctx, workspace.GetNamespacedName(), rollout); err != nil {
		if !k8serrors.IsNotFound(err) {
			log.Error(errGetCr, "error", err)
			return nil, RolloutGetFailed(err.Error())
		}
		return nil, RolloutDone_NoMatchRefNew()
	}
	readyCondition := rollout.GetCondition(condv1alpha1.ConditionTypeReady)
	if readyCondition.Status != metav1.ConditionTrue {
		if readyCondition.Reason == string(condv1alpha1.ConditionReasonFailed) {
			return rollout, RolloutFailed(readyCondition.Message)
		}
		return rollout, RollingOut()
	}
	if workspace.Spec.Ref != rollout.Spec.Ref {
		return rollout, RolloutDone_NoMatchRef()
	}
	return rollout, RolloutDone()
}

func (r *reconciler) applyRollout(ctx context.Context, workspace *invv1alpha1.Workspace, rollout *invv1alpha1.Rollout) error {
	//log := log.FromContext(ctx)

	if rollout == nil {
		rollout := invv1alpha1.BuildRollout(
			metav1.ObjectMeta{
				Name:      workspace.Name,
				Namespace: workspace.Namespace,
				OwnerReferences: []metav1.OwnerReference{
					{
						APIVersion: workspace.APIVersion,
						Kind:       workspace.Kind,
						Name:       workspace.Name,
						UID:        workspace.UID,
						Controller: ptr.To(true),
					},
				},
			},
			invv1alpha1.RolloutSpec{
				Repository:            workspace.Spec.Repository,
				Strategy:              invv1alpha1.RolloutStrategy_NetworkWideTransaction,
				SkipUnavailableTarget: ptr.To(true),
			},
		)
		if err := r.Client.Create(ctx, rollout); err != nil {
			return err
		}
		return nil

	}

	rollout.Spec.Repository = workspace.Spec.Repository
	rollout.Spec.Strategy = invv1alpha1.RolloutStrategy_NetworkWideTransaction

	if err := r.Client.Update(ctx, rollout); err != nil {
		return err
	}
	return nil
}

type RolloutStatus string

const (
	// TODO need to indicate success or failure
	ConditionType_RolloutGetFailed        condv1alpha1.ConditionType = "rolloutGetFailed"
	ConditionType_RollingOut              condv1alpha1.ConditionType = "rollingOut"
	ConditionType_RolloutFailed           condv1alpha1.ConditionType = "rolloutFailed"
	ConditionType_RolloutDone             condv1alpha1.ConditionType = "ready"
	ConditionType_RolloutDone_NoMatchRef  condv1alpha1.ConditionType = "done_nomatch_ref"
	ConditionType_RolloutDone_MatchRefNew condv1alpha1.ConditionType = "done_nomatch_ref_new"
)

func RolloutGetFailed(msg string) condv1alpha1.Condition {
	return condv1alpha1.Condition{Condition: metav1.Condition{
		Type:               string(ConditionType_RolloutGetFailed),
		Status:             metav1.ConditionFalse,
		LastTransitionTime: metav1.Now(),
		Reason:             string(condv1alpha1.ConditionReasonFailed),
		Message:            msg,
	}}
}

func RollingOut() condv1alpha1.Condition {
	return condv1alpha1.Condition{Condition: metav1.Condition{
		Type:               string(ConditionType_RollingOut),
		Status:             metav1.ConditionFalse,
		LastTransitionTime: metav1.Now(),
		Reason:             string(condv1alpha1.ConditionReasonRollout),
	}}
}

func RolloutFailed(msg string) condv1alpha1.Condition {
	return condv1alpha1.Condition{Condition: metav1.Condition{
		Type:               string(ConditionType_RolloutFailed),
		Status:             metav1.ConditionFalse,
		LastTransitionTime: metav1.Now(),
		Reason:             string(condv1alpha1.ConditionReasonFailed),
		Message:            msg,
	}}
}

func RolloutDone_NoMatchRef() condv1alpha1.Condition {
	return condv1alpha1.Condition{Condition: metav1.Condition{
		Type:               string(ConditionType_RolloutDone_NoMatchRef),
		Status:             metav1.ConditionFalse,
		LastTransitionTime: metav1.Now(),
		Reason:             string(condv1alpha1.ConditionReasonRollout),
	}}
}

func RolloutDone_NoMatchRefNew() condv1alpha1.Condition {
	return condv1alpha1.Condition{Condition: metav1.Condition{
		Type:               string(ConditionType_RolloutDone_MatchRefNew),
		Status:             metav1.ConditionFalse,
		LastTransitionTime: metav1.Now(),
		Reason:             string(condv1alpha1.ConditionReasonRollout),
	}}
}

func RolloutDone() condv1alpha1.Condition {
	return condv1alpha1.Condition{Condition: metav1.Condition{
		Type:               string(ConditionType_RolloutDone),
		Status:             metav1.ConditionTrue,
		LastTransitionTime: metav1.Now(),
		Reason:             string(condv1alpha1.ConditionReasonReady),
	}}
}
