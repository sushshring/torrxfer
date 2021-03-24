package common

import (
	"io/fs"
	"os"
	"path/filepath"
)

func IsSubdir(rootDir, subdir string) bool {
	pattern := rootDir + string(filepath.Separator) + "*"
	matched, err := filepath.Match(pattern, subdir)
	return err == nil && matched
}

type DirectoryDecoder struct {
	FsInfo   fs.FileInfo
	Filepath string
}

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

func CleanPath(path string) (absolutePath string, err error) {
	// Evaluate symlinks and clean filepath
	err = nil
	cleanedFilePath, err := filepath.EvalSymlinks(path)
	if err != nil {
		return "", err
	}

	// Generate absolute path
	absolutePath, err = filepath.Abs(cleanedFilePath)
	if err != nil {
		return "", err
	}

	absolutePath = filepath.FromSlash(absolutePath)
	return
}
