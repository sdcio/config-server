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
	"net/http"
	"net/url"
	"os"

	gogit "github.com/go-git/go-git/v5"
	"github.com/go-git/go-git/v5/config"
	"github.com/go-git/go-git/v5/plumbing"
	"github.com/go-git/go-git/v5/plumbing/transport"
	"github.com/go-git/go-git/v5/plumbing/transport/client"
	githttp "github.com/go-git/go-git/v5/plumbing/transport/http"
	"github.com/go-git/go-git/v5/storage/memory"
	"github.com/henderiw/logger/log"
	sdcerrors "github.com/sdcio/config-server/pkg/errors"
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
	ProxyURL   *url.URL
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

func (g *GoGit) SetProxy(p string) error {
	var err error
	g.ProxyURL, err = url.Parse(p)
	if err != nil {
		return err
	}
	return nil
}

// Clone takes the given GitRepo reference and clones the repo
// with its internal implementation.
func (g *GoGit) Clone(ctx context.Context) error {
	log := log.FromContext(ctx)
	// if the directory is not present
	if s, err := os.Stat(g.gitRepo.GetLocalPath()); errors.Is(err, fs.ErrNotExist) {
		log.Info("cloning a new local repository")
		return g.cloneNonExisting(ctx)
	} else if s.IsDir() {
		log.Info("updating an existing local repository")
		return g.cloneExistingRepo(ctx)
	}
	return &sdcerrors.UnrecoverableError{Message: fmt.Sprintf("repo %q exists, but is a file", g.gitRepo.GetName())}
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
		return "", &sdcerrors.UnrecoverableError{Message: "cannot get default branch", WrappedError: err}
	}

	for _, ref := range refs {
		if ref.Type() == plumbing.SymbolicReference && ref.Name() == plumbing.HEAD {
			return ref.Target().Short(), nil
		}
	}

	return "", &sdcerrors.UnrecoverableError{Message: fmt.Sprintf("unable to determine default branch for %q", g.gitRepo.GetCloneURL().String())}
}

func (g *GoGit) openRepo(_ context.Context) error {
	var err error

	// load the git repository
	g.r, err = gogit.PlainOpen(g.gitRepo.GetLocalPath())
	if err != nil {
		return &sdcerrors.UnrecoverableError{Message: "cannot open repo", WrappedError: err}
	}
	return nil
}

func (g *GoGit) cloneExistingRepo(ctx context.Context) error {
	var err error

	log := log.FromContext(ctx)
	log.Info("loading git", "repo", g.gitRepo.GetLocalPath())

	// if the ProxyURL is set, use custom transport as per https://github.com/go-git/go-git/blob/master/_examples/custom_http/main.go
	if g.ProxyURL != nil {
		log.Info("Proxy is set to ", g.ProxyURL)
		customClient := &http.Client{
			Transport: &http.Transport{
				Proxy: http.ProxyURL(g.ProxyURL),
			},
		}
		client.InstallProtocol("https", githttp.NewClient(customClient))
		client.InstallProtocol("http", githttp.NewClient(customClient))
	}

	// open the existing repo
	err = g.openRepo(ctx)
	if err != nil {
		return err
	}

	// loading remote
	remote, err := g.r.Remote("origin")
	if err != nil {
		return &sdcerrors.UnrecoverableError{Message: "cannot get remote from repo", WrappedError: err}
	}

	// checking that the configured remote equals the provided remote
	if remote.Config().URLs[0] != g.gitRepo.GetCloneURL().String() {
		return &sdcerrors.UnrecoverableError{Message: fmt.Sprintf("repository url of %q differs (%q) from the provided url (%q). stopping",
			g.gitRepo.GetName(), remote.Config().URLs[0], g.gitRepo.GetCloneURL().String())}
	}

	// We have a shallow clone - we cannot simply pull new changes. See:
	// https://stackoverflow.com/a/41081908 for a detailed explanation of how this works
	// We need to fetch and then reset && clean to update the repo contents - otherwise can be left with
	// 'object not found' error, presumably because there is no link between the two commits (due to shallow clone)

	// get the branch or tag or figure out the default branch main / master / sth. else.
	var refRemoteName plumbing.ReferenceName
	var refName plumbing.ReferenceName
	branch := g.gitRepo.GetBranch()
	tag := g.gitRepo.GetTag()
	if branch != "" {
		refName = plumbing.NewBranchReferenceName(branch)
		refRemoteName = plumbing.NewRemoteReferenceName("origin", branch)
	} else if tag != "" {
		refRemoteName = plumbing.NewTagReferenceName(tag)
		refName = plumbing.NewTagReferenceName(tag)
	} else {
		log.Debug("default branch not set. determining it")
		branch, err = g.getDefaultBranch(ctx)
		if err != nil {
			return err
		}
		refRemoteName = plumbing.NewRemoteReferenceName("origin", branch)
		refName = plumbing.NewBranchReferenceName(branch)
		log.Debug("default", "branch", branch)
	}
	refSpec := config.RefSpec(fmt.Sprintf("+%s:%s", refName, refRemoteName))

	log.Debug("fetching latest repo data")
	// execute the fetch
	err = g.doGitWithAuth(ctx, func(auth transport.AuthMethod) error {
		return g.r.FetchContext(ctx, &gogit.FetchOptions{
			Depth: 1,
			Auth:  auth,
			Force: true,
			Prune: true,
			RefSpecs: []config.RefSpec{
				refSpec,
			},
		})
	})
	switch {
	case errors.Is(err, gogit.NoErrAlreadyUpToDate):
		err = nil
	}
	if err != nil {
		return &sdcerrors.UnrecoverableError{Message: "cannot perform fetch", WrappedError: err}
	}

	// get the worktree reference
	tree, err := g.r.Worktree()
	if err != nil {
		return &sdcerrors.UnrecoverableError{Message: "cannot get worktree", WrappedError: err}
	}

	revisionHash, err := g.r.ResolveRevision(plumbing.Revision(refRemoteName))
	if err != nil {
		return &sdcerrors.UnrecoverableError{Message: fmt.Sprintf("unable to resolve revision '%s'", refName), WrappedError: err}
	}
	err = tree.Reset(&gogit.ResetOptions{
		Mode:   gogit.HardReset,
		Commit: *revisionHash,
	})
	if err != nil {
		return &sdcerrors.UnrecoverableError{Message: "cannot perform hard reset on repository", WrappedError: err}
	}

	err = tree.Clean(&gogit.CleanOptions{Dir: true})
	if err != nil {
		return &sdcerrors.UnrecoverableError{Message: "cannot perform clean", WrappedError: err}
	}

	return nil
}

