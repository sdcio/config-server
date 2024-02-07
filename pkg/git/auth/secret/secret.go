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

package secret

import (
	"context"
	"fmt"

	"github.com/sdcio/config-server/pkg/git/auth"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

const (
	// Values for scret types supported by porch.
	BasicAuthType = corev1.SecretTypeBasicAuth
)

func NewCredentialResolver(client client.Reader, resolverChain []Resolver) auth.CredentialResolver {
	return &secretResolver{
		client:        client,
		resolverChain: resolverChain,
	}
}

type secretResolver struct {
	resolverChain []Resolver
	client        client.Reader
}

var _ auth.CredentialResolver = &secretResolver{}

func (r *secretResolver) ResolveCredential(ctx context.Context, nsn types.NamespacedName) (auth.Credential, error) {
	var secret corev1.Secret
	if err := r.client.Get(ctx, nsn, &secret); err != nil {
		return nil, fmt.Errorf("cannot resolve credentials in a secret %s: %w", nsn.String(), err)
	}
	for _, resolver := range r.resolverChain {
		cred, found, err := resolver.Resolve(ctx, secret)
		if err != nil {
			return nil, fmt.Errorf("error resolving credential: %w", err)
		}
		if found {
			return cred, nil
		}
	}
	return nil, &NoMatchingResolverError{
		Type: string(secret.Type),
	}
}

type NoMatchingResolverError struct {
	Type string
}

func (e *NoMatchingResolverError) Error() string {
	return fmt.Sprintf("no resolver for secret with type %s", e.Type)
}

func (e *NoMatchingResolverError) Is(err error) bool {
	nmre, ok := err.(*NoMatchingResolverError)
	if !ok {
		return false
	}
	return nmre.Type == e.Type
}
