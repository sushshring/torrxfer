package common

import (
	"io/fs"
	"os"
	"path/filepath"
)

// IsSubdir returns true if subDir is a sub directory of rootdir
func IsSubdir(rootDir, subdir string) bool {
	pattern := filepath.Clean(rootDir) + string(filepath.Separator) + "*"
	matched, err := filepath.Match(pattern, subdir)
	return err == nil && matched
}

// DirectoryDecoder decodes a directory from json
type DirectoryDecoder struct {
	FsInfo   fs.FileInfo
	Filepath string
}

// Decode take a file name and uses it as the filepath
func (d *DirectoryDecoder) Decode(value string) error {
	fileInfo, err := os.Stat(value)
	if err != nil {
		return err
	}
	d.FsInfo = fileInfo
	cleanedPath, err := CleanPath(value)
	if err != nil {
		return err
	}
	d.Filepath = cleanedPath
	return nil
}

// CleanPath takes a directory path and evaluates symlinks, takes the absolute path, and adds path separators based on the OS
func CleanPath(path string) (absolutePath string, err error) {
	// Evaluate symlinks and clean filepath
	err = nil
	var cleanedFilePath string
	cleanedFilePath, err = filepath.EvalSymlinks(path)
	if err != nil {
		perr, ok := err.(*fs.PathError)
		if !ok {
			return "", err
		}
		cleanedFilePath = perr.Path
	}

	// Generate absolute path
	absolutePath, err = filepath.Abs(cleanedFilePath)
	if err != nil {
		return "", err
	}

	absolutePath = filepath.FromSlash(absolutePath)
	return
}