func (g *GoGit) fetchNonExistingBranch(ctx context.Context, branch string) error {
	// init the remote
	remote, err := g.r.Remote("origin")
	if err != nil {
		return &sdcerrors.UnrecoverableError{Message: "cannot get remote from repo", WrappedError: err}
	}

	// build the RefSpec, that wires the remote to the local branch
	localRef := plumbing.NewBranchReferenceName(branch)
	remoteRef := plumbing.NewRemoteReferenceName("origin", branch)
	refSpec := config.RefSpec(fmt.Sprintf("+%s:%s", localRef, remoteRef))

	// execute the fetch
	err = g.doGitWithAuth(ctx, func(auth transport.AuthMethod) error {
		return remote.Fetch(&gogit.FetchOptions{
			Depth:    1,
			RefSpecs: []config.RefSpec{refSpec},
			Auth:     auth,
		})
	})
	switch {
	case err == nil, errors.Is(err, gogit.NoErrAlreadyUpToDate):
	default:
		return &sdcerrors.UnrecoverableError{Message: "cannot fetch repo for branch that does not exist", WrappedError: err}
	}

	// make sure the branch is also showing up in .git/config
	err = g.r.CreateBranch(&config.Branch{
		Name:   branch,
		Remote: "origin",
		Merge:  localRef,
	})

	return &sdcerrors.UnrecoverableError{Message: "cannot create branch", WrappedError: err}
}

func (g *GoGit) cloneNonExisting(ctx context.Context) error {
	var err error
	// if the ProxyURL is set, use custom transport as per https://github.com/go-git/go-git/blob/master/_examples/custom_http/main.go
	if g.ProxyURL != nil {
		customClient := &http.Client{
			Transport: &http.Transport{
				Proxy: http.ProxyURL(g.ProxyURL),
			},
		}
		client.InstallProtocol("https", githttp.NewClient(customClient))
		client.InstallProtocol("http", githttp.NewClient(customClient))
	}
	// init clone options
	co := &gogit.CloneOptions{
		Depth:        1,
		URL:          g.gitRepo.GetCloneURL().String(),
		SingleBranch: true,
	}

	// set branch reference if set
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
func (g *GoGit) doGitWithAuth(ctx context.Context, op func(transport.AuthMethod) error) error {
	log := log.FromContext(ctx)
	auth, err := g.getAuthMethod(ctx, false)
	if err != nil {
		return err
	}
	err = op(auth)
	if err != nil {
		if !errors.Is(err, transport.ErrAuthenticationRequired) || !errors.Is(err, transport.ErrAuthorizationFailed) {
			return &sdcerrors.UnrecoverableError{Message: "authentication failed", WrappedError: err}
		}
		log.Info("Authentication failed. Trying to refresh credentials")
		// TODO: Consider having some kind of backoff here.
		auth, err := g.getAuthMethod(ctx, true)
		if err != nil {
			return err
		}
		err = op(auth)
		if err != nil {
			return &sdcerrors.UnrecoverableError{Message: "authentication failed", WrappedError: err}
		}
	}
	return nil
}

// getAuthMethod fetches the credentials for authenticating to git. It caches the
// credentials between calls and refresh credentials when the tokens have expired.
func (g *GoGit) getAuthMethod(ctx context.Context, forceRefresh bool) (transport.AuthMethod, error) {
	// If no secret is provided, we try without any auth.
	log := log.FromContext(ctx)
	log.Info("getAuthMethod", "secret", g.secret, "credential", g.credential)
	if g.secret.Name == "" {
		return nil, nil
	}

	if g.credential == nil || !g.credential.Valid() || forceRefresh {
		if cred, err := g.credentialResolver.ResolveCredential(ctx, g.secret); err != nil {
			return nil, &sdcerrors.UnrecoverableError{Message: "cannot obtain credentials", WrappedError: err}
		} else {
			g.credential = cred
		}
	}

	return g.credential.ToAuthMethod(), nil
}
