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
	"context"
	"errors"
	"fmt"
	"os"

	gogit "github.com/go-git/go-git/v5"
	"github.com/go-git/go-git/v5/config"
	"github.com/go-git/go-git/v5/plumbing"
	"github.com/go-git/go-git/v5/plumbing/transport"
	"github.com/go-git/go-git/v5/storage/memory"
	"github.com/henderiw/logger/log"
	"github.com/iptecharch/config-server/pkg/git/auth"
	"k8s.io/apimachinery/pkg/types"
)

type GoGit struct {
	gitRepo            GitRepo
	r                  *gogit.Repository
	credentialResolver auth.CredentialResolver
	secret             types.NamespacedName
	// credential contains the information needed to authenticate against
	// a git repository.
	credential auth.Credential
}

// make sure GoGit satisfies the Git interface.
var _ Git = (*GoGit)(nil)

func NewGoGit(gitRepo GitRepo, secret types.NamespacedName, credentialResolver auth.CredentialResolver) *GoGit {
	return &GoGit{
		gitRepo:            gitRepo,
		credentialResolver: credentialResolver,
		secret:             secret,
	}
}

// Clone takes the given GitRepo reference and clones the repo
// with its internal implementation.
func (g *GoGit) Clone(ctx context.Context) error {
	log := log.FromContext(ctx)
	// if the directory is not present
	if s, err := os.Stat(g.gitRepo.GetLocalPath()); os.IsNotExist(err) {
		log.Info("cloning a new local repository")
		return g.cloneNonExisting(ctx)
	} else if s.IsDir() {
		log.Info("cloning an existing local repository")
		return g.cloneExistingRepo(ctx)
	}
	return fmt.Errorf("error %q exists already but is a file", g.gitRepo.GetName())
}

func (g *GoGit) getDefaultBranch(ctx context.Context) (string, error) {
	rem := gogit.NewRemote(memory.NewStorage(), &config.RemoteConfig{
		Name: "origin",
		URLs: []string{g.gitRepo.GetCloneURL().String()},
	})

	// We can then use every Remote functions to retrieve wanted information
	var refs []*plumbing.Reference
	if err := g.doGitWithAuth(ctx, func(auth transport.AuthMethod) error {
		var err error
		refs, err = rem.List(&gogit.ListOptions{
			Auth: auth,
		})
		return err
	}); err != nil {
		return "", err
	}

	for _, ref := range refs {
		if ref.Type() == plumbing.SymbolicReference && ref.Name() == plumbing.HEAD {
			return ref.Target().Short(), nil
		}
	}

	return "", fmt.Errorf("unable to determine default branch for %q", g.gitRepo.GetCloneURL().String())
}

func (g *GoGit) openRepo(ctx context.Context) error {
	var err error

	// load the git repository
	g.r, err = gogit.PlainOpen(g.gitRepo.GetLocalPath())
	if err != nil {
		return err
	}
	return nil
}

func (g *GoGit) cloneExistingRepo(ctx context.Context) error {
	log := log.FromContext(ctx)
	log.Info("loading git", "repo", g.gitRepo.GetLocalPath())

	// open the existing repo
	err := g.openRepo(ctx)
	if err != nil {
		return err
	}

	// loading remote
	remote, err := g.r.Remote("origin")
	if err != nil {
		return err
	}

	// checking that the configured remote equals the provided remote
	if remote.Config().URLs[0] != g.gitRepo.GetCloneURL().String() {
		return fmt.Errorf("repository url of %q differs (%q) from the provided url (%q). stopping",
			g.gitRepo.GetName(), remote.Config().URLs[0], g.gitRepo.GetCloneURL().String())
	}

	// get the worktree reference
	tree, err := g.r.Worktree()
	if err != nil {
		return err
	}

	// prepare the checkout options
	checkoutOpts := &gogit.CheckoutOptions{}

	// resolve the branch
	// the branch ref from the URL might be empty -> ""
	// then we need to figure out whats the default branch main / master / sth. else.
	branch := g.gitRepo.GetBranch()
	tag := g.gitRepo.GetTag()
	isTag := false
	if branch != "" {
		checkoutOpts.Branch = plumbing.NewBranchReferenceName(branch)
	} else if tag != "" {
		checkoutOpts.Branch = plumbing.NewTagReferenceName(tag)
		isTag = true
	} else {
		log.Info("default branch not set. determining it")
		branch, err = g.getDefaultBranch(ctx)
		if err != nil {
			return err
		}
		log.Info("default", "branch", branch)
	}

	// check if the branch already exists locally.
	// if not fetch it and check it out.
	if _, err = g.r.Reference(plumbing.NewBranchReferenceName(branch), false); !isTag && err != nil {
		err = g.fetchNonExistingBranch(ctx, branch)
		if err != nil {
			return err
		}

		ref, err := g.r.Reference(plumbing.NewRemoteReferenceName("origin", branch), true)
		if err != nil {
			return err
		}

		checkoutOpts.Hash = ref.Hash()
		checkoutOpts.Create = true
	}

	if isTag {
		log.Info("checking out", "tag", tag)
	} else {
		log.Info("checking out", "branch", branch)
	}

	// execute the checkout
	err = tree.Checkout(checkoutOpts)
	if err != nil {
		return err
	}

	if !isTag {
		log.Debug("pulling latest repo data")
		// execute the pull
		// execute the checkout
		switch err := g.doGitWithAuth(ctx, func(auth transport.AuthMethod) error {
			return tree.Pull(&gogit.PullOptions{
				Depth:        1,
				SingleBranch: true,
				Force:        true,
				Auth:         auth,
			})
		}); err {
		case nil, gogit.NoErrAlreadyUpToDate:
			err = nil
		default:
			return err
		}
	}

	return err
}

