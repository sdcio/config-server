//go:build !ignore_autogenerated
// +build !ignore_autogenerated

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

// Code generated by controller-gen. DO NOT EDIT.

package v1alpha1

import (
	"k8s.io/apimachinery/pkg/apis/meta/v1"
	runtime "k8s.io/apimachinery/pkg/runtime"
)

// DeepCopyInto is an autogenerated deepcopy function, copying the receiver, writing into out. in must be non-nil.
func (in *DiscoveryInfo) DeepCopyInto(out *DiscoveryInfo) {
	*out = *in
	if in.SupportedEncodings != nil {
		in, out := &in.SupportedEncodings, &out.SupportedEncodings
		*out = make([]string, len(*in))
		copy(*out, *in)
	}
}

// DeepCopy is an autogenerated deepcopy function, copying the receiver, creating a new DiscoveryInfo.
func (in *DiscoveryInfo) DeepCopy() *DiscoveryInfo {
	if in == nil {
		return nil
	}
	out := new(DiscoveryInfo)
	in.DeepCopyInto(out)
	return out
}

// DeepCopyInto is an autogenerated deepcopy function, copying the receiver, writing into out. in must be non-nil.
func (in *DiscoveryParameters) DeepCopyInto(out *DiscoveryParameters) {
	*out = *in
	if in.DefaultSchema != nil {
		in, out := &in.DefaultSchema, &out.DefaultSchema
		*out = new(SchemaKey)
		**out = **in
	}
	if in.DiscoveryProfile != nil {
		in, out := &in.DiscoveryProfile, &out.DiscoveryProfile
		*out = new(DiscoveryProfile)
		(*in).DeepCopyInto(*out)
	}
	if in.TargetConnectionProfiles != nil {
		in, out := &in.TargetConnectionProfiles, &out.TargetConnectionProfiles
		*out = make([]TargetProfile, len(*in))
		for i := range *in {
			(*in)[i].DeepCopyInto(&(*out)[i])
		}
	}
	if in.TargetTemplate != nil {
		in, out := &in.TargetTemplate, &out.TargetTemplate
		*out = new(TargetTemplate)
		(*in).DeepCopyInto(*out)
	}
	out.Period = in.Period
}

// DeepCopy is an autogenerated deepcopy function, copying the receiver, creating a new DiscoveryParameters.
func (in *DiscoveryParameters) DeepCopy() *DiscoveryParameters {
	if in == nil {
		return nil
	}
	out := new(DiscoveryParameters)
	in.DeepCopyInto(out)
	return out
}

// DeepCopyInto is an autogenerated deepcopy function, copying the receiver, writing into out. in must be non-nil.
func (in *DiscoveryPathDefinition) DeepCopyInto(out *DiscoveryPathDefinition) {
	*out = *in
	if in.Script != nil {
		in, out := &in.Script, &out.Script
		*out = new(string)
		**out = **in
	}
	if in.Regex != nil {
		in, out := &in.Regex, &out.Regex
		*out = new(string)
		**out = **in
	}
}

// DeepCopy is an autogenerated deepcopy function, copying the receiver, creating a new DiscoveryPathDefinition.
func (in *DiscoveryPathDefinition) DeepCopy() *DiscoveryPathDefinition {
	if in == nil {
		return nil
	}
	out := new(DiscoveryPathDefinition)
	in.DeepCopyInto(out)
	return out
}

// DeepCopyInto is an autogenerated deepcopy function, copying the receiver, writing into out. in must be non-nil.
func (in *DiscoveryProfile) DeepCopyInto(out *DiscoveryProfile) {
	*out = *in
	if in.TLSSecret != nil {
		in, out := &in.TLSSecret, &out.TLSSecret
		*out = new(string)
		**out = **in
	}
	if in.ConnectionProfiles != nil {
		in, out := &in.ConnectionProfiles, &out.ConnectionProfiles
		*out = make([]string, len(*in))
		copy(*out, *in)
	}
}

// DeepCopy is an autogenerated deepcopy function, copying the receiver, creating a new DiscoveryProfile.
func (in *DiscoveryProfile) DeepCopy() *DiscoveryProfile {
	if in == nil {
		return nil
	}
	out := new(DiscoveryProfile)
	in.DeepCopyInto(out)
	return out
}

// DeepCopyInto is an autogenerated deepcopy function, copying the receiver, writing into out. in must be non-nil.
func (in *DiscoveryRule) DeepCopyInto(out *DiscoveryRule) {
	*out = *in
	out.TypeMeta = in.TypeMeta
	in.ObjectMeta.DeepCopyInto(&out.ObjectMeta)
	in.Spec.DeepCopyInto(&out.Spec)
	in.Status.DeepCopyInto(&out.Status)
}

// DeepCopy is an autogenerated deepcopy function, copying the receiver, creating a new DiscoveryRule.
func (in *DiscoveryRule) DeepCopy() *DiscoveryRule {
	if in == nil {
		return nil
	}
	out := new(DiscoveryRule)
	in.DeepCopyInto(out)
	return out
}

