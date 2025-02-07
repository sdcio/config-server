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
	"path"

	"github.com/henderiw/logger/log"
	invv1alpha1 "github.com/sdcio/config-server/apis/inv/v1alpha1"
	"github.com/sdcio/config-server/pkg/git"
	"github.com/sdcio/config-server/pkg/git/auth"
	"github.com/sdcio/config-server/pkg/utils"
	"k8s.io/apimachinery/pkg/types"
)

type Loader struct {
	workspaceDir       string
	credentialResolver auth.CredentialResolver
}

func NewLoader(workspaceDir string, credentialResolver auth.CredentialResolver) (*Loader, error) {
	if !utils.DirExists(workspaceDir) {
		if err := utils.CreateDirectory(workspaceDir, 0766); err != nil {
			return nil, err
		}
	}
	return &Loader{
		workspaceDir:       workspaceDir,
		credentialResolver: credentialResolver,
	}, nil
}

func (r *Loader) EnsureCommit(ctx context.Context, workspace *invv1alpha1.Workspace) (string, error) {
	log := log.FromContext(ctx)

	repo, err := git.NewRepo(workspace.Spec.RepositoryURL)
	if err != nil {
		return "", err
	}
	repoPath := path.Join(r.workspaceDir, repo.GetCloneURL().Path)
	repo.SetLocalPath(repoPath)

	// init the actual git instance
	goGit := git.NewGoGit(repo,
		types.NamespacedName{
			Namespace: workspace.Namespace,
			Name:      workspace.Spec.Credentials},
		r.credentialResolver,
	)
	if workspace.Spec.Proxy.URL != "" {
		err = goGit.SetProxy(workspace.Spec.Proxy.URL)
		if err != nil {
			return "", err
		}
		log.Debug("SetProxy", "proxy", workspace.Spec.Proxy.URL)
	}
	return goGit.EnsureCommit(ctx, workspace.Spec.Ref)
}
