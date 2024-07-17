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
	"strings"
	"sync"

	"github.com/henderiw/logger/log"
	"github.com/otiai10/copy"
	invv1alpha1 "github.com/sdcio/config-server/apis/inv/v1alpha1"
	"github.com/sdcio/config-server/pkg/git/auth"
	"github.com/sdcio/config-server/pkg/utils"
	"k8s.io/apimachinery/pkg/types"
)

type Loader struct {
	tempDir            string
	schemaDir          string
	credentialResolver auth.CredentialResolver

	//schemas contains the Schema Reference indexed by Provider.Version key
	m       sync.RWMutex
	schemas map[string]*invv1alpha1.SchemaSpec
}

type Downloadable interface {
	Download(ctx context.Context) error
	LocalPath() (string, error)
}

type ProviderDownloader struct {
	destDir            string
	schemaSpec         *invv1alpha1.SchemaSpec
	credentialNSN      types.NamespacedName
	credentialResolver auth.CredentialResolver
}

func NewLoader(tempDir string, schemaDir string, credentialResolver auth.CredentialResolver) (*Loader, error) {
	var err error

	if !utils.DirExists(schemaDir) {
		err = utils.CreateDirectory(schemaDir, 0766)
		if err != nil {
			return nil, err
		}
	}
	if !utils.DirExists(tempDir) {
		err = utils.CreateDirectory(tempDir, 0766)
		if err != nil {
			return nil, err
		}
	}

	return &Loader{
		tempDir:            tempDir,
		schemaDir:          schemaDir,
		schemas:            map[string]*invv1alpha1.SchemaSpec{},
		credentialResolver: credentialResolver,
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
		// ref does not exist -> we dont return an error
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

// GetRef return an error if the ref does not exist
// If the ref exists the ref is retrieved with an indication if the base provider schema version dir exists
func (r *Loader) GetRef(ctx context.Context, key string) (*invv1alpha1.SchemaSpec, bool, error) {
	ref, exists := r.get(key)
	if !exists {
		return nil, false, fmt.Errorf("no repository reference registered for key %q", key)
	}
	baseRefPath := ref.GetBasePath(r.schemaDir)

	return ref, utils.DirExists(baseRefPath), nil
}

func (r *Loader) get(key string) (*invv1alpha1.SchemaSpec, bool) {
	r.m.RLock()
	defer r.m.RUnlock()
	ref, exists := r.schemas[key]
	return ref, exists
}

func (r *Loader) Load(ctx context.Context, key string, credentialNSN types.NamespacedName) error {
	logger := log.FromContext(ctx)

	schemaSpec, _, err := r.GetRef(ctx, key)
	if err != nil {
		return err
	}

	var providerDownloader Downloadable

	// maybe there is a better way to verify this?
	if strings.HasSuffix(schemaSpec.RepositoryURL, ".git") {
		providerDownloader = NewGitDownloader(r.tempDir, schemaSpec, r.credentialResolver, credentialNSN)
	}
	// here we can use other downloaders e.g. OCI, ...

	if providerDownloader == nil {
		return fmt.Errorf("no provider found for url %q", schemaSpec.RepositoryURL)
	}
	err = providerDownloader.Download(ctx)
	if err != nil {
		return err
	}

	localPath, err := providerDownloader.LocalPath()
	if err != nil {
		return err
	}
	providerVersionBasePath := schemaSpec.GetBasePath(r.schemaDir)

	// copy data to correct destination
	if len(schemaSpec.Dirs) == 0 {
		schemaSpec.Dirs = []invv1alpha1.SrcDstPath{{Src: ".", Dst: "."}}
	}
	for i, dir := range schemaSpec.Dirs {
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

		logger.Info("copying", "index", fmt.Sprintf("%d, %d", i+1, len(schemaSpec.Dirs)), "from", src, "to", dst)
		err = copy.Copy(src, dst)
		if err != nil {
			return err
		}
	}
	return nil

}
