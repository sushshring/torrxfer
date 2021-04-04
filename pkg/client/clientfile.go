package client

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/rs/zerolog"
	"github.com/rs/zerolog/log"
	"github.com/sushshring/torrxfer/pkg/common"
	"github.com/sushshring/torrxfer/pkg/net"
)

const delimiter string = "*?*"

// File in the client package represents the clients view of a watched file
type File struct {
	Path         string
	MediaPrefix  string
	Size         uint64
	ModifiedTime time.Time
	WatchTime    time.Time
	TransferTime time.Time
}

// NewClientFile parses a preset media root and a path to string and outputs the client file representation for
// the file path. This function should be used to tell the Torrxfer server what part of the file path
// should be used to generate its directory structure and what path from the client should be discarded
// Example:
// If the provided path is /home/user/foo/bar/file.txt and the directory structure under foo should be
// maintained on the server, mediaDirectoryRoot should be /home/user/foo. This will tell the server that
// under its own media folder it should create the directory bar and store file.txt there.
func NewClientFile(path, mediaDirectoryRoot string) (*File, error) {
	mediaPrefix, err := generateMediaPrefix(mediaDirectoryRoot, path)
	if err != nil {
		log.Info().Err(err).Msg("Could not generate media prefix. Setting to watched directory")
		mediaPrefix = ""
	}
	absolutePath, err := common.CleanPath(path)
	if err != nil {
		// Not expected to error here since media prefix generation already cleaned path once
		log.Debug().Err(err).Msg("Clean path failed")
		return nil, err
	}

	stat, err := os.Stat(absolutePath)
	if err != nil {
		return nil, err
	}

	clientFile := &File{
		Path:         absolutePath,
		MediaPrefix:  mediaPrefix,
		Size:         uint64(stat.Size()),
		ModifiedTime: stat.ModTime(),
		WatchTime:    time.Unix(0, 0),
		TransferTime: time.Unix(0, 0),
	}
	return clientFile, nil
}

func generateMediaPrefix(mediaDirectoryRoot string, path string) (string, error) {
	absoluteMediaDirectory, err := common.CleanPath(mediaDirectoryRoot)
	if err != nil {
		return "", err
	}
	absoluteFilePath, err := common.CleanPath(path)
	if err != nil {
		return "", err
	}
	fileDir := filepath.Dir(absoluteFilePath)
	if !strings.HasPrefix(fileDir, absoluteMediaDirectory) {
		err := errors.New("media directory root is not part of the file path")
		return "", err
	}
	return strings.TrimPrefix(fileDir, absoluteMediaDirectory), nil
}

func (f *File) getStrings() (stringReprs []string) {
	stringReprs = make([]string, 0)
	modifiedTime, err := f.ModifiedTime.MarshalText()
	if err != nil {
		return nil
	}
	var watchTime []byte
	if f.WatchTime != time.Unix(0, 0) {
		watchTime, err = f.WatchTime.MarshalText()
		if err != nil {
			return nil
		}
	}
	var transferTime []byte
	if f.TransferTime != time.Unix(0, 0) {
		transferTime, err = f.WatchTime.MarshalText()
		if err != nil {
			return nil
		}
	}
	stringReprs = append(stringReprs,
		f.Path,
		delimiter,
		f.MediaPrefix,
		delimiter,
		fmt.Sprintf("%d", f.Size),
		delimiter,
		string(modifiedTime),
		delimiter,
		string(watchTime),
		delimiter,
		string(transferTime))
	return stringReprs
}

// MarshalText converts the clientFile representation to a utf encoded byte array
func (f *File) MarshalText() (text []byte, err error) {
	err = nil
	bytes := f.getStrings()
	if bytes == nil {
		err = errors.New("failed to marshal file object")
		return nil, err
	}
	var marshalSize int
	for _, i := range bytes {
		marshalSize += len(i)
	}
	text = make([]byte, marshalSize)
	currentCopy := 0
	for _, i := range bytes {
		copy(text[currentCopy:currentCopy+len(i)], i)
		currentCopy += len(i)
	}
	return text, err
}

// UnmarshalText takes a utf encoded byte array and builds a ClientFile object from it
func (f *File) UnmarshalText(text []byte) error {
	var modifiedTime, watchTime, transferTime string
	var size uint64
	textString := string(text)
	tokens := strings.Split(textString, delimiter)
	if len(tokens) != 6 {
		log.Panic().Strs("tokens", tokens).Msg("Error while unmarshalling")
	}

	f.Path = strings.TrimSpace(tokens[0])
	f.MediaPrefix = strings.TrimSpace(tokens[1])
	size, err := strconv.ParseUint(strings.TrimSpace(tokens[2]), 10, 64)
	if err != nil {
		return err
	}
	f.Size = size

	modifiedTime = tokens[4]
	f.ModifiedTime = time.Time{}
	if err := f.ModifiedTime.UnmarshalText([]byte(strings.TrimSpace(modifiedTime))); err != nil {
		return err
	}

	watchTime = tokens[5]
	f.WatchTime = time.Time{}
	if err := f.WatchTime.UnmarshalText([]byte(strings.TrimSpace(watchTime))); err != nil {
		log.Debug().Time("Watch time", f.TransferTime).Msg("Failed to parse time. Keeping default")
		f.WatchTime = time.Unix(0, 0)
	}

	transferTime = tokens[6]
	f.TransferTime = time.Time{}
	if err := f.TransferTime.UnmarshalText([]byte(strings.TrimSpace(transferTime))); err != nil {
		log.Debug().Time("Transfer time", f.TransferTime).Msg("Failed to parse time. Keeping default")
		f.TransferTime = time.Unix(0, 0)
	}
	return nil
}

// GenerateRPCFile creates an RPCFile representation from the current file
func (f *File) GenerateRPCFile() (*net.RPCFile, error) {
	rpcFile, err := net.NewFile(f.Path)
	if err != nil {
		return nil, err
	}
	rpcFile.SetMediaPath(f.MediaPrefix)
	return rpcFile, nil
}

// MarshalZerologObject adds the file details to the current zerolog event
func (f *File) MarshalZerologObject(e *zerolog.Event) {
	e.Str("Path", f.Path).Str("Media Path Prefix", f.MediaPrefix).Uint64("Size", f.Size).Time("Transfer started at", f.TransferTime)
}