func (g *GoGit) fetchNonExistingBranch(ctx context.Context, branch string) error {
	// init the remote
	remote, err := g.r.Remote("origin")
	if err != nil {
		return err
	}

	// build the RefSpec, that wires the remote to the locla branch
	localRef := plumbing.NewBranchReferenceName(branch)
	remoteRef := plumbing.NewRemoteReferenceName("origin", branch)
	refSpec := config.RefSpec(fmt.Sprintf("+%s:%s", localRef, remoteRef))

	// execute the fetch
	switch err := g.doGitWithAuth(ctx, func(auth transport.AuthMethod) error {
		return remote.Fetch(&gogit.FetchOptions{
			Depth:    1,
			RefSpecs: []config.RefSpec{refSpec},
			Auth:     auth,
		})
	}); err {
	case nil, gogit.NoErrAlreadyUpToDate:
	default:
		return err
	}

	// make sure the branch is also showing up in .git/config
	err = g.r.CreateBranch(&config.Branch{
		Name:   branch,
		Remote: "origin",
		Merge:  localRef,
	})

	return err
}

func (g *GoGit) cloneNonExisting(ctx context.Context) error {
	var err error
	// init clone options
	co := &gogit.CloneOptions{
		Depth:        1,
		URL:          g.gitRepo.GetCloneURL().String(),
		SingleBranch: true,
	}

	// set brach reference if set
	if g.gitRepo.GetBranch() != "" {
		co.ReferenceName = plumbing.NewBranchReferenceName(g.gitRepo.GetBranch())
	} else if g.gitRepo.GetTag() != "" {
		co.ReferenceName = plumbing.NewTagReferenceName(g.gitRepo.GetTag())
	} else {
		branchName, err := g.getDefaultBranch(ctx)
		if err != nil {
			return err
		}
		co.ReferenceName = plumbing.NewBranchReferenceName(branchName)
	}

	// perform clone
	return g.doGitWithAuth(ctx, func(auth transport.AuthMethod) error {
		co.Auth = auth
		g.r, err = gogit.PlainClone(g.gitRepo.GetLocalPath(), false, co)
		return err
	})
}

type Git interface {
	// Clone takes the given GitRepo reference and clones the repo
	// with its internal implementation.
	Clone(ctx context.Context) error
}

// doGitWithAuth fetches auth information for git and provides it
// to the provided function which performs the operation against a git repo.
func (r *GoGit) doGitWithAuth(ctx context.Context, op func(transport.AuthMethod) error) error {
	log := log.FromContext(ctx)
	auth, err := r.getAuthMethod(ctx, false)
	if err != nil {
		return fmt.Errorf("failed to obtain git credentials: %w", err)
	}
	err = op(auth)
	if err != nil {
		if !errors.Is(err, transport.ErrAuthenticationRequired) {
			return err
		}
		log.Info("Authentication failed. Trying to refresh credentials")
		// TODO: Consider having some kind of backoff here.
		auth, err := r.getAuthMethod(ctx, true)
		if err != nil {
			return fmt.Errorf("failed to obtain git credentials: %w", err)
		}
		return op(auth)
	}
	return nil
}

// getAuthMethod fetches the credentials for authenticating to git. It caches the
// credentials between calls and refresh credentials when the tokens have expired.
func (r *GoGit) getAuthMethod(ctx context.Context, forceRefresh bool) (transport.AuthMethod, error) {
	// If no secret is provided, we try without any auth.

	log := log.FromContext(ctx)
	log.Info("getAuthMethod", "secret", r.secret, "credential", r.credential)
	if r.secret.Name == "" {
		return nil, nil
	}

	if r.credential == nil || !r.credential.Valid() || forceRefresh {
		if cred, err := r.credentialResolver.ResolveCredential(ctx, r.secret); err != nil {
			return nil, fmt.Errorf("failed to obtain credential from secret %s/%s: %w", "", "", err)
		} else {
			r.credential = cred
		}
	}

	return r.credential.ToAuthMethod(), nil
}
