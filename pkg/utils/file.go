package utils

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
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
		return fmt.Errorf("error while creating a directory path %v: %v", path, err)
	}
	return nil
}

func ErrNotIsSubfolder(base, specific string) error {
	var err error
	rel, err := filepath.Rel(base, specific)
	if err != nil {
		return err
	}
	if strings.HasPrefix(rel, "../") {
		err = fmt.Errorf("folder %q is not located within the defined base folder %q", specific, base)
	}
	return err
}
