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

package git

import (
	"net/url"

	"github.com/go-git/go-git/v5/plumbing/transport"
)

// GitRepoStruct is a struct that contains all the fields
// required for a GitRepo instance.
type GitRepoStruct struct {
	// cloneURL is the URL that will be used for cloning the repo
	cloneURL       *url.URL
	repoAuth       transport.AuthMethod
	repositoryName string
	ref            string
	refKind        GitRefKind
	localRepoPath  string
	proxy          *url.URL
}

// GetName returns the repository name.
func (u *GitRepoStruct) GetName() string {
	return u.repositoryName
}

func (u *GitRepoStruct) GetLocalPath() string {
	return u.localRepoPath
}

// GetCloneURL returns the CloneURL of the repository.
func (u *GitRepoStruct) GetCloneURL() *url.URL {
	return u.cloneURL
}

func (u *GitRepoStruct) GetGitRef() (GitRefKind, string) {
	return u.refKind, u.ref
}

func (u *GitRepoStruct) SetGitRef(refKind GitRefKind, ref string) {
	u.ref = ref
	u.refKind = refKind
}

func (u *GitRepoStruct) SetLocalPath(localPath string) {
	u.localRepoPath = localPath
}

func (u *GitRepoStruct) SetProxy(proxy *url.URL) {
	u.proxy = proxy
}

func (u *GitRepoStruct) GetProxy() *url.URL {
	return u.proxy
}

func (u *GitRepoStruct) SetAuth(auth transport.AuthMethod) {
	u.repoAuth = auth
}

func (u *GitRepoStruct) GetAuth() transport.AuthMethod {
	return u.repoAuth
}

type GitRepoSpec interface {
	GetName() string
	GetCloneURL() *url.URL
	SetGitRef(refKind GitRefKind, ref string)
	GetGitRef() (GitRefKind, string)
	GetLocalPath() string
	SetLocalPath(string)
	SetProxy(*url.URL)
	GetProxy() *url.URL
	SetAuth(transport.AuthMethod)
	GetAuth() transport.AuthMethod
}

// NewRepoSpec parses the given git urlPath and returns an interface
// that is backed by Github or Gitlab repo implementations.
func NewRepoSpec(urlPath *url.URL) (GitRepoSpec, error) {
	var r GitRepoSpec
	var err error

	r = &GitRepoStruct{
		cloneURL: urlPath,
	}

	return r, err
}
