package net

import (
	"errors"
	"os"
	"path/filepath"
	"time"

	"github.com/rs/zerolog/log"
	"github.com/sushshring/torrxfer/pkg/common"
	"github.com/sushshring/torrxfer/pkg/crypto"
	pb "github.com/sushshring/torrxfer/rpc"
)

// RPCFile wraps around the gRPC RPCFile type.
// This file structure should be used by both client and server during the transfer
type RPCFile struct {
	file *pb.File
}

// NewFile constructs a new file object that wraps around the gRPC struct
// This function can be called on files that don't exist
func NewFile(filePath string) (*RPCFile, error) {
	file := new(RPCFile)
	// Get file name
	fileName := filepath.Base(filePath)

	// TODO: Add performance evaluation here for larger files
	var hash string = ""
	var modtime time.Time
	var size uint64
	stat, err := os.Stat(filePath)
	if err == nil {
		hash, err = crypto.HashFile(filePath)
		if err != nil {
			common.AddLogger(os.Stderr, false)
			log.Fatal().Err(err).Msg("Failed to hash file")
			return nil, err
		}
		modtime = stat.ModTime()
		size = uint64(stat.Size())
	} else if os.IsNotExist(err) {
		modtime = time.Unix(0, 0)
		size = 0
	}

	file.file = &pb.File{
		Name:           fileName,
		DataHash:       hash,
		MediaDirectory: "",
		CreatedTime:    uint64(modtime.Unix()),
		ModifiedTime:   uint64(modtime.Unix()),
		Size:           size,
	}
	return file, nil
}

// NewFileFromGrpc returns a RPCFile from a gRPC wire file object
func NewFileFromGrpc(grpcFile *pb.File) *RPCFile {
	return &RPCFile{grpcFile}
}

// SetMediaPath sets the media path to convey the folder structure on the server side
func (f *RPCFile) SetMediaPath(mediaDirectory string) error {
	if f.file == nil {
		err := errors.New("No grpc file. Construct with NewFile")
		log.Fatal().Stack().Err(err).Msg("")
		return err
	}
	f.file.MediaDirectory = mediaDirectory
	return nil
}

// GetFileName file name
func (f *RPCFile) GetFileName() string {
	return f.file.Name
}

// GetSize size
func (f *RPCFile) GetSize() uint64 {
	return f.file.Size
}

// GetRemoteSize returns the size of the file on the server
func (f *RPCFile) GetRemoteSize() uint64 {
	return f.file.SizeOnDisk
}

// GetDataHash data hash
func (f *RPCFile) GetDataHash() string {
	return f.file.DataHash
}

// GetCreationTime creation time
func (f *RPCFile) GetCreationTime() time.Time {
	return time.Unix(int64(f.file.CreatedTime), 0)
}

// GetModifiedTime modified time
func (f *RPCFile) GetModifiedTime() time.Time {
	return time.Unix(int64(f.file.ModifiedTime), 0)
}

// GetMediaPath media root directory
func (f *RPCFile) GetMediaPath() string {
	return f.file.MediaDirectory
}
