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
	"github.com/go-git/go-git/v5/plumbing"
)

const (
	//DefaultMainReferenceName plumbing.ReferenceName = "refs/heads/main"
	OriginName string     = "origin"
	MainBranch BranchName = "main"

	BranchPrefixInRemoteRepo = "refs/remotes/" + OriginName + "/"
	BranchPrefixInLocalRepo  = "refs/heads/"
	TagsPrefixInLocalRepo    = "refs/tags/"
	TagsPrefixInRemoteRepo   = "refs/tags/"

)

var (
	DefaultMainReferenceName plumbing.ReferenceName = plumbing.ReferenceName(BranchPrefixInLocalRepo + "/" + string(MainBranch))
)

// BranchName represents a relative branch name (i.e. 'main', 'drafts/bucket/v1')
// and supports transformation to the ReferenceName in local (cached) repository
// (those references are in the form 'refs/remotes/origin/...') or in the remote
// repository (those references are in the form 'refs/heads/...').
type BranchName string
type TagName string

func (b BranchName) BranchInLocal() plumbing.ReferenceName {
	return plumbing.ReferenceName(BranchPrefixInLocalRepo + string(b))
}

func (b BranchName) BranchInRemote() plumbing.ReferenceName {
	return plumbing.ReferenceName(BranchPrefixInRemoteRepo + string(b))
}

func (b TagName) TagInLocal() plumbing.ReferenceName {
	return plumbing.ReferenceName(TagsPrefixInLocalRepo + string(b))
}

func (b TagName) TagInRemote() plumbing.ReferenceName {
	return plumbing.ReferenceName(TagsPrefixInRemoteRepo + string(b))
}
