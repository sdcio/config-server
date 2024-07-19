package schema

import (
	"context"
	"path"

	invv1alpha1 "github.com/sdcio/config-server/apis/inv/v1alpha1"
	"github.com/sdcio/config-server/pkg/git"
	"github.com/sdcio/config-server/pkg/git/auth"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/log"
)

type gitDownloader struct {
	providerDownloader
}

func newGitDownloader(destDir string, schemaSpec *invv1alpha1.SchemaSpec, credentialResolver auth.CredentialResolver, credentialNSN types.NamespacedName) *gitDownloader {
	return &gitDownloader{
		providerDownloader{
			destDir:            destDir,
			schemaSpec:         schemaSpec,
			credentialResolver: credentialResolver,
			credentialNSN:      credentialNSN,
		},
	}
}

func (l *gitDownloader) Download(ctx context.Context) error {
	log := log.FromContext(ctx)

	repo, err := git.NewRepo(l.schemaSpec.RepositoryURL)
	if err != nil {
		return err
	}

	repoPath := path.Join(l.destDir, repo.GetCloneURL().Path)
	repo.SetLocalPath(repoPath)

	// set branch or tag
	if l.schemaSpec.Kind == invv1alpha1.BranchTagKindBranch {
		repo.SetBranch(l.schemaSpec.Ref)
	} else {
		// set the git tag that we're after
		// if both branch and tag are the empty string
		// the git impl will retrieve the default branch
		repo.SetTag(l.schemaSpec.Ref)
	}

	// init the actual git instance
	goGit := git.NewGoGit(repo, l.credentialNSN, l.credentialResolver)

	log.Info("cloning", "from", repo.GetCloneURL(), "to", repo.GetLocalPath())

	err = goGit.Clone(ctx)
	if err != nil {
		return err
	}
	return nil
}

func (l *gitDownloader) LocalPath() (string, error) {
	repo, err := git.NewRepo(l.schemaSpec.RepositoryURL)
	if err != nil {
		return "", err
	}

	return path.Join(l.destDir, repo.GetCloneURL().Path), nil
}
