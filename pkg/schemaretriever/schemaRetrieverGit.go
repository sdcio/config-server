package schemaretriever

import (
	"fmt"
	"net/url"
	"path"

	"github.com/iptecharch/config-server/pkg/git"
	"github.com/iptecharch/config-server/pkg/utils"
	"github.com/otiai10/copy"
	log "github.com/sirupsen/logrus"
)

type SchemaRetrieverGit struct {
	tmpFolder    string
	schemaFolder string

	//downloadableSchemas contains the SchemaDownloadDefinition indexed by Vendor, Kind, Version
	downloadableSchemas map[string]map[string]map[string]*SchemaDownloadRef
}

func NewSchemaRetrieverGit(tmpFolder, schemaFolder string) (*SchemaRetrieverGit, error) {
	var err error

	if !utils.DirExists(tmpFolder) {
		err = utils.CreateDirectory(tmpFolder, 0766)
		if err != nil {
			return nil, err
		}
	}

	if !utils.DirExists(schemaFolder) {
		err = utils.CreateDirectory(schemaFolder, 0766)
		if err != nil {
			return nil, err
		}
	}

	return &SchemaRetrieverGit{
		tmpFolder:           tmpFolder,
		schemaFolder:        schemaFolder,
		downloadableSchemas: map[string]map[string]map[string]*SchemaDownloadRef{},
	}, nil
}

func (s *SchemaRetrieverGit) AddSchemaRef(sdr *SchemaDownloadRef) error {
	_, exists := s.downloadableSchemas[sdr.Vendor]
	if !exists {
		s.downloadableSchemas[sdr.Vendor] = map[string]map[string]*SchemaDownloadRef{}
	}
	_, exists = s.downloadableSchemas[sdr.Vendor][sdr.Kind]
	if !exists {
		s.downloadableSchemas[sdr.Vendor][sdr.Kind] = map[string]*SchemaDownloadRef{}
	}
	_, exists = s.downloadableSchemas[sdr.Vendor][sdr.Kind][sdr.Version]
	if !exists {
		s.downloadableSchemas[sdr.Vendor][sdr.Kind][sdr.Version] = sdr
	} else {
		return fmt.Errorf("schema entry for %s - %s -%s already exists", sdr.Vendor, sdr.Kind, sdr.Version)
	}
	return nil
}

func (s *SchemaRetrieverGit) GetSchemaRef(vendor, kind, version string) (*SchemaDownloadRef, error) {
	vendorMap, exists := s.downloadableSchemas[vendor]
	if !exists {
		return nil, fmt.Errorf("no repository reference registered for vendor %q", vendor)
	}
	kindMap, exists := vendorMap[kind]
	if !exists {
		return nil, fmt.Errorf("no repository reference registered for vendor %q, kind %q", vendor, kind)
	}
	result, exists := kindMap[version]
	if !exists {
		return nil, fmt.Errorf("no repository reference registered for vendor %q, kind %q, version %q", vendor, kind, version)
	}
	return result, nil
}

func (s *SchemaRetrieverGit) Retrieve(vendor, kind, version string) (string, error) {
	gcd, err := s.GetSchemaRef(vendor, kind, version)
	if err != nil {
		return "", err
	}

	url, err := url.Parse(gcd.Repository)
	if err != nil {
		return "", fmt.Errorf("error parsing repository url %q, %v", gcd.Repository, err)
	}
	repo, err := git.NewGitHubRepoFromURL(url)
	if err != nil {
		return "", err
	}

	repoDst := path.Join(s.tmpFolder, repo.ProjectOwner, repo.RepositoryName)
	repo.SetLocalPath(repoDst)

	// set branch or tag
	if gcd.Branch != "" {
		repo.GitBranch = gcd.Branch
	} else {
		// set the git tag that we're after
		// if both branch and tag are the empty string
		// the git impl will retrieve the default branch
		repo.Tag = gcd.Tag
	}

	// init the actual git instance
	gogit := git.NewGoGit(repo)

	log.Infof("cloning %s into %s", repo.GetCloneURL(), repoDst)
	err = gogit.Clone()
	if err != nil {
		return "", err
	}

	schemaDst := path.Join(s.schemaFolder, gcd.Vendor, gcd.Kind, gcd.Version)

	for i, f := range gcd.Folders {
		// build the source path
		src := path.Join(repoDst, f.Src)
		// check path is still within the base schema folder
		// -> prevent escaping the folder
		err := utils.ErrNotIsSubfolder(repoDst, src)
		if err != nil {
			return "", err
		}
		// build dst path

		dst := path.Join(schemaDst, f.Dst)
		// check path is still within the base schema folder
		// -> prevent escaping the folder
		err = utils.ErrNotIsSubfolder(schemaDst, dst)
		if err != nil {
			return "", err
		}

		log.Infof("copying (%d/%d) - %s to %s", i+1, len(gcd.Folders), src, dst)
		err = copy.Copy(src, dst)
		if err != nil {
			return "", err
		}
	}
	return schemaDst, nil
}
