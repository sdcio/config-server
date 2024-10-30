package schema

import (
	"context"
	"errors"
	"path"

	"github.com/henderiw/logger/log"
	invv1alpha1 "github.com/sdcio/config-server/apis/inv/v1alpha1"
	"github.com/sdcio/config-server/pkg/git"
	"github.com/sdcio/config-server/pkg/git/auth"
	"k8s.io/apimachinery/pkg/types"
)

type gitDownloader struct {
	providerDownloader
}

func newGitDownloader(destDir string, schema *invv1alpha1.Schema, credentialResolver auth.CredentialResolver) *gitDownloader {
	return &gitDownloader{
		providerDownloader{
			destDir:            destDir,
			schema:             schema,
			credentialResolver: credentialResolver,
		},
	}
}

func (l *gitDownloader) Download(ctx context.Context) error {
	log := log.FromContext(ctx)

	var errm error
	for _, schemaRepo := range l.schema.Spec.Repositories {
		repo, err := git.NewRepo(schemaRepo.RepositoryURL)
		if err != nil {
			errm = errors.Join(errm, err)
			continue
		}

		repoPath := path.Join(l.destDir, repo.GetCloneURL().Path)
		repo.SetLocalPath(repoPath)

		// set branch or tag
		if schemaRepo.Kind == invv1alpha1.BranchTagKindBranch {
			repo.SetBranch(schemaRepo.Ref)
		} else {
			// set the git tag that we're after
			// if both branch and tag are the empty string
			// the git impl will retrieve the default branch
			repo.SetTag(schemaRepo.Ref)
		}

		// init the actual git instance
		goGit := git.NewGoGit(repo,
			types.NamespacedName{
				Namespace: l.schema.Namespace,
				Name:      schemaRepo.Credentials},
			l.credentialResolver,
		)

		log.Info("cloning", "from", repo.GetCloneURL(), "to", repo.GetLocalPath())

		if schemaRepo.Proxy.URL != "" {
			err = goGit.SetProxy(schemaRepo.Proxy.URL)
			if err != nil {
				errm = errors.Join(errm, err)
				continue
			}
			log.Debug("SetProxy", "proxy", schemaRepo.Proxy.URL)
		}

		err = goGit.Clone(ctx)
		if err != nil {
			errm = errors.Join(errm, err)
			continue
		}
	}
	return errm
}

func (l *gitDownloader) LocalPath(urlPath string) (string, error) {
	repo, err := git.NewRepo(urlPath)
	if err != nil {
		return "", err
	}

	return path.Join(l.destDir, repo.GetCloneURL().Path), nil
}
