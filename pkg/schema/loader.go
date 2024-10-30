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
	"path"
	"sync"

	"github.com/henderiw/logger/log"
	"github.com/otiai10/copy"
	invv1alpha1 "github.com/sdcio/config-server/apis/inv/v1alpha1"
	sdcerrors "github.com/sdcio/config-server/pkg/errors"
	"github.com/sdcio/config-server/pkg/git/auth"
	"github.com/sdcio/config-server/pkg/utils"
)

type Loader struct {
	tmpDir             string
	schemaDir          string
	credentialResolver auth.CredentialResolver

	//schemas contains the Schema Reference indexed by Provider.Version key
	m       sync.RWMutex
	schemas map[string]*invv1alpha1.Schema
}

type downloadable interface {
	Download(ctx context.Context) error
	LocalPath(urlPath string) (string, error)
}

type providerDownloader struct {
	destDir            string
	schema             *invv1alpha1.Schema
	credentialResolver auth.CredentialResolver
}

func NewLoader(tmpDir string, schemaDir string, credentialResolver auth.CredentialResolver) (*Loader, error) {
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
		tmpDir:             tmpDir,
		schemaDir:          schemaDir,
		schemas:            map[string]*invv1alpha1.Schema{},
		credentialResolver: credentialResolver,
	}, nil
}

// AddRef overwrites the provider schema version
// The schemaRef is immutable
func (r *Loader) AddRef(ctx context.Context, schema *invv1alpha1.Schema) {
	r.m.Lock()
	defer r.m.Unlock()
	r.schemas[schema.Spec.GetKey()] = schema
}

// DelRef deletes the provider schema version
func (r *Loader) DelRef(ctx context.Context, key string) error {
	schema, dirExists, err := r.GetRef(ctx, key)
	if err != nil {
		// ref does not exist -> we dont return an error
		return nil
	}
	if dirExists {
		if err := utils.RemoveDirectory(schema.Spec.GetBasePath(r.schemaDir)); err != nil {
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

// GetRef return an error if the ref does not exist
// If the ref exists the ref is retrieved with an indication if the base provider schema version dir exists
func (r *Loader) GetRef(ctx context.Context, key string) (*invv1alpha1.Schema, bool, error) {
	schema, exists := r.get(key)
	if !exists {
		return nil, false, fmt.Errorf("no repository reference registered for key %q", key)
	}
	baseRefPath := schema.Spec.GetBasePath(r.schemaDir)

	return schema, utils.DirExists(baseRefPath), nil
}

func (r *Loader) get(key string) (*invv1alpha1.Schema, bool) {
	r.m.RLock()
	defer r.m.RUnlock()
	schema, exists := r.schemas[key]
	return schema, exists
}

func (r *Loader) Load(ctx context.Context, key string) error {
	log := log.FromContext(ctx)

	schema, _, err := r.GetRef(ctx, key)
	if err != nil {
		return err
	}

	

	// for now we only use git, but in the future we can extend this to use other downloaders e.g. OCI/...
	var downloader downloadable
	switch {
	default:
		downloader = newGitDownloader(r.tmpDir, schema, r.credentialResolver)
	}

	if downloader == nil {
		return &sdcerrors.UnrecoverableError{
			Message:      "could not detect repository type",
			WrappedError: fmt.Errorf("no provider found for schema %q", schema.GetName()),
		}
	}
	err = downloader.Download(ctx)
	if err != nil {
		return err
	}

	for _, schemaRepo := range schema.Spec.Repositories {
		localPath, err := downloader.LocalPath(schemaRepo.RepositoryURL)
		if err != nil {
			return err
		}
		providerVersionBasePath := schema.Spec.GetBasePath(r.schemaDir)

		// copy data to correct destination
		if len(schemaRepo.Dirs) == 0 {
			schemaRepo.Dirs = []invv1alpha1.SrcDstPath{{Src: ".", Dst: "."}}
		}
		for i, dir := range schemaRepo.Dirs {
			// build the source path
			src := path.Join(localPath, dir.Src)
			// check path is still within the base schema folder
			// -> prevent escaping the folder
			err := utils.ErrNotIsSubfolder(localPath, src)
			if err != nil {
				return err
			}
			// build dst path
			dst := path.Join(providerVersionBasePath, dir.Dst)
			// check path is still within the base schema folder
			// -> prevent escaping the folder
			err = utils.ErrNotIsSubfolder(providerVersionBasePath, dst)
			if err != nil {
				return err
			}

			log.Info("copying", "index", fmt.Sprintf("%d, %d", i+1, len(schemaRepo.Dirs)), "from", src, "to", dst)
			err = copy.Copy(src, dst)
			if err != nil {
				return err
			}
		}
	}

	return nil

}
