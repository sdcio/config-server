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

package utils

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	sdcerrors "github.com/sdcio/config-server/pkg/errors"
)

// FileExists returns true if a file referenced by filename exists & accessible.
func FileExists(filename string) bool {
	f, err := os.Stat(filename)
	if err != nil {
		return false
	}
	return !f.IsDir()
}

// FileOrDirExists returns true if a file or dir referenced by path exists & accessible.
func FileOrDirExists(filename string) bool {
	f, err := os.Stat(filename)

	return err == nil && f != nil
}

// DirExists returns true if a dir referenced by path exists & accessible.
func DirExists(filename string) bool {
	f, err := os.Stat(filename)

	return err == nil && f != nil && f.IsDir()
}

// CreateDirectory creates a directory by a path with a mode/permission specified by perm.
// If directory exists, the function does not do anything.
func CreateDirectory(path string, perm os.FileMode) error {
	err := os.MkdirAll(path, perm)
	if err != nil {
		return &sdcerrors.UnrecoverableError{Message: fmt.Sprintf("cannot create directory path %s", path), WrappedError: err}
	}
	return nil
}

func ErrNotIsSubfolder(base, specific string) error {
	var err error
	rel, err := filepath.Rel(base, specific)
	if err != nil {
		return &sdcerrors.UnrecoverableError{Message: fmt.Sprintf("cannot create relative path from base %s and specific %s", base, specific), WrappedError: err}
	}
	if strings.HasPrefix(rel, "../") {
		return &sdcerrors.UnrecoverableError{Message: fmt.Sprintf("folder %q is not located within the defined base folder %q", specific, base)}
	}
	return err
}

func RemoveDirectory(path string) error {
	if err := os.RemoveAll(path); err != nil {
		return &sdcerrors.UnrecoverableError{Message: fmt.Sprintf("cannot delete directory path %s", path), WrappedError: err}
	}
	return nil
}
