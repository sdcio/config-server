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

type Repository struct {
	// RepositoryURL specifies the base URL for a given repository
	RepositoryURL string `json:"repoURL" protobuf:"bytes,1,opt,name=repoURL"`
	// Credentials defines the name of the secret that holds the credentials to connect to the repo
	Credentials string `json:"credentials,omitempty" protobuf:"bytes,2,opt,name=credentials"`
	// Proxy defines the HTTP/HTTPS proxy to be used to connect to the repo.
	Proxy *Proxy `json:"proxy,omitempty" protobuf:"bytes,3,opt,name=proxy"`
	// +kubebuilder:validation:Enum=branch;tag;hash;
	// +kubebuilder:default:=tag
	// Kind defines the that the BranchOrTag string is a repository branch or a tag
	Kind BranchTagKind `json:"kind" protobuf:"bytes,4,opt,name=kind,casttype=BranchTagKind"`
	// Ref defines the branch or tag of the repository corresponding to the
	// provider schema version
	Ref string `json:"ref" protobuf:"bytes,5,opt,name=ref"`
}

type Proxy struct {
	// URL specifies the base URL of the HTTP/HTTPS proxy server.
	URL string `json:"URL" protobuf:"bytes,1,opt,name=URL"`
	// Credentials defines the name of the secret that holds the credentials to connect to the proxy server
	Credentials string `json:"credentials" protobuf:"bytes,2,opt,name=credentials"`
}