// DeepCopyObject is an autogenerated deepcopy function, copying the receiver, creating a new runtime.Object.
func (in *DiscoveryRule) DeepCopyObject() runtime.Object {
	if c := in.DeepCopy(); c != nil {
		return c
	}
	return nil
}

// DeepCopyInto is an autogenerated deepcopy function, copying the receiver, writing into out. in must be non-nil.
func (in *DiscoveryRuleAddress) DeepCopyInto(out *DiscoveryRuleAddress) {
	*out = *in
}

// DeepCopy is an autogenerated deepcopy function, copying the receiver, creating a new DiscoveryRuleAddress.
func (in *DiscoveryRuleAddress) DeepCopy() *DiscoveryRuleAddress {
	if in == nil {
		return nil
	}
	out := new(DiscoveryRuleAddress)
	in.DeepCopyInto(out)
	return out
}

// DeepCopyInto is an autogenerated deepcopy function, copying the receiver, writing into out. in must be non-nil.
func (in *DiscoveryRuleList) DeepCopyInto(out *DiscoveryRuleList) {
	*out = *in
	out.TypeMeta = in.TypeMeta
	in.ListMeta.DeepCopyInto(&out.ListMeta)
	if in.Items != nil {
		in, out := &in.Items, &out.Items
		*out = make([]DiscoveryRule, len(*in))
		for i := range *in {
			(*in)[i].DeepCopyInto(&(*out)[i])
		}
	}
}

// DeepCopy is an autogenerated deepcopy function, copying the receiver, creating a new DiscoveryRuleList.
func (in *DiscoveryRuleList) DeepCopy() *DiscoveryRuleList {
	if in == nil {
		return nil
	}
	out := new(DiscoveryRuleList)
	in.DeepCopyInto(out)
	return out
}

// DeepCopyObject is an autogenerated deepcopy function, copying the receiver, creating a new runtime.Object.
func (in *DiscoveryRuleList) DeepCopyObject() runtime.Object {
	if c := in.DeepCopy(); c != nil {
		return c
	}
	return nil
}

// DeepCopyInto is an autogenerated deepcopy function, copying the receiver, writing into out. in must be non-nil.
func (in *DiscoveryRulePrefix) DeepCopyInto(out *DiscoveryRulePrefix) {
	*out = *in
	if in.Excludes != nil {
		in, out := &in.Excludes, &out.Excludes
		*out = make([]string, len(*in))
		copy(*out, *in)
	}
}

// DeepCopy is an autogenerated deepcopy function, copying the receiver, creating a new DiscoveryRulePrefix.
func (in *DiscoveryRulePrefix) DeepCopy() *DiscoveryRulePrefix {
	if in == nil {
		return nil
	}
	out := new(DiscoveryRulePrefix)
	in.DeepCopyInto(out)
	return out
}

// DeepCopyInto is an autogenerated deepcopy function, copying the receiver, writing into out. in must be non-nil.
func (in *DiscoveryRuleSpec) DeepCopyInto(out *DiscoveryRuleSpec) {
	*out = *in
	if in.Prefixes != nil {
		in, out := &in.Prefixes, &out.Prefixes
		*out = make([]DiscoveryRulePrefix, len(*in))
		for i := range *in {
			(*in)[i].DeepCopyInto(&(*out)[i])
		}
	}
	if in.Addresses != nil {
		in, out := &in.Addresses, &out.Addresses
		*out = make([]DiscoveryRuleAddress, len(*in))
		copy(*out, *in)
	}
	if in.PodSelector != nil {
		in, out := &in.PodSelector, &out.PodSelector
		*out = new(v1.LabelSelector)
		(*in).DeepCopyInto(*out)
	}
	if in.ServiceSelector != nil {
		in, out := &in.ServiceSelector, &out.ServiceSelector
		*out = new(v1.LabelSelector)
		(*in).DeepCopyInto(*out)
	}
	in.DiscoveryParameters.DeepCopyInto(&out.DiscoveryParameters)
}

// DeepCopy is an autogenerated deepcopy function, copying the receiver, creating a new DiscoveryRuleSpec.
func (in *DiscoveryRuleSpec) DeepCopy() *DiscoveryRuleSpec {
	if in == nil {
		return nil
	}
	out := new(DiscoveryRuleSpec)
	in.DeepCopyInto(out)
	return out
}

// DeepCopyInto is an autogenerated deepcopy function, copying the receiver, writing into out. in must be non-nil.
func (in *DiscoveryRuleStatus) DeepCopyInto(out *DiscoveryRuleStatus) {
	*out = *in
	in.ConditionedStatus.DeepCopyInto(&out.ConditionedStatus)
	in.StartTime.DeepCopyInto(&out.StartTime)
}

// DeepCopy is an autogenerated deepcopy function, copying the receiver, creating a new DiscoveryRuleStatus.
func (in *DiscoveryRuleStatus) DeepCopy() *DiscoveryRuleStatus {
	if in == nil {
		return nil
	}
	out := new(DiscoveryRuleStatus)
	in.DeepCopyInto(out)
	return out
}

