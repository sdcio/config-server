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
)

// GitRepoStruct is a struct that contains all the fields
// required for a GitRepo instance.
type GitRepoStruct struct {
	// CloneURL is the URL that will be used for cloning the repo
	CloneURL       *url.URL
	RepositoryName string
	// GitBranch is the referenced Git branch name.
	GitBranch string
	// Tag name - either tag or branch should be set.
	Tag           string
	LocalRepoPath string
}

// GetName returns the repository name.
func (u *GitRepoStruct) GetName() string {
	return u.RepositoryName
}

func (u *GitRepoStruct) GetLocalPath() string {
	return u.LocalRepoPath
}

// GetBranch returns the referenced Git branch name.
// the empty string is returned otherwise.
func (u *GitRepoStruct) GetBranch() string {
	return u.GitBranch
}

// GetCloneURL returns the CloneURL of the repository.
func (u *GitRepoStruct) GetCloneURL() *url.URL {
	return u.CloneURL
}

func (u *GitRepoStruct) GetTag() string {
	return u.Tag
}

func (u *GitRepoStruct) SetTag(t string) {
	u.Tag = t
}

func (u *GitRepoStruct) SetBranch(b string) {
	u.GitBranch = b
}

func (u *GitRepoStruct) SetLocalPath(p string) {
	u.LocalRepoPath = p
}

type GitRepo interface {
	GetName() string
	GetCloneURL() *url.URL
	GetBranch() string
	GetLocalPath() string
	GetTag() string
	SetTag(string)
	SetBranch(string)
	SetLocalPath(string)
}

// NewRepo parses the given git urlPath and returns an interface
// that is backed by Github or Gitlab repo implementations.
func NewRepo(urlPath string) (GitRepo, error) {
	var r GitRepo
	var err error

	u, err := url.Parse(urlPath)
	if err != nil {
		return nil, err
	}

	r = &GitRepoStruct{
		CloneURL: u,
	}

	return r, err
}
