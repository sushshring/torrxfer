package net

import (
	"errors"
	"os"
	"path/filepath"
	"strings"

	log "github.com/sirupsen/logrus"
	"github.com/sushshring/torrxfer/pkg/crypto"
	pb "github.com/sushshring/torrxfer/rpc"
)

// RPCFile wraps around the gRPC RPCFile type
type RPCFile struct {
	file *pb.File
}

// NewFile constructs a new file object that wraps around the gRPC struct
func NewFile(filePath string) (*RPCFile, error) {
	file := new(RPCFile)

	// Evaluate symlinks and clean filepath
	cleanedFilePath, err := filepath.EvalSymlinks(filePath)
	if err != nil {
		return nil, err
	}

	// Generate absolute path
	absolutePath, err := filepath.Abs(cleanedFilePath)
	if err != nil {
		return nil, err
	}

	// Get file name
	fileName := filepath.Base(absolutePath)

	hash, err := crypto.HashFile(absolutePath)

	if err != nil {
		log.Fatal(err)
		return nil, err
	}
	stat, err := os.Stat(absolutePath)
	if err != nil {
		log.Error(err)
		return nil, err
	}
	modifiedTime := new(uint64)
	*modifiedTime = uint64(stat.ModTime().Unix())
	size := new(uint64)
	*size = uint64(stat.Size())

	file.file = &pb.File{
		Name:           fileName,
		FullPath:       absolutePath,
		DataHash:       hash,
		MediaDirectory: nil,
		CreatedTime:    nil,
		ModifiedTime:   modifiedTime,
		Size:           size,
	}
	return file, nil
}

// SetMediaPath sets the media path to convey the folder structure on the server side
func (f *RPCFile) SetMediaPath(mediaDirectory *string) error {
	if f.file == nil {
		err := errors.New("No grpc file. Construct with NewFile")
		log.Fatal(err)
		return err
	}
	if mediaDirectory == nil {
		err := errors.New("No media directory provided")
		log.Fatal(err)
		return err
	}
	fullPath := f.file.GetFullPath()
	if !strings.Contains(fullPath, *mediaDirectory) {
		err := errors.New("Media directory not part of full path")
		log.Fatal(err)
		return err
	}
	f.file.MediaDirectory = mediaDirectory
	return nil
}
