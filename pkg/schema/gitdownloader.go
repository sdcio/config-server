package schema

import (
	"context"
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

func newGitDownloader(destDir, namespace string, schemaRepo *invv1alpha1.SchemaSpecRepository, credentialResolver auth.CredentialResolver) *gitDownloader {
	return &gitDownloader{
		providerDownloader{
			destDir:            destDir,
			namespace:          namespace,
			schemaRepo:         schemaRepo,
			credentialResolver: credentialResolver,
		},
	}
}

func (l *gitDownloader) Download(ctx context.Context) error {
	log := log.FromContext(ctx)

	repo, err := git.NewRepo(l.schemaRepo.RepositoryURL)
	if err != nil {
		return err
	}

	repoPath := path.Join(l.destDir, repo.GetCloneURL().Path)
	repo.SetLocalPath(repoPath)

	// set branch or tag
	if l.schemaRepo.Kind == invv1alpha1.BranchTagKindBranch {
		repo.SetBranch(l.schemaRepo.Ref)
	} else {
		// set the git tag that we're after
		// if both branch and tag are the empty string
		// the git impl will retrieve the default branch
		repo.SetTag(l.schemaRepo.Ref)
	}

	// init the actual git instance
	goGit := git.NewGoGit(repo,
		types.NamespacedName{
			Namespace: l.namespace,
			Name:      l.schemaRepo.Credentials},
		l.credentialResolver,
	)

	log.Info("cloning", "from", repo.GetCloneURL(), "to", repo.GetLocalPath())

	if l.schemaRepo.Proxy.URL != "" {
		err = goGit.SetProxy(l.schemaRepo.Proxy.URL)
		if err != nil {
			return err
		}
		log.Debug("SetProxy", "proxy", l.schemaRepo.Proxy.URL)
	}

	return goGit.Clone(ctx)
}

func (l *gitDownloader) LocalPath(urlPath string) (string, error) {
	repo, err := git.NewRepo(urlPath)
	if err != nil {
		return "", err
	}

	return path.Join(l.destDir, repo.GetCloneURL().Path), nil
}
