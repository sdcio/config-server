package schema

import (
	"context"
	"fmt"
	"net/url"
	"os"
	"path"
	"sync"

	"github.com/go-git/go-git/v5/plumbing/transport"
	logger "github.com/henderiw/logger/log"
	"github.com/otiai10/copy"
	"github.com/sdcio/config-server/pkg/git"
	"github.com/sdcio/config-server/pkg/utils"
	sdcpb "github.com/sdcio/sdc-protos/sdcpb"
)

type SchemaLoader struct {
	tmpDir             string
	schemaDir          string
	schemas            map[SchemaIDHash]*SchemaDefinition
	schemasMutex       *sync.Mutex
	schemaServerClient sdcpb.SchemaServerClient
	schemaUploader     SchemaServerConnector
}

func NewSchemaLoader(tmpDir string, schemaDir string, ssc sdcpb.SchemaServerClient, schemaUploader SchemaServerConnector) (*SchemaLoader, error) {
	var err error

	// create the temp dir if not existing
	if !utils.DirExists(tmpDir) {
		err = utils.CreateDirectory(tmpDir, 0766)
		if err != nil {
			return nil, err
		}
	}

	// create the schema dir if not existing
	if !utils.DirExists(schemaDir) {
		err = utils.CreateDirectory(schemaDir, 0766)
		if err != nil {
			return nil, err
		}
	}

	return &SchemaLoader{
		tmpDir:             tmpDir,
		schemaDir:          schemaDir,
		schemaServerClient: ssc,
		schemaUploader:     schemaUploader,
		schemas:            map[SchemaIDHash]*SchemaDefinition{},
		schemasMutex:       &sync.Mutex{},
	}, nil
}

func (s *SchemaLoader) SchemaExists(sid *SchemaID) bool {
	s.schemasMutex.Lock()
	defer s.schemasMutex.Unlock()
	_, exists := s.schemas[sid.Hash()]
	return exists
}

func (s *SchemaLoader) AddSchema(ctx context.Context, sd *SchemaDefinition) error {

	relSchemaDstPath := path.Join(sd.provider, sd.version)
	absSchemaDstPath := path.Join(s.schemaDir, sd.provider, sd.version)

	// if directory exists, clear it
	if utils.DirExists(absSchemaDstPath) {
		err := os.RemoveAll(absSchemaDstPath)
		if err != nil {
			return err
		}
	}

	// create directory
	err := utils.CreateDirectory(absSchemaDstPath, 0755)
	if err != nil {
		return err
	}

	// iterate over repositories
	for _, repoSpec := range sd.repositorySpecs {
		gitRepoSpec, err := git.NewRepoSpec(repoSpec.url)
		if err != nil {
			return err
		}

		repoPath := path.Join(s.tmpDir, repoSpec.url.Path)
		gitRepoSpec.SetLocalPath(repoPath)
		gitRepoSpec.SetGitRef(repoSpec.refKind, repoSpec.ref)

		// init gogit instance
		gogit := git.NewGoGit(gitRepoSpec)

		// perform clone
		err = gogit.Clone(ctx)
		if err != nil {
			return err
		}

		// copy data from repo to schema dir
		err = s.copyDirs(ctx, repoPath, absSchemaDstPath, repoSpec.dirs)
		if err != nil {
			return err
		}
	}

	// collect the LoadSpecs of all the repo definitions
	schemaOverallLoadSpec := NewSchemaLoadSpec(nil, nil, nil)
	for _, x := range sd.repositorySpecs {
		schemaOverallLoadSpec.Join(x.schemaLoadSpec)
	}

	// add the relative path portion based on the s.schemaDir
	schemaOverallLoadSpec.AddPrefixDir(relSchemaDstPath)

	// upload the schema to schema server
	err = s.schemaUploader.Upload(ctx, relSchemaDstPath, sd.SchemaID, schemaOverallLoadSpec)
	if err != nil {
		return err
	}

	s.schemasMutex.Lock()
	s.schemas[sd.Hash()] = sd
	s.schemasMutex.Unlock()

	return nil
}

func (s *SchemaLoader) copyDirs(ctx context.Context, repoPath string, schemaDstPath string, sdp []*SrcDstPath) error {

	log := logger.FromContext(ctx)

	for i, dir := range sdp {
		// build the source path
		src := path.Join(repoPath, dir.Src)
		// check path is still within the base schema folder
		// -> prevent escaping the folder
		err := utils.ErrNotIsSubfolder(repoPath, src)
		if err != nil {
			return err
		}
		// build dst path
		dst := path.Join(schemaDstPath, dir.Dst)
		// check path is still within the base schema folder
		// -> prevent escaping the folder
		err = utils.ErrNotIsSubfolder(schemaDstPath, dst)
		if err != nil {
			return err
		}

		log.Info("copying", "index", fmt.Sprintf("%d, %d", i+1, len(sdp)), "from", src, "to", dst)
		err = copy.Copy(src, dst)
		if err != nil {
			return err
		}
	}

	return nil
}