// DeepCopyInto is an autogenerated deepcopy function, copying the receiver, writing into out. in must be non-nil.
func (in *DiscoveryVendorProfile) DeepCopyInto(out *DiscoveryVendorProfile) {
	*out = *in
	out.TypeMeta = in.TypeMeta
	in.ObjectMeta.DeepCopyInto(&out.ObjectMeta)
	in.Spec.DeepCopyInto(&out.Spec)
}

// DeepCopy is an autogenerated deepcopy function, copying the receiver, creating a new DiscoveryVendorProfile.
func (in *DiscoveryVendorProfile) DeepCopy() *DiscoveryVendorProfile {
	if in == nil {
		return nil
	}
	out := new(DiscoveryVendorProfile)
	in.DeepCopyInto(out)
	return out
}

// DeepCopyObject is an autogenerated deepcopy function, copying the receiver, creating a new runtime.Object.
func (in *DiscoveryVendorProfile) DeepCopyObject() runtime.Object {
	if c := in.DeepCopy(); c != nil {
		return c
	}
	return nil
}

// DeepCopyInto is an autogenerated deepcopy function, copying the receiver, writing into out. in must be non-nil.
func (in *DiscoveryVendorProfileList) DeepCopyInto(out *DiscoveryVendorProfileList) {
	*out = *in
	out.TypeMeta = in.TypeMeta
	in.ListMeta.DeepCopyInto(&out.ListMeta)
	if in.Items != nil {
		in, out := &in.Items, &out.Items
		*out = make([]DiscoveryVendorProfile, len(*in))
		for i := range *in {
			(*in)[i].DeepCopyInto(&(*out)[i])
		}
	}
}

// DeepCopy is an autogenerated deepcopy function, copying the receiver, creating a new DiscoveryVendorProfileList.
func (in *DiscoveryVendorProfileList) DeepCopy() *DiscoveryVendorProfileList {
	if in == nil {
		return nil
	}
	out := new(DiscoveryVendorProfileList)
	in.DeepCopyInto(out)
	return out
}

// DeepCopyObject is an autogenerated deepcopy function, copying the receiver, creating a new runtime.Object.
func (in *DiscoveryVendorProfileList) DeepCopyObject() runtime.Object {
	if c := in.DeepCopy(); c != nil {
		return c
	}
	return nil
}

// DeepCopyInto is an autogenerated deepcopy function, copying the receiver, writing into out. in must be non-nil.
func (in *DiscoveryVendorProfileSpec) DeepCopyInto(out *DiscoveryVendorProfileSpec) {
	*out = *in
	in.Gnmi.DeepCopyInto(&out.Gnmi)
}

// DeepCopy is an autogenerated deepcopy function, copying the receiver, creating a new DiscoveryVendorProfileSpec.
func (in *DiscoveryVendorProfileSpec) DeepCopy() *DiscoveryVendorProfileSpec {
	if in == nil {
		return nil
	}
	out := new(DiscoveryVendorProfileSpec)
	in.DeepCopyInto(out)
	return out
}

// DeepCopyInto is an autogenerated deepcopy function, copying the receiver, writing into out. in must be non-nil.
func (in *GnmiDiscoveryVendorProfileParameters) DeepCopyInto(out *GnmiDiscoveryVendorProfileParameters) {
	*out = *in
	if in.ModelMatch != nil {
		in, out := &in.ModelMatch, &out.ModelMatch
		*out = new(string)
		**out = **in
	}
	if in.Paths != nil {
		in, out := &in.Paths, &out.Paths
		*out = make([]DiscoveryPathDefinition, len(*in))
		for i := range *in {
			(*in)[i].DeepCopyInto(&(*out)[i])
		}
	}
	if in.Encoding != nil {
		in, out := &in.Encoding, &out.Encoding
		*out = new(Encoding)
		**out = **in
	}
}

// DeepCopy is an autogenerated deepcopy function, copying the receiver, creating a new GnmiDiscoveryVendorProfileParameters.
func (in *GnmiDiscoveryVendorProfileParameters) DeepCopy() *GnmiDiscoveryVendorProfileParameters {
	if in == nil {
		return nil
	}
	out := new(GnmiDiscoveryVendorProfileParameters)
	in.DeepCopyInto(out)
	return out
}

// DeepCopyInto is an autogenerated deepcopy function, copying the receiver, writing into out. in must be non-nil.
func (in *Schema) DeepCopyInto(out *Schema) {
	*out = *in
	out.TypeMeta = in.TypeMeta
	in.ObjectMeta.DeepCopyInto(&out.ObjectMeta)
	in.Spec.DeepCopyInto(&out.Spec)
	in.Status.DeepCopyInto(&out.Status)
}

// DeepCopy is an autogenerated deepcopy function, copying the receiver, creating a new Schema.
func (in *Schema) DeepCopy() *Schema {
	if in == nil {
		return nil
	}
	out := new(Schema)
	in.DeepCopyInto(out)
	return out
}

// DeepCopyObject is an autogenerated deepcopy function, copying the receiver, creating a new runtime.Object.
func (in *Schema) DeepCopyObject() runtime.Object {
	if c := in.DeepCopy(); c != nil {
		return c
	}
	return nil
}

// DeepCopyInto is an autogenerated deepcopy function, copying the receiver, writing into out. in must be non-nil.
func (in *SchemaKey) DeepCopyInto(out *SchemaKey) {
	*out = *in
}

