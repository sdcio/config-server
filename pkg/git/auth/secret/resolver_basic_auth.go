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

	"github.com/go-git/go-git/v5/plumbing/transport"
	"github.com/go-git/go-git/v5/plumbing/transport/http"
	"github.com/sdcio/config-server/pkg/git/auth"
	corev1 "k8s.io/api/core/v1"
)

func NewBasicAuthResolver() Resolver {
	return &BasicAuthResolver{}
}

var _ Resolver = &BasicAuthResolver{}

type BasicAuthResolver struct{}

func (b *BasicAuthResolver) Resolve(_ context.Context, secret corev1.Secret) (auth.Credential, bool, error) {
	if secret.Type != BasicAuthType {
		return nil, false, nil
	}

	return &BasicAuthCredential{
		Username: string(secret.Data["username"]),
		Password: string(secret.Data["password"]),
	}, true, nil
}

type BasicAuthCredential struct {
	Username string
	Password string
}

var _ auth.Credential = &BasicAuthCredential{}

func (b *BasicAuthCredential) Valid() bool {
	return true
}

func (b *BasicAuthCredential) ToAuthMethod() transport.AuthMethod {
	fmt.Println("auth: username/password", string(b.Username), string(b.Password))
	return &http.BasicAuth{
		Username: string(b.Username),
		Password: string(b.Password),
	}
}