func (s *SchemaLoader) RemoveSchema(ctx context.Context, schema *SchemaID) error {
	_, err := s.schemaServerClient.DeleteSchema(ctx, &sdcpb.DeleteSchemaRequest{Schema: schema.ToSdcpbSchema()})
	return err
}

type SchemaDefinition struct {
	SchemaID
	repositorySpecs []*RepositorySpec
}

func NewSchemaDefinition(id *SchemaID) *SchemaDefinition {
	return &SchemaDefinition{
		SchemaID:        *id,
		repositorySpecs: []*RepositorySpec{},
	}
}

func (sd *SchemaDefinition) AddRepositorySpec(rs *RepositorySpec) {
	sd.repositorySpecs = append(sd.repositorySpecs, rs)
}

type RepositorySpec struct {
	url         *url.URL
	credentials transport.AuthMethod
	// Ref defines the branch or tag of the repository corresponding to the
	// provider schema version
	ref     string
	refKind git.GitRefKind
	// Dirs defines the list of directories that identified the provider schema in src/dst pairs
	// relative within the repository
	dirs           []*SrcDstPath
	schemaLoadSpec *SchemaLoadSpec
	proxy          *url.URL
}

func NewRepositorySpec(url *url.URL, refKind git.GitRefKind, ref string, schemaLoadSpec *SchemaLoadSpec, dirs []*SrcDstPath) *RepositorySpec {
	return &RepositorySpec{
		url:            url,
		ref:            ref,
		refKind:        refKind,
		dirs:           dirs,
		schemaLoadSpec: schemaLoadSpec,
	}
}

func (rs *RepositorySpec) SetProxy(proxyUrl *url.URL) {
	rs.proxy = proxyUrl
}

func (rs *RepositorySpec) SetCredentials(cred transport.AuthMethod) {
	rs.credentials = cred
}

// SrcDstPath provide a src/dst pair for the loader to download the schema from a specific src
// in the repository to a given destination in the schema server
type SrcDstPath struct {
	// Src is the relative directory in the repository URL
	Src string
	// Dst is the relative directory in the schema server
	Dst string
}

func NewSrcDstPath(src string, dst string) *SrcDstPath {
	return &SrcDstPath{
		Src: src,
		Dst: dst,
	}
}

type SchemaID struct {
	provider string
	version  string
}

func NewSchemaID(provider, version string) *SchemaID {
	return &SchemaID{
		provider: provider,
		version:  version,
	}
}

func (s *SchemaID) Hash() SchemaIDHash {
	return SchemaIDHash(fmt.Sprintf("%s|%s", s.provider, s.version))
}

func (s *SchemaID) ToSdcpbSchema() *sdcpb.Schema {
	return &sdcpb.Schema{
		Vendor:  s.provider,
		Version: s.version,
	}
}

type SchemaIDHash string

type SchemaLoadSpec struct {
	// Models defines the list of files/directories to be used as a model
	Models []string `json:"models,omitempty" yaml:"models,omitempty" protobuf:"bytes,1,rep,name=models"`
	// Excludes defines the list of files/directories to be excluded
	Includes []string `json:"includes,omitempty" yaml:"includes,omitempty" protobuf:"bytes,2,rep,name=includes"`
	// Excludes defines the list of files/directories to be excluded
	Excludes []string `json:"excludes,omitempty" yaml:"excludes,omitempty" protobuf:"bytes,3,rep,name=excludes"`
}

func NewSchemaLoadSpec(models, includes, excludes []string) *SchemaLoadSpec {
	return &SchemaLoadSpec{
		Models:   models,
		Includes: includes,
		Excludes: excludes,
	}
}

func (s *SchemaLoadSpec) Join(other *SchemaLoadSpec) {
	if other == nil {
		return
	}
	s.Models = append(s.Models, other.Models...)
	s.Includes = append(s.Includes, other.Includes...)
	s.Excludes = append(s.Excludes, other.Excludes...)
}

func (s *SchemaLoadSpec) AddPrefixDir(prefix string) {
	for idx, x := range s.Models {
		s.Models[idx] = path.Join(prefix, x)
	}
	for idx, x := range s.Includes {
		s.Includes[idx] = path.Join(prefix, x)
	}
}