// DeepCopy is an autogenerated deepcopy function, copying the receiver, creating a new SchemaKey.
func (in *SchemaKey) DeepCopy() *SchemaKey {
	if in == nil {
		return nil
	}
	out := new(SchemaKey)
	in.DeepCopyInto(out)
	return out
}

// DeepCopyInto is an autogenerated deepcopy function, copying the receiver, writing into out. in must be non-nil.
func (in *SchemaList) DeepCopyInto(out *SchemaList) {
	*out = *in
	out.TypeMeta = in.TypeMeta
	in.ListMeta.DeepCopyInto(&out.ListMeta)
	if in.Items != nil {
		in, out := &in.Items, &out.Items
		*out = make([]Schema, len(*in))
		for i := range *in {
			(*in)[i].DeepCopyInto(&(*out)[i])
		}
	}
}

// DeepCopy is an autogenerated deepcopy function, copying the receiver, creating a new SchemaList.
func (in *SchemaList) DeepCopy() *SchemaList {
	if in == nil {
		return nil
	}
	out := new(SchemaList)
	in.DeepCopyInto(out)
	return out
}

// DeepCopyObject is an autogenerated deepcopy function, copying the receiver, creating a new runtime.Object.
func (in *SchemaList) DeepCopyObject() runtime.Object {
	if c := in.DeepCopy(); c != nil {
		return c
	}
	return nil
}

// DeepCopyInto is an autogenerated deepcopy function, copying the receiver, writing into out. in must be non-nil.
func (in *SchemaSpec) DeepCopyInto(out *SchemaSpec) {
	*out = *in
	if in.Repositories != nil {
		in, out := &in.Repositories, &out.Repositories
		*out = make([]*SchemaSpecRepository, len(*in))
		for i := range *in {
			if (*in)[i] != nil {
				in, out := &(*in)[i], &(*out)[i]
				*out = new(SchemaSpecRepository)
				(*in).DeepCopyInto(*out)
			}
		}
	}
}

// DeepCopy is an autogenerated deepcopy function, copying the receiver, creating a new SchemaSpec.
func (in *SchemaSpec) DeepCopy() *SchemaSpec {
	if in == nil {
		return nil
	}
	out := new(SchemaSpec)
	in.DeepCopyInto(out)
	return out
}

// DeepCopyInto is an autogenerated deepcopy function, copying the receiver, writing into out. in must be non-nil.
func (in *SchemaSpecProxy) DeepCopyInto(out *SchemaSpecProxy) {
	*out = *in
}

// DeepCopy is an autogenerated deepcopy function, copying the receiver, creating a new SchemaSpecProxy.
func (in *SchemaSpecProxy) DeepCopy() *SchemaSpecProxy {
	if in == nil {
		return nil
	}
	out := new(SchemaSpecProxy)
	in.DeepCopyInto(out)
	return out
}

// DeepCopyInto is an autogenerated deepcopy function, copying the receiver, writing into out. in must be non-nil.
func (in *SchemaSpecRepository) DeepCopyInto(out *SchemaSpecRepository) {
	*out = *in
	out.Proxy = in.Proxy
	if in.Dirs != nil {
		in, out := &in.Dirs, &out.Dirs
		*out = make([]SrcDstPath, len(*in))
		copy(*out, *in)
	}
	in.Schema.DeepCopyInto(&out.Schema)
}

// DeepCopy is an autogenerated deepcopy function, copying the receiver, creating a new SchemaSpecRepository.
func (in *SchemaSpecRepository) DeepCopy() *SchemaSpecRepository {
	if in == nil {
		return nil
	}
	out := new(SchemaSpecRepository)
	in.DeepCopyInto(out)
	return out
}

// DeepCopyInto is an autogenerated deepcopy function, copying the receiver, writing into out. in must be non-nil.
func (in *SchemaSpecSchema) DeepCopyInto(out *SchemaSpecSchema) {
	*out = *in
	if in.Models != nil {
		in, out := &in.Models, &out.Models
		*out = make([]string, len(*in))
		copy(*out, *in)
	}
	if in.Includes != nil {
		in, out := &in.Includes, &out.Includes
		*out = make([]string, len(*in))
		copy(*out, *in)
	}
	if in.Excludes != nil {
		in, out := &in.Excludes, &out.Excludes
		*out = make([]string, len(*in))
		copy(*out, *in)
	}
}

// DeepCopy is an autogenerated deepcopy function, copying the receiver, creating a new SchemaSpecSchema.
func (in *SchemaSpecSchema) DeepCopy() *SchemaSpecSchema {
	if in == nil {
		return nil
	}
	out := new(SchemaSpecSchema)
	in.DeepCopyInto(out)
	return out
}

// DeepCopyInto is an autogenerated deepcopy function, copying the receiver, writing into out. in must be non-nil.
func (in *SchemaStatus) DeepCopyInto(out *SchemaStatus) {
	*out = *in
	in.ConditionedStatus.DeepCopyInto(&out.ConditionedStatus)
}

