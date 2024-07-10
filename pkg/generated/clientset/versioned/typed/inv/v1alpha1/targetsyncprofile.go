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
// Code generated by client-gen. DO NOT EDIT.

package v1alpha1

import (
	"context"
	"time"

	v1alpha1 "github.com/sdcio/config-server/apis/inv/v1alpha1"
	scheme "github.com/sdcio/config-server/pkg/generated/clientset/versioned/scheme"
	v1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	types "k8s.io/apimachinery/pkg/types"
	watch "k8s.io/apimachinery/pkg/watch"
	rest "k8s.io/client-go/rest"
)

// TargetSyncProfilesGetter has a method to return a TargetSyncProfileInterface.
// A group's client should implement this interface.
type TargetSyncProfilesGetter interface {
	TargetSyncProfiles(namespace string) TargetSyncProfileInterface
}

// TargetSyncProfileInterface has methods to work with TargetSyncProfile resources.
type TargetSyncProfileInterface interface {
	Create(ctx context.Context, targetSyncProfile *v1alpha1.TargetSyncProfile, opts v1.CreateOptions) (*v1alpha1.TargetSyncProfile, error)
	Update(ctx context.Context, targetSyncProfile *v1alpha1.TargetSyncProfile, opts v1.UpdateOptions) (*v1alpha1.TargetSyncProfile, error)
	Delete(ctx context.Context, name string, opts v1.DeleteOptions) error
	DeleteCollection(ctx context.Context, opts v1.DeleteOptions, listOpts v1.ListOptions) error
	Get(ctx context.Context, name string, opts v1.GetOptions) (*v1alpha1.TargetSyncProfile, error)
	List(ctx context.Context, opts v1.ListOptions) (*v1alpha1.TargetSyncProfileList, error)
	Watch(ctx context.Context, opts v1.ListOptions) (watch.Interface, error)
	Patch(ctx context.Context, name string, pt types.PatchType, data []byte, opts v1.PatchOptions, subresources ...string) (result *v1alpha1.TargetSyncProfile, err error)
	TargetSyncProfileExpansion
}

// targetSyncProfiles implements TargetSyncProfileInterface
type targetSyncProfiles struct {
	client rest.Interface
	ns     string
}

// newTargetSyncProfiles returns a TargetSyncProfiles
func newTargetSyncProfiles(c *InvV1alpha1Client, namespace string) *targetSyncProfiles {
	return &targetSyncProfiles{
		client: c.RESTClient(),
		ns:     namespace,
	}
}

// Get takes name of the targetSyncProfile, and returns the corresponding targetSyncProfile object, and an error if there is any.
func (c *targetSyncProfiles) Get(ctx context.Context, name string, options v1.GetOptions) (result *v1alpha1.TargetSyncProfile, err error) {
	result = &v1alpha1.TargetSyncProfile{}
	err = c.client.Get().
		Namespace(c.ns).
		Resource("targetsyncprofiles").
		Name(name).
		VersionedParams(&options, scheme.ParameterCodec).
		Do(ctx).
		Into(result)
	return
}

// List takes label and field selectors, and returns the list of TargetSyncProfiles that match those selectors.
func (c *targetSyncProfiles) List(ctx context.Context, opts v1.ListOptions) (result *v1alpha1.TargetSyncProfileList, err error) {
	var timeout time.Duration
	if opts.TimeoutSeconds != nil {
		timeout = time.Duration(*opts.TimeoutSeconds) * time.Second
	}
	result = &v1alpha1.TargetSyncProfileList{}
	err = c.client.Get().
		Namespace(c.ns).
		Resource("targetsyncprofiles").
		VersionedParams(&opts, scheme.ParameterCodec).
		Timeout(timeout).
		Do(ctx).
		Into(result)
	return
}

// Watch returns a watch.Interface that watches the requested targetSyncProfiles.
func (c *targetSyncProfiles) Watch(ctx context.Context, opts v1.ListOptions) (watch.Interface, error) {
	var timeout time.Duration
	if opts.TimeoutSeconds != nil {
		timeout = time.Duration(*opts.TimeoutSeconds) * time.Second
	}
	opts.Watch = true
	return c.client.Get().
		Namespace(c.ns).
		Resource("targetsyncprofiles").
		VersionedParams(&opts, scheme.ParameterCodec).
		Timeout(timeout).
		Watch(ctx)
}

// Create takes the representation of a targetSyncProfile and creates it.  Returns the server's representation of the targetSyncProfile, and an error, if there is any.
func (c *targetSyncProfiles) Create(ctx context.Context, targetSyncProfile *v1alpha1.TargetSyncProfile, opts v1.CreateOptions) (result *v1alpha1.TargetSyncProfile, err error) {
	result = &v1alpha1.TargetSyncProfile{}
	err = c.client.Post().
		Namespace(c.ns).
		Resource("targetsyncprofiles").
		VersionedParams(&opts, scheme.ParameterCodec).
		Body(targetSyncProfile).
		Do(ctx).
		Into(result)
	return
}

// Update takes the representation of a targetSyncProfile and updates it. Returns the server's representation of the targetSyncProfile, and an error, if there is any.
func (c *targetSyncProfiles) Update(ctx context.Context, targetSyncProfile *v1alpha1.TargetSyncProfile, opts v1.UpdateOptions) (result *v1alpha1.TargetSyncProfile, err error) {
	result = &v1alpha1.TargetSyncProfile{}
	err = c.client.Put().
		Namespace(c.ns).
		Resource("targetsyncprofiles").
		Name(targetSyncProfile.Name).
		VersionedParams(&opts, scheme.ParameterCodec).
		Body(targetSyncProfile).
		Do(ctx).
		Into(result)
	return
}

// Delete takes name of the targetSyncProfile and deletes it. Returns an error if one occurs.
func (c *targetSyncProfiles) Delete(ctx context.Context, name string, opts v1.DeleteOptions) error {
	return c.client.Delete().
		Namespace(c.ns).
		Resource("targetsyncprofiles").
		Name(name).
		Body(&opts).
		Do(ctx).
		Error()
}

// DeleteCollection deletes a collection of objects.
func (c *targetSyncProfiles) DeleteCollection(ctx context.Context, opts v1.DeleteOptions, listOpts v1.ListOptions) error {
	var timeout time.Duration
	if listOpts.TimeoutSeconds != nil {
		timeout = time.Duration(*listOpts.TimeoutSeconds) * time.Second
	}
	return c.client.Delete().
		Namespace(c.ns).
		Resource("targetsyncprofiles").
		VersionedParams(&listOpts, scheme.ParameterCodec).
		Timeout(timeout).
		Body(&opts).
		Do(ctx).
		Error()
}

// Patch applies the patch and returns the patched targetSyncProfile.
func (c *targetSyncProfiles) Patch(ctx context.Context, name string, pt types.PatchType, data []byte, opts v1.PatchOptions, subresources ...string) (result *v1alpha1.TargetSyncProfile, err error) {
	result = &v1alpha1.TargetSyncProfile{}
	err = c.client.Patch(pt).
		Namespace(c.ns).
		Resource("targetsyncprofiles").
		Name(name).
		SubResource(subresources...).
		VersionedParams(&opts, scheme.ParameterCodec).
		Body(data).
		Do(ctx).
		Into(result)
	return
}