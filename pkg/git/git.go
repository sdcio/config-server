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
	"io/fs"
	"os"

	gogit "github.com/go-git/go-git/v5"
	"github.com/go-git/go-git/v5/config"
	"github.com/go-git/go-git/v5/plumbing"
	"github.com/go-git/go-git/v5/plumbing/transport"
	"github.com/go-git/go-git/v5/storage/memory"
	"github.com/henderiw/logger/log"
	sdcerror "github.com/sdcio/config-server/pkg/error"
	"github.com/sdcio/config-server/pkg/git/auth"
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
func (r *GoGit) Clone(ctx context.Context) error {
	log := log.FromContext(ctx)
	// if the directory is not present
	if s, err := os.Stat(r.gitRepo.GetLocalPath()); errors.Is(err, fs.ErrNotExist) {
		log.Info("cloning a new local repository")
		return r.cloneNonExisting(ctx)
	} else if s.IsDir() {
		log.Info("updating an existing local repository")
		return r.cloneExistingRepo(ctx)
	}
	return &sdcerror.UnrecoverableError{Message: fmt.Sprintf("repo %q exists, but is a file", r.gitRepo.GetName())}
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
		return "", &sdcerror.UnrecoverableError{Message: "cannot get default branch", WrappedError: err}
	}

	for _, ref := range refs {
		if ref.Type() == plumbing.SymbolicReference && ref.Name() == plumbing.HEAD {
			return ref.Target().Short(), nil
		}
	}

	return "", &sdcerror.UnrecoverableError{Message: fmt.Sprintf("unable to determine default branch for %q", g.gitRepo.GetCloneURL().String())}
}

func (g *GoGit) openRepo(_ context.Context) error {
	var err error

	// load the git repository
	g.r, err = gogit.PlainOpen(g.gitRepo.GetLocalPath())
	if err != nil {
		return &sdcerror.UnrecoverableError{Message: "cannot open repo", WrappedError: err}
	}
	return nil
}

func (r *GoGit) cloneExistingRepo(ctx context.Context) error {
	log := log.FromContext(ctx)
	log.Info("loading git", "repo", r.gitRepo.GetLocalPath())

	// open the existing repo
	err := r.openRepo(ctx)
	if err != nil {
		return err
	}

	// loading remote
	remote, err := r.r.Remote("origin")
	if err != nil {
		return &sdcerror.UnrecoverableError{Message: "cannot get remote from repo", WrappedError: err}
	}

	// checking that the configured remote equals the provided remote
	if remote.Config().URLs[0] != r.gitRepo.GetCloneURL().String() {
		return &sdcerror.UnrecoverableError{Message: fmt.Sprintf("repository url of %q differs (%q) from the provided url (%q). stopping",
			r.gitRepo.GetName(), remote.Config().URLs[0], r.gitRepo.GetCloneURL().String())}
	}

	// get the worktree reference
	tree, err := r.r.Worktree()
	if err != nil {
		return &sdcerror.UnrecoverableError{Message: "cannot get worktree", WrappedError: err}
	}

	// prepare the checkout options
	checkoutOpts := &gogit.CheckoutOptions{}

	// resolve the branch
	// the branch ref from the URL might be empty -> ""
	// then we need to figure out whats the default branch main / master / sth. else.
	branch := r.gitRepo.GetBranch()
	tag := r.gitRepo.GetTag()
	isTag := false
	if branch != "" {
		checkoutOpts.Branch = plumbing.NewBranchReferenceName(branch)
	} else if tag != "" {
		checkoutOpts.Branch = plumbing.NewTagReferenceName(tag)
		isTag = true
	} else {
		log.Debug("default branch not set. determining it")
		branch, err = r.getDefaultBranch(ctx)
		if err != nil {
			return err
		}
		log.Debug("default", "branch", branch)
	}

	// check if the branch already exists locally.
	// if not fetch it and check it out.
	if _, err = r.r.Reference(plumbing.NewBranchReferenceName(branch), false); !isTag && err != nil {
		err = r.fetchNonExistingBranch(ctx, branch)
		if err != nil {
			return &sdcerror.UnrecoverableError{Message: "cannot get reference", WrappedError: err}
		}

		ref, err := r.r.Reference(plumbing.NewRemoteReferenceName("origin", branch), true)
		if err != nil {
			return &sdcerror.UnrecoverableError{Message: "cannot get remote reference", WrappedError: err}
		}

		checkoutOpts.Hash = ref.Hash()
		checkoutOpts.Create = true
	}

	if isTag {
		log.Debug("checking out", "tag", tag)
	} else {
		log.Debug("checking out", "branch", branch)
	}

	// execute the checkout
	err = tree.Checkout(checkoutOpts)
	if err != nil {
		return &sdcerror.UnrecoverableError{Message: "cannot checkout tree", WrappedError: err}
	}

	if !isTag {
		log.Debug("pulling latest repo data")
		// execute the pull
		err = r.doGitWithAuth(ctx, func(auth transport.AuthMethod) error {
			return tree.Pull(&gogit.PullOptions{
				Depth:        1,
				SingleBranch: true,
				Force:        true,
				Auth:         auth,
			})
		})
		switch {
		case err == nil, errors.Is(err, gogit.NoErrAlreadyUpToDate):
			return nil
		default:
			return err
		}
	}
	return err
}

func (r *GoGit) fetchNonExistingBranch(ctx context.Context, branch string) error {
	// init the remote
	remote, err := r.r.Remote("origin")
	if err != nil {
		return &sdcerror.UnrecoverableError{Message: "cannot get remote from repo", WrappedError: err}
	}

	// build the RefSpec, that wires the remote to the local branch
	localRef := plumbing.NewBranchReferenceName(branch)
	remoteRef := plumbing.NewRemoteReferenceName("origin", branch)
	refSpec := config.RefSpec(fmt.Sprintf("+%s:%s", localRef, remoteRef))

	// execute the fetch
	err = r.doGitWithAuth(ctx, func(auth transport.AuthMethod) error {
		return remote.Fetch(&gogit.FetchOptions{
			Depth:    1,
			RefSpecs: []config.RefSpec{refSpec},
			Auth:     auth,
		})
	})
	switch {
	case err == nil, errors.Is(err, gogit.NoErrAlreadyUpToDate):
	default:
		return &sdcerror.UnrecoverableError{Message: "cannot fetch repo for branch that does not exist", WrappedError: err}
	}

	// make sure the branch is also showing up in .git/config
	err = r.r.CreateBranch(&config.Branch{
		Name:   branch,
		Remote: "origin",
		Merge:  localRef,
	})

	return &sdcerror.UnrecoverableError{Message: "cannot create branch", WrappedError: err}
}

func (r *GoGit) cloneNonExisting(ctx context.Context) error {
	var err error
	// init clone options
	co := &gogit.CloneOptions{
		Depth:        1,
		URL:          r.gitRepo.GetCloneURL().String(),
		SingleBranch: true,
	}

	// set branch reference if set
	if r.gitRepo.GetBranch() != "" {
		co.ReferenceName = plumbing.NewBranchReferenceName(r.gitRepo.GetBranch())
	} else if r.gitRepo.GetTag() != "" {
		co.ReferenceName = plumbing.NewTagReferenceName(r.gitRepo.GetTag())
	} else {
		branchName, err := r.getDefaultBranch(ctx)
		if err != nil {
			return err
		}
		co.ReferenceName = plumbing.NewBranchReferenceName(branchName)
	}

	// perform clone
	return r.doGitWithAuth(ctx, func(auth transport.AuthMethod) error {
		co.Auth = auth
		r.r, err = gogit.PlainClone(r.gitRepo.GetLocalPath(), false, co)
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
		return err
	}
	err = op(auth)
	if err != nil {
		if !errors.Is(err, transport.ErrAuthenticationRequired) || !errors.Is(err, transport.ErrAuthorizationFailed) {
			return &sdcerror.UnrecoverableError{Message: "authentication failed", WrappedError: err}
		}
		log.Info("Authentication failed. Trying to refresh credentials")
		// TODO: Consider having some kind of backoff here.
		auth, err := r.getAuthMethod(ctx, true)
		if err != nil {
			return err
		}
		err = op(auth)
		if err != nil {
			return &sdcerror.UnrecoverableError{Message: "authentication failed", WrappedError: err}
		}
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
			return nil, &sdcerror.UnrecoverableError{Message: "cannot obtain credentials", WrappedError: err}
		} else {
			r.credential = cred
		}
	}

	return r.credential.ToAuthMethod(), nil
}