// DeepCopy is an autogenerated deepcopy function, copying the receiver, creating a new SchemaStatus.
func (in *SchemaStatus) DeepCopy() *SchemaStatus {
	if in == nil {
		return nil
	}
	out := new(SchemaStatus)
	in.DeepCopyInto(out)
	return out
}

// DeepCopyInto is an autogenerated deepcopy function, copying the receiver, writing into out. in must be non-nil.
func (in *SrcDstPath) DeepCopyInto(out *SrcDstPath) {
	*out = *in
}

// DeepCopy is an autogenerated deepcopy function, copying the receiver, creating a new SrcDstPath.
func (in *SrcDstPath) DeepCopy() *SrcDstPath {
	if in == nil {
		return nil
	}
	out := new(SrcDstPath)
	in.DeepCopyInto(out)
	return out
}

// DeepCopyInto is an autogenerated deepcopy function, copying the receiver, writing into out. in must be non-nil.
func (in *Subscription) DeepCopyInto(out *Subscription) {
	*out = *in
	out.TypeMeta = in.TypeMeta
	in.ObjectMeta.DeepCopyInto(&out.ObjectMeta)
	in.Spec.DeepCopyInto(&out.Spec)
	in.Status.DeepCopyInto(&out.Status)
}

// DeepCopy is an autogenerated deepcopy function, copying the receiver, creating a new Subscription.
func (in *Subscription) DeepCopy() *Subscription {
	if in == nil {
		return nil
	}
	out := new(Subscription)
	in.DeepCopyInto(out)
	return out
}

// DeepCopyObject is an autogenerated deepcopy function, copying the receiver, creating a new runtime.Object.
func (in *Subscription) DeepCopyObject() runtime.Object {
	if c := in.DeepCopy(); c != nil {
		return c
	}
	return nil
}

// DeepCopyInto is an autogenerated deepcopy function, copying the receiver, writing into out. in must be non-nil.
func (in *SubscriptionList) DeepCopyInto(out *SubscriptionList) {
	*out = *in
	out.TypeMeta = in.TypeMeta
	in.ListMeta.DeepCopyInto(&out.ListMeta)
	if in.Items != nil {
		in, out := &in.Items, &out.Items
		*out = make([]Subscription, len(*in))
		for i := range *in {
			(*in)[i].DeepCopyInto(&(*out)[i])
		}
	}
}

// DeepCopy is an autogenerated deepcopy function, copying the receiver, creating a new SubscriptionList.
func (in *SubscriptionList) DeepCopy() *SubscriptionList {
	if in == nil {
		return nil
	}
	out := new(SubscriptionList)
	in.DeepCopyInto(out)
	return out
}

// DeepCopyObject is an autogenerated deepcopy function, copying the receiver, creating a new runtime.Object.
func (in *SubscriptionList) DeepCopyObject() runtime.Object {
	if c := in.DeepCopy(); c != nil {
		return c
	}
	return nil
}

// DeepCopyInto is an autogenerated deepcopy function, copying the receiver, writing into out. in must be non-nil.
func (in *SubscriptionParameters) DeepCopyInto(out *SubscriptionParameters) {
	*out = *in
	if in.Description != nil {
		in, out := &in.Description, &out.Description
		*out = new(string)
		**out = **in
	}
	if in.Labels != nil {
		in, out := &in.Labels, &out.Labels
		*out = make(map[string]string, len(*in))
		for key, val := range *in {
			(*out)[key] = val
		}
	}
	if in.AdminState != nil {
		in, out := &in.AdminState, &out.AdminState
		*out = new(AdminState)
		**out = **in
	}
	if in.Interval != nil {
		in, out := &in.Interval, &out.Interval
		*out = new(v1.Duration)
		**out = **in
	}
	if in.Paths != nil {
		in, out := &in.Paths, &out.Paths
		*out = make([]string, len(*in))
		copy(*out, *in)
	}
}

// DeepCopy is an autogenerated deepcopy function, copying the receiver, creating a new SubscriptionParameters.
func (in *SubscriptionParameters) DeepCopy() *SubscriptionParameters {
	if in == nil {
		return nil
	}
	out := new(SubscriptionParameters)
	in.DeepCopyInto(out)
	return out
}

// DeepCopyInto is an autogenerated deepcopy function, copying the receiver, writing into out. in must be non-nil.
func (in *SubscriptionSpec) DeepCopyInto(out *SubscriptionSpec) {
	*out = *in
	in.Target.DeepCopyInto(&out.Target)
	if in.Encoding != nil {
		in, out := &in.Encoding, &out.Encoding
		*out = new(Encoding)
		**out = **in
	}
	if in.Subscriptions != nil {
		in, out := &in.Subscriptions, &out.Subscriptions
		*out = make([]SubscriptionParameters, len(*in))
		for i := range *in {
			(*in)[i].DeepCopyInto(&(*out)[i])
		}
	}
}

// DeepCopy is an autogenerated deepcopy function, copying the receiver, creating a new SubscriptionSpec.
func (in *SubscriptionSpec) DeepCopy() *SubscriptionSpec {
	if in == nil {
		return nil
	}
	out := new(SubscriptionSpec)
	in.DeepCopyInto(out)
	return out
}

