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
	"fmt"
	"os"
	"path"
	"path/filepath"

	"github.com/henderiw/apiserver-store/pkg/storebackend"
	memstore "github.com/henderiw/apiserver-store/pkg/storebackend/memory"
	"github.com/henderiw/logger/log"
	"github.com/sdcio/config-server/apis/config"
	configv1alpha1 "github.com/sdcio/config-server/apis/config/v1alpha1"
	invv1alpha1 "github.com/sdcio/config-server/apis/inv/v1alpha1"
	"github.com/sdcio/config-server/pkg/git"
	"github.com/sdcio/config-server/pkg/git/auth"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/yaml"
)

type Reader struct {
	workspaceDir       string
	credentialResolver auth.CredentialResolver
}

func NewReader(workspaceDir string, credentialResolver auth.CredentialResolver) (*Reader, error) {
	return &Reader{
		workspaceDir:       workspaceDir,
		credentialResolver: credentialResolver,
	}, nil
}

func (r *Reader) GetConfigs(ctx context.Context, rollout *invv1alpha1.Rollout) (storebackend.Storer[storebackend.Storer[*config.Config]], error) {
	log := log.FromContext(ctx)

	repo, err := git.NewRepo(rollout.Spec.RepoURL)
	if err != nil {
		return nil, err
	}
	repoPath := path.Join(r.workspaceDir, repo.GetCloneURL().Path)
	repo.SetLocalPath(repoPath)

	// init the actual git instance
	goGit := git.NewGoGit(repo,
		types.NamespacedName{
			Namespace: rollout.Namespace,
			Name:      rollout.Spec.Credentials},
		r.credentialResolver,
	)
	if rollout.Spec.Proxy != nil && rollout.Spec.Proxy.URL != "" {
		err = goGit.SetProxy(rollout.Spec.Proxy.URL)
		if err != nil {
			return nil, err
		}
		log.Debug("SetProxy", "proxy", rollout.Spec.Proxy.URL)
	}
	if err := goGit.CheckoutCommit(ctx, rollout.Spec.Ref); err != nil {
		return nil, err
	}
	// Extract configurations from the checked-out commit
	return extractConfigsFromRepo(ctx, repoPath)
}

func extractConfigsFromRepo(ctx context.Context, repoPath string) (storebackend.Storer[storebackend.Storer[*config.Config]], error) {
	newTargetConfigStore := memstore.NewStore[storebackend.Storer[*config.Config]]()

	err := filepath.Walk(repoPath, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}

		// Process only YAML files
		if info.IsDir() || (filepath.Ext(path) != ".yaml" && filepath.Ext(path) != ".yml") {
			return nil
		}

		// Read YAML file
		data, err := os.ReadFile(path)
		if err != nil {
			return fmt.Errorf("failed to read file %s: %w", path, err)
		}

		var cfg *configv1alpha1.Config
		if err := yaml.Unmarshal(data, &cfg); err != nil {
			return fmt.Errorf("failed to parse YAML in file %s: %w", path, err)
		}

		// Check if it matches expected apiVersion and kind
		if cfg.APIVersion == config.SchemeGroupVersion.Identifier() && cfg.Kind == config.ConfigKind {
			targetName, ok := cfg.Labels[config.TargetNameKey]
			if !ok {
				return nil // Ignore files without a targetName
			}
			targetNamespace, ok := cfg.Labels[config.TargetNamespaceKey]
			if !ok {
				return nil // Ignore files without a targetName
			}
			targetKey := types.NamespacedName{Namespace: targetNamespace, Name: targetName}

			configStore, err := newTargetConfigStore.Get(ctx, storebackend.KeyFromNSN(targetKey))
			if err != nil {
				// if the target is not found we create a new config store
				// in which we store all configs for this target
				configStore = memstore.NewStore[*config.Config]()
				if err := newTargetConfigStore.Create(ctx, storebackend.KeyFromNSN(targetKey), configStore); err != nil {
					return err
				}
			}
			internalcfg := &config.Config{}
			if err := configv1alpha1.Convert_v1alpha1_Config_To_config_Config(cfg, internalcfg, nil); err != nil {
				return err
			}
			if err := configStore.Create(
				ctx,
				storebackend.KeyFromNSN(types.NamespacedName{Namespace: cfg.Namespace, Name: cfg.Name}),
				internalcfg,
			); err != nil {
				return err // this checks duplicates
			}
			if err := newTargetConfigStore.Update(ctx, storebackend.KeyFromNSN(targetKey), configStore); err != nil {
				return err
			}
		}
		return nil
	})

	if err != nil {
		return nil, err
	}

	return newTargetConfigStore, nil
}
