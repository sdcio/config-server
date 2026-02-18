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

package main // nolint:revive

import (
	"fmt"
	"io/fs"
	"log"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/spf13/cobra"
	"golang.org/x/mod/modfile"
)

var bin string

// only needed still for protobuf
var output string

var cmd = cobra.Command{
	Use:     "apiserver-runtime-gen",
	Short:   "run code generators",
	PreRunE: preRunE,
	RunE:    runE,
}

func preRunE(cmd *cobra.Command, args []string) error {
	if module == "" {
		return fmt.Errorf("must specify module")
	}
	// we use the default versions now so the version input is derived from that
	if len(versions) == 0 {
		return fmt.Errorf("must specify versions")
	}
	return nil
}

func runE(cmd *cobra.Command, args []string) error {
	// get the location the generators are installed
	bin = os.Getenv("GOBIN")
	if bin == "" {
		bin = filepath.Join(os.Getenv("HOME"), "go", "bin")
	}
	// install the generators
	if install {
		for _, gen := range generators {
			// nolint:gosec
			if gen == "openapi-gen" {
				err := run(exec.Command("go", "install", "k8s.io/kube-openapi/cmd/openapi-gen@latest"))
				if err != nil {
					return err
				}
			} else {
				err := run(exec.Command("go", "install", path.Join("k8s.io/code-generator/cmd", gen)))
				if err != nil {
					return err
				}
			}
			if gen == "go-to-protobuf" {
				err := run(exec.Command("go", "mod", "vendor"))
				if err != nil {
					return err
				}
				err = run(exec.Command("go", "mod", "tidy"))
				if err != nil {
					return err
				}
				// setup the directory to generate the code to.
				// code generators don't work with go modules, and try the full path of the module
				output, err = os.MkdirTemp("", "gen")
				if err != nil {
					return err
				}
				if clean {
					// nolint:errcheck
					defer os.RemoveAll(output)
				}
				d, l := path.Split(module)                   // split the directory from the link we will create
				p := filepath.Join(strings.Split(d, "/")...) // convert module path to os filepath
				p = filepath.Join(output, p)                 // create the directory which will contain the link
				err = os.MkdirAll(p, 0700)
				if err != nil {
					return err
				}
				// link the tmp location to this one so the code generator writes to the correct path
				wd, err := os.Getwd()
				if err != nil {
					return err
				}
				err = os.Symlink(wd, filepath.Join(p, l))
				if err != nil {
					return err
				}
			}
		}
	}

	return doGen()
}

func doGen() error {
	// exclude condition from versions for clientgen, listergen, informergen
	// clientgen
	clientgenVersions := []string{}
	informerListergenVersions := []string{}
	protobufVersions := []string{}
	typeVersions := []string{}
	for _, version := range versions {
		if !strings.Contains(version, "condition") {
			// expand the path with the module
			clientgenVersions = append(clientgenVersions, path.Join(module, version))
			// dont expand the versions with modules
			informerListergenVersions = append(informerListergenVersions, fmt.Sprintf("./%s", path.Join(version, "...")))
		}
		protobufVersions = append(protobufVersions, path.Join(module, version))
		typeVersions = append(typeVersions, path.Join(module, version))
	}

	//protoInputs := strings.Join(rawVersions, ",")

	gen := map[string]bool{}
	for _, g := range generators {
		gen[g] = true
	}

	if gen["deepcopy-gen"] {
		err := run(getCmd("deepcopy-gen",
			"--output-file", "zz_generated.deepcopy.go",
			"./apis/..."))
		if err != nil {
			return err
		}
	}

	if gen["openapi-gen"] {
		// HACK had to use openapi-gen and use go mod v1.23 iso go.mod v1.20
		cmdArgs := []string{
			"--output-dir", "pkg/generated/openapi",
			"--output-pkg", "openapi",
			"--output-file", "zz_generated.openapi.go",
			"k8s.io/apimachinery/pkg/apis/meta/v1",
			"k8s.io/api/core/v1",
			"k8s.io/apimachinery/pkg/api/resource",
			"k8s.io/apimachinery/pkg/runtime",
			"k8s.io/apimachinery/pkg/version",
		}
		cmdArgs = append(cmdArgs, typeVersions...)
		err := run(getCmdSimple("bin/openapi-gen", cmdArgs...))

		if err != nil {
			return err
		}
	}

	if gen["client-gen"] {
		err := run(getCmd(
			"client-gen",
			"--clientset-name", "versioned",
			"--input-base", "",
			"--input", strings.Join(clientgenVersions, ","),
			"--output-dir", "pkg/generated/clientset",
			"--output-pkg", fmt.Sprintf("%s/pkg/generated/clientset", module),
		))
		if err != nil {
			return err
		}
	}

	listerGenStringer := []string{
		"--output-dir", "pkg/generated/listers",
		"--output-pkg", fmt.Sprintf("%s/pkg/generated/listers", module),
	}
	listerGenStringer = append(listerGenStringer, informerListergenVersions...)
	if gen["lister-gen"] {
		if err := run(getCmd("lister-gen", listerGenStringer...)); err != nil {
			return err
		}
	}

	informerGenStringer := []string{
		"--versioned-clientset-package", fmt.Sprintf("%s/pkg/generated/clientset/versioned", module),
		"--internal-clientset-package", fmt.Sprintf("%s/pkg/generated/clientset/versioned", module),
		"--listers-package", fmt.Sprintf("%s/pkg/generated/listers", module),
		"--output-dir", "pkg/generated/informers",
		"--output-pkg", fmt.Sprintf("%s/pkg/generated/informers", module),
	}
	informerGenStringer = append(informerGenStringer, informerListergenVersions...)
	if gen["informer-gen"] {
		if err := run(getCmd("informer-gen", informerGenStringer...)); err != nil {
			return err
		}
	}

	if gen["defaulter-gen"] {
		defaultArgs := []string{"--output-file", "zz_generated.defaults.go"}
		defaultArgs = append(defaultArgs, clientgenVersions...)
		err := run(getCmd("defaulter-gen",
			defaultArgs...,
		))
		if err != nil {
			return err
		}
	}

	if gen["conversion-gen"] {
		err := run(getCmd("conversion-gen",
			"--output-file", "zz_generated.conversion.go",
			"./apis/...",
		))
		if err != nil {
			return err
		}
	}

	if gen["go-to-protobuf"] {

		err := run(getProtoCmd(
			"go-to-protobuf",
			"--packages", strings.Join(protobufVersions, ","),
			"--apimachinery-packages", "-k8s.io/apimachinery/pkg/api/resource,-k8s.io/apimachinery/pkg/runtime/schema,-k8s.io/apimachinery/pkg/runtime,-k8s.io/apimachinery/pkg/apis/meta/v1",
			"--proto-import", "./vendor",
		))
		if err != nil {
			return err
		}
	}

	if gen["applyconfiguration-gen"] {
		applyArgs := []string{
			"--output-dir", "pkg/generated/applyconfiguration",
			"--output-pkg", fmt.Sprintf("%s/pkg/generated/applyconfiguration", module),
		}
		applyArgs = append(applyArgs, clientgenVersions...)
		err := run(getCmd("applyconfiguration-gen", applyArgs...))
		if err != nil {
			return err
		}
	}

	return nil
}