// DeepCopyInto is an autogenerated deepcopy function, copying the receiver, writing into out. in must be non-nil.
func (in *SubscriptionStatus) DeepCopyInto(out *SubscriptionStatus) {
	*out = *in
	in.ConditionedStatus.DeepCopyInto(&out.ConditionedStatus)
	if in.Targets != nil {
		in, out := &in.Targets, &out.Targets
		*out = make([]string, len(*in))
		copy(*out, *in)
	}
}

// DeepCopy is an autogenerated deepcopy function, copying the receiver, creating a new SubscriptionStatus.
func (in *SubscriptionStatus) DeepCopy() *SubscriptionStatus {
	if in == nil {
		return nil
	}
	out := new(SubscriptionStatus)
	in.DeepCopyInto(out)
	return out
}

// DeepCopyInto is an autogenerated deepcopy function, copying the receiver, writing into out. in must be non-nil.
func (in *SubscriptionTarget) DeepCopyInto(out *SubscriptionTarget) {
	*out = *in
	if in.TargetSelector != nil {
		in, out := &in.TargetSelector, &out.TargetSelector
		*out = new(v1.LabelSelector)
		(*in).DeepCopyInto(*out)
	}
}

// DeepCopy is an autogenerated deepcopy function, copying the receiver, creating a new SubscriptionTarget.
func (in *SubscriptionTarget) DeepCopy() *SubscriptionTarget {
	if in == nil {
		return nil
	}
	out := new(SubscriptionTarget)
	in.DeepCopyInto(out)
	return out
}

// DeepCopyInto is an autogenerated deepcopy function, copying the receiver, writing into out. in must be non-nil.
func (in *Target) DeepCopyInto(out *Target) {
	*out = *in
	out.TypeMeta = in.TypeMeta
	in.ObjectMeta.DeepCopyInto(&out.ObjectMeta)
	in.Spec.DeepCopyInto(&out.Spec)
	in.Status.DeepCopyInto(&out.Status)
}

// DeepCopy is an autogenerated deepcopy function, copying the receiver, creating a new Target.
func (in *Target) DeepCopy() *Target {
	if in == nil {
		return nil
	}
	out := new(Target)
	in.DeepCopyInto(out)
	return out
}

// DeepCopyObject is an autogenerated deepcopy function, copying the receiver, creating a new runtime.Object.
func (in *Target) DeepCopyObject() runtime.Object {
	if c := in.DeepCopy(); c != nil {
		return c
	}
	return nil
}

// DeepCopyInto is an autogenerated deepcopy function, copying the receiver, writing into out. in must be non-nil.
func (in *TargetConnectionProfile) DeepCopyInto(out *TargetConnectionProfile) {
	*out = *in
	out.TypeMeta = in.TypeMeta
	in.ObjectMeta.DeepCopyInto(&out.ObjectMeta)
	in.Spec.DeepCopyInto(&out.Spec)
}

// DeepCopy is an autogenerated deepcopy function, copying the receiver, creating a new TargetConnectionProfile.
func (in *TargetConnectionProfile) DeepCopy() *TargetConnectionProfile {
	if in == nil {
		return nil
	}
	out := new(TargetConnectionProfile)
	in.DeepCopyInto(out)
	return out
}

// DeepCopyObject is an autogenerated deepcopy function, copying the receiver, creating a new runtime.Object.
func (in *TargetConnectionProfile) DeepCopyObject() runtime.Object {
	if c := in.DeepCopy(); c != nil {
		return c
	}
	return nil
}

// DeepCopyInto is an autogenerated deepcopy function, copying the receiver, writing into out. in must be non-nil.
func (in *TargetConnectionProfileList) DeepCopyInto(out *TargetConnectionProfileList) {
	*out = *in
	out.TypeMeta = in.TypeMeta
	in.ListMeta.DeepCopyInto(&out.ListMeta)
	if in.Items != nil {
		in, out := &in.Items, &out.Items
		*out = make([]TargetConnectionProfile, len(*in))
		for i := range *in {
			(*in)[i].DeepCopyInto(&(*out)[i])
		}
	}
}

// DeepCopy is an autogenerated deepcopy function, copying the receiver, creating a new TargetConnectionProfileList.
func (in *TargetConnectionProfileList) DeepCopy() *TargetConnectionProfileList {
	if in == nil {
		return nil
	}
	out := new(TargetConnectionProfileList)
	in.DeepCopyInto(out)
	return out
}

// DeepCopyObject is an autogenerated deepcopy function, copying the receiver, creating a new runtime.Object.
func (in *TargetConnectionProfileList) DeepCopyObject() runtime.Object {
	if c := in.DeepCopy(); c != nil {
		return c
	}
	return nil
}

