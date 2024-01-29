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

package schema

import (
	"context"
	"fmt"
	"net/url"
	"path"
	"sync"

	"github.com/henderiw/logger/log"
	invv1alpha1 "github.com/iptecharch/config-server/apis/inv/v1alpha1"
	"github.com/iptecharch/config-server/pkg/git"
	"github.com/iptecharch/config-server/pkg/utils"
	"github.com/otiai10/copy"
)

type Loader struct {
	tmpDir    string
	schemaDir string

	//schemas contains the Schema Reference indexed by Provider.Version key
	m       sync.RWMutex
	schemas map[string]*invv1alpha1.SchemaSpec
}

func NewLoader(tmpDir, schemaDir string) (*Loader, error) {
	var err error

	if !utils.DirExists(tmpDir) {
		err = utils.CreateDirectory(tmpDir, 0766)
		if err != nil {
			return nil, err
		}
	}

	if !utils.DirExists(schemaDir) {
		err = utils.CreateDirectory(schemaDir, 0766)
		if err != nil {
			return nil, err
		}
	}

	return &Loader{
		tmpDir:    tmpDir,
		schemaDir: schemaDir,
		schemas:   map[string]*invv1alpha1.SchemaSpec{},
	}, nil
}

// AddRef overwrites the provider schema version
// The schemaRef is immutable
func (r *Loader) AddRef(ctx context.Context, spec *invv1alpha1.SchemaSpec) {
	r.m.Lock()
	defer r.m.Unlock()
	r.schemas[spec.GetKey()] = spec
}

// DelRef deletes the provider schema version
func (r *Loader) DelRef(ctx context.Context, key string) error {
	ref, dirExists, err := r.GetRef(ctx, key)
	if err != nil {
		// ref does not exist
		return nil
	}
	if dirExists {
		if err := utils.RemoveDirectory(ref.GetBasePath(r.schemaDir)); err != nil {
			return err
		}
	}
	r.del(key)
	return nil
}

func (r *Loader) del(key string) {
	r.m.Lock()
	defer r.m.Unlock()
	delete(r.schemas, key)
}

// GetRef return an error of the ref does not exist
// If the ref exists the ref is retrieved with an inidcation if the base provider schema version dir exists
func (r *Loader) GetRef(ctx context.Context, key string) (*invv1alpha1.SchemaSpec, bool, error) {
	ref, exists := r.get(key)
	if !exists {
		return nil, false, fmt.Errorf("no repository reference registered for key %q", key)
	}
	baseRefPath := ref.GetBasePath(r.schemaDir)
	if utils.DirExists(baseRefPath) {
		return ref, true, nil
	}
	return ref, false, nil
}

func (r *Loader) get(key string) (*invv1alpha1.SchemaSpec, bool) {
	r.m.RLock()
	defer r.m.RUnlock()
	ref, exists := r.schemas[key]
	return ref, exists
}

func (r *Loader) Load(ctx context.Context, key string) error {
	log := log.FromContext(ctx)
	spec, _, err := r.GetRef(ctx, key)
	if err != nil {
		return err
	}

	url, err := url.Parse(spec.RepositoryURL)
	if err != nil {
		return fmt.Errorf("error parsing repository url %q, %v", spec.RepositoryURL, err)
	}
	repo, err := git.NewGitHubRepoFromURL(url)
	if err != nil {
		return err
	}

	repoPath := path.Join(r.tmpDir, repo.ProjectOwner, repo.RepositoryName)
	repo.SetLocalPath(repoPath)

	// set branch or tag
	if spec.Kind == invv1alpha1.BranchTagKindBranch {
		repo.GitBranch = spec.Ref
	} else {
		// set the git tag that we're after
		// if both branch and tag are the empty string
		// the git impl will retrieve the default branch
		repo.Tag = spec.Ref
	}

	// init the actual git instance
	gogit := git.NewGoGit(repo)

	log.Info("cloning", "from", repo.GetCloneURL(), "to", repoPath)
	err = gogit.Clone()
	if err != nil {
		return err
	}

	provVersionBasePath := spec.GetBasePath(r.schemaDir)

	if len(spec.Dirs) == 0 {
		spec.Dirs = []invv1alpha1.SrcDstPath{{Src: ".", Dst: "."}}
	}
	for i, dir := range spec.Dirs {
		// build the source path
		src := path.Join(repoPath, dir.Src)
		// check path is still within the base schema folder
		// -> prevent escaping the folder
		err := utils.ErrNotIsSubfolder(repoPath, src)
		if err != nil {
			return err
		}
		// build dst path

		dst := path.Join(provVersionBasePath, dir.Dst)
		// check path is still within the base schema folder
		// -> prevent escaping the folder
		err = utils.ErrNotIsSubfolder(provVersionBasePath, dst)
		if err != nil {
			return err
		}

		log.Info("copying", "index", fmt.Sprintf("%d, %d", i+1, len(spec.Dirs)), "from", src, "to", dst)
		err = copy.Copy(src, dst)
		if err != nil {
			return err
		}
	}
	return nil
}