var (
	generators     []string
	header         string
	module         string
	versions       []string
	//rawVersions    []string
	clean, install bool
)

func main() {
	cmd.Flags().BoolVar(&clean, "clean", true, "Delete temporary directory for code generation.")

	options := []string{"client-gen", "deepcopy-gen", "informer-gen", "lister-gen", "openapi-gen", "go-to-protobuf", "applyconfiguration-gen"}
	defaultGen := []string{"deepcopy-gen", "openapi-gen"}
	cmd.Flags().StringSliceVarP(&generators, "generator", "g",
		defaultGen, fmt.Sprintf("Code generator to install and run.  Options: %v.", options))
	defaultBoilerplate := filepath.Join("hack", "boilerplate.go.txt")
	cmd.Flags().StringVar(&header, "go-header-file", defaultBoilerplate,
		"File containing boilerplate header text. The string YEAR will be replaced with the current 4-digit year.")
	cmd.Flags().BoolVar(&install, "install-generators", true, "Go get the generators")

	var defaultModule string
	cwd, _ := os.Getwd()
	if modRoot := findModuleRoot(cwd); modRoot != "" {
		if b, err := os.ReadFile(filepath.Clean(path.Join(modRoot, "go.mod"))); err == nil {
			defaultModule = modfile.ModulePath(b)
		}
	}
	cmd.Flags().StringVar(&module, "module", defaultModule, "Go module of the apiserver.")

	// calculate the versions
	var defaultVersions []string
	if err := fs.WalkDir(os.DirFS("apis"), ".", func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			if strings.HasPrefix(p, "generated") { // skip the generated directory
				return nil
			}
			match := versionRegexp.MatchString(filepath.Base(p))
			if match {
				defaultVersions = append(defaultVersions, path.Join("apis", p))
			}

		}
		return nil
	}); err != nil {
		panic(err)
	}

	cmd.Flags().StringSliceVar(
		&versions, "versions", defaultVersions, "Go packages of API versions to generate code for.")
	if err := cmd.Execute(); err != nil {
		log.Fatal(err)
	}
}

var versionRegexp = regexp.MustCompile("^v[0-9]+((alpha|beta)?[0-9]+)?$")

func run(cmd *exec.Cmd) error {
	fmt.Println(strings.Join(cmd.Args, " "))
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	cmd.Stdin = os.Stdin
	return cmd.Run()
}

func getCmd(cmd string, args ...string) *exec.Cmd {
	// nolint:gosec
	e := exec.Command(filepath.Join(bin, cmd), "--go-header-file", header)

	e.Args = append(e.Args, args...)
	return e
}

func getProtoCmd(cmd string, args ...string) *exec.Cmd {
	// nolint:gosec
	e := exec.Command(filepath.Join(bin, cmd), "--output-dir", output, "--go-header-file", header)

	e.Args = append(e.Args, args...)
	return e
}

func getCmdSimple(cmd string, args ...string) *exec.Cmd {
	// nolint:gosec
	e := exec.Command(cmd, "--go-header-file", header)

	e.Args = append(e.Args, args...)
	return e
}

func findModuleRoot(dir string) string {
	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir
		}

		parentDIR := path.Dir(dir)
		if parentDIR == dir {
			break
		}
		dir = parentDIR
	}
	return ""
}