// DeepCopyInto is an autogenerated deepcopy function, copying the receiver, writing into out. in must be non-nil.
func (in *TargetConnectionProfileSpec) DeepCopyInto(out *TargetConnectionProfileSpec) {
	*out = *in
	out.ConnectRetry = in.ConnectRetry
	out.Timeout = in.Timeout
	if in.Encoding != nil {
		in, out := &in.Encoding, &out.Encoding
		*out = new(Encoding)
		**out = **in
	}
	if in.PreferredNetconfVersion != nil {
		in, out := &in.PreferredNetconfVersion, &out.PreferredNetconfVersion
		*out = new(string)
		**out = **in
	}
	if in.Insecure != nil {
		in, out := &in.Insecure, &out.Insecure
		*out = new(bool)
		**out = **in
	}
	if in.SkipVerify != nil {
		in, out := &in.SkipVerify, &out.SkipVerify
		*out = new(bool)
		**out = **in
	}
	if in.IncludeNS != nil {
		in, out := &in.IncludeNS, &out.IncludeNS
		*out = new(bool)
		**out = **in
	}
	if in.OperationWithNS != nil {
		in, out := &in.OperationWithNS, &out.OperationWithNS
		*out = new(bool)
		**out = **in
	}
	if in.UseOperationRemove != nil {
		in, out := &in.UseOperationRemove, &out.UseOperationRemove
		*out = new(bool)
		**out = **in
	}
	if in.CommitCandidate != nil {
		in, out := &in.CommitCandidate, &out.CommitCandidate
		*out = new(CommitCandidate)
		**out = **in
	}
}

// DeepCopy is an autogenerated deepcopy function, copying the receiver, creating a new TargetConnectionProfileSpec.
func (in *TargetConnectionProfileSpec) DeepCopy() *TargetConnectionProfileSpec {
	if in == nil {
		return nil
	}
	out := new(TargetConnectionProfileSpec)
	in.DeepCopyInto(out)
	return out
}

// DeepCopyInto is an autogenerated deepcopy function, copying the receiver, writing into out. in must be non-nil.
func (in *TargetList) DeepCopyInto(out *TargetList) {
	*out = *in
	out.TypeMeta = in.TypeMeta
	in.ListMeta.DeepCopyInto(&out.ListMeta)
	if in.Items != nil {
		in, out := &in.Items, &out.Items
		*out = make([]Target, len(*in))
		for i := range *in {
			(*in)[i].DeepCopyInto(&(*out)[i])
		}
	}
}

// DeepCopy is an autogenerated deepcopy function, copying the receiver, creating a new TargetList.
func (in *TargetList) DeepCopy() *TargetList {
	if in == nil {
		return nil
	}
	out := new(TargetList)
	in.DeepCopyInto(out)
	return out
}

// DeepCopyObject is an autogenerated deepcopy function, copying the receiver, creating a new runtime.Object.
func (in *TargetList) DeepCopyObject() runtime.Object {
	if c := in.DeepCopy(); c != nil {
		return c
	}
	return nil
}

// DeepCopyInto is an autogenerated deepcopy function, copying the receiver, writing into out. in must be non-nil.
func (in *TargetProfile) DeepCopyInto(out *TargetProfile) {
	*out = *in
	if in.TLSSecret != nil {
		in, out := &in.TLSSecret, &out.TLSSecret
		*out = new(string)
		**out = **in
	}
	if in.SyncProfile != nil {
		in, out := &in.SyncProfile, &out.SyncProfile
		*out = new(string)
		**out = **in
	}
}

// DeepCopy is an autogenerated deepcopy function, copying the receiver, creating a new TargetProfile.
func (in *TargetProfile) DeepCopy() *TargetProfile {
	if in == nil {
		return nil
	}
	out := new(TargetProfile)
	in.DeepCopyInto(out)
	return out
}

// DeepCopyInto is an autogenerated deepcopy function, copying the receiver, writing into out. in must be non-nil.
func (in *TargetSpec) DeepCopyInto(out *TargetSpec) {
	*out = *in
	in.TargetProfile.DeepCopyInto(&out.TargetProfile)
}

// DeepCopy is an autogenerated deepcopy function, copying the receiver, creating a new TargetSpec.
func (in *TargetSpec) DeepCopy() *TargetSpec {
	if in == nil {
		return nil
	}
	out := new(TargetSpec)
	in.DeepCopyInto(out)
	return out
}

// DeepCopyInto is an autogenerated deepcopy function, copying the receiver, writing into out. in must be non-nil.
func (in *TargetStatus) DeepCopyInto(out *TargetStatus) {
	*out = *in
	in.ConditionedStatus.DeepCopyInto(&out.ConditionedStatus)
	if in.DiscoveryInfo != nil {
		in, out := &in.DiscoveryInfo, &out.DiscoveryInfo
		*out = new(DiscoveryInfo)
		(*in).DeepCopyInto(*out)
	}
	if in.UsedReferences != nil {
		in, out := &in.UsedReferences, &out.UsedReferences
		*out = new(TargetStatusUsedReferences)
		**out = **in
	}
}

// DeepCopy is an autogenerated deepcopy function, copying the receiver, creating a new TargetStatus.
func (in *TargetStatus) DeepCopy() *TargetStatus {
	if in == nil {
		return nil
	}
	out := new(TargetStatus)
	in.DeepCopyInto(out)
	return out
}

// DeepCopyInto is an autogenerated deepcopy function, copying the receiver, writing into out. in must be non-nil.
func (in *TargetStatusUsedReferences) DeepCopyInto(out *TargetStatusUsedReferences) {
	*out = *in
}

// DeepCopy is an autogenerated deepcopy function, copying the receiver, creating a new TargetStatusUsedReferences.
func (in *TargetStatusUsedReferences) DeepCopy() *TargetStatusUsedReferences {
	if in == nil {
		return nil
	}
	out := new(TargetStatusUsedReferences)
	in.DeepCopyInto(out)
	return out
}

// DeepCopyInto is an autogenerated deepcopy function, copying the receiver, writing into out. in must be non-nil.
func (in *TargetSyncProfile) DeepCopyInto(out *TargetSyncProfile) {
	*out = *in
	out.TypeMeta = in.TypeMeta
	in.ObjectMeta.DeepCopyInto(&out.ObjectMeta)
	in.Spec.DeepCopyInto(&out.Spec)
}

// DeepCopy is an autogenerated deepcopy function, copying the receiver, creating a new TargetSyncProfile.
func (in *TargetSyncProfile) DeepCopy() *TargetSyncProfile {
	if in == nil {
		return nil
	}
	out := new(TargetSyncProfile)
	in.DeepCopyInto(out)
	return out
}

// DeepCopyObject is an autogenerated deepcopy function, copying the receiver, creating a new runtime.Object.
func (in *TargetSyncProfile) DeepCopyObject() runtime.Object {
	if c := in.DeepCopy(); c != nil {
		return c
	}
	return nil
}

// DeepCopyInto is an autogenerated deepcopy function, copying the receiver, writing into out. in must be non-nil.
func (in *TargetSyncProfileList) DeepCopyInto(out *TargetSyncProfileList) {
	*out = *in
	out.TypeMeta = in.TypeMeta
	in.ListMeta.DeepCopyInto(&out.ListMeta)
	if in.Items != nil {
		in, out := &in.Items, &out.Items
		*out = make([]TargetSyncProfile, len(*in))
		for i := range *in {
			(*in)[i].DeepCopyInto(&(*out)[i])
		}
	}
}

// DeepCopy is an autogenerated deepcopy function, copying the receiver, creating a new TargetSyncProfileList.
func (in *TargetSyncProfileList) DeepCopy() *TargetSyncProfileList {
	if in == nil {
		return nil
	}
	out := new(TargetSyncProfileList)
	in.DeepCopyInto(out)
	return out
}

// DeepCopyObject is an autogenerated deepcopy function, copying the receiver, creating a new runtime.Object.
func (in *TargetSyncProfileList) DeepCopyObject() runtime.Object {
	if c := in.DeepCopy(); c != nil {
		return c
	}
	return nil
}

// DeepCopyInto is an autogenerated deepcopy function, copying the receiver, writing into out. in must be non-nil.
func (in *TargetSyncProfileSpec) DeepCopyInto(out *TargetSyncProfileSpec) {
	*out = *in
	if in.Sync != nil {
		in, out := &in.Sync, &out.Sync
		*out = make([]TargetSyncProfileSync, len(*in))
		for i := range *in {
			(*in)[i].DeepCopyInto(&(*out)[i])
		}
	}
}

// DeepCopy is an autogenerated deepcopy function, copying the receiver, creating a new TargetSyncProfileSpec.
func (in *TargetSyncProfileSpec) DeepCopy() *TargetSyncProfileSpec {
	if in == nil {
		return nil
	}
	out := new(TargetSyncProfileSpec)
	in.DeepCopyInto(out)
	return out
}

// DeepCopyInto is an autogenerated deepcopy function, copying the receiver, writing into out. in must be non-nil.
func (in *TargetSyncProfileSync) DeepCopyInto(out *TargetSyncProfileSync) {
	*out = *in
	if in.Paths != nil {
		in, out := &in.Paths, &out.Paths
		*out = make([]string, len(*in))
		copy(*out, *in)
	}
	if in.Encoding != nil {
		in, out := &in.Encoding, &out.Encoding
		*out = new(Encoding)
		**out = **in
	}
	out.Interval = in.Interval
}

// DeepCopy is an autogenerated deepcopy function, copying the receiver, creating a new TargetSyncProfileSync.
func (in *TargetSyncProfileSync) DeepCopy() *TargetSyncProfileSync {
	if in == nil {
		return nil
	}
	out := new(TargetSyncProfileSync)
	in.DeepCopyInto(out)
	return out
}

// DeepCopyInto is an autogenerated deepcopy function, copying the receiver, writing into out. in must be non-nil.
func (in *TargetTemplate) DeepCopyInto(out *TargetTemplate) {
	*out = *in
	if in.Annotations != nil {
		in, out := &in.Annotations, &out.Annotations
		*out = make(map[string]string, len(*in))
		for key, val := range *in {
			(*out)[key] = val
		}
	}
	if in.Labels != nil {
		in, out := &in.Labels, &out.Labels
		*out = make(map[string]string, len(*in))
		for key, val := range *in {
			(*out)[key] = val
		}
	}
}

// DeepCopy is an autogenerated deepcopy function, copying the receiver, creating a new TargetTemplate.
func (in *TargetTemplate) DeepCopy() *TargetTemplate {
	if in == nil {
		return nil
	}
	out := new(TargetTemplate)
	in.DeepCopyInto(out)
	return out
}
