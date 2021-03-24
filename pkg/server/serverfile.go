package server

import (
	"errors"
	"fmt"
	"io"
	"strconv"
	"strings"
	"time"

	"github.com/juju/fslock"
	"github.com/rs/zerolog/log"
	"github.com/sushshring/torrxfer/pkg/net"
)

const delimiter string = "*?*"

type ServerFile struct {
	fullPath     string
	mediaPrefix  string
	size         uint64
	currentSize  uint64
	creationTime time.Time
	modifiedTime time.Time

	writeChannel *io.PipeWriter
	readChannel  io.Reader
	errorChannel chan error
	doneChannel  chan struct{}
	mux          fslock.Lock
}

func (f *ServerFile) getStrings() (stringReprs []string) {
	stringReprs = make([]string, 0)
	var creationTime []byte
	var err error
	if f.creationTime != time.Unix(0, 0) {
		creationTime, err = f.creationTime.MarshalText()
		if err != nil {
			return nil
		}
	}
	modifiedTime, err := f.modifiedTime.MarshalText()
	if err != nil {
		return nil
	}
	stringReprs = append(stringReprs,
		f.fullPath,
		delimiter,
		f.mediaPrefix,
		delimiter,
		fmt.Sprintf("%d", f.size),
		delimiter,
		fmt.Sprintf("%d", f.currentSize),
		delimiter,
		string(creationTime),
		delimiter,
		string(modifiedTime))
	return stringReprs
}

// MarshalText converts the clientFile representation to a utf encoded byte array
func (f *ServerFile) MarshalText() (text []byte, err error) {
	err = nil
	stringRepr := f.getStrings()
	if stringRepr == nil {
		err = errors.New("Failed to marshal file object")
		return nil, err
	}
	var marshalSize int
	for _, i := range stringRepr {
		marshalSize += len(i)
	}
	text = make([]byte, marshalSize)
	currentCopy := 0
	for _, i := range stringRepr {
		copy(text[currentCopy:currentCopy+len(i)], i)
		currentCopy += len(i)
	}
	return text, err
}

// UnmarshalText takes a utf encoded byte array and builds a ClientFile object from it
func (f *ServerFile) UnmarshalText(text []byte) error {
	var modifiedTime, creationTime string
	var size, currentSize uint64
	textString := string(text)
	tokens := strings.Split(textString, delimiter)
	if len(tokens) != 6 {
		err := errors.New("Not enough tokens in provided text")
		log.Error().Strs("tokens", tokens).Msg("Error while unmarshalling")
		return err
	}
	f.fullPath = strings.TrimSpace(tokens[0])
	f.mediaPrefix = strings.TrimSpace(tokens[1])
	size, err := strconv.ParseUint(strings.TrimSpace(tokens[2]), 10, 64)
	if err != nil {
		return err
	}
	f.size = size

	currentSize, err = strconv.ParseUint(strings.TrimSpace(tokens[3]), 10, 64)
	if err != nil {
		return err
	}
	f.currentSize = currentSize
	modifiedTime = tokens[4]
	creationTime = tokens[5]
	f.modifiedTime = time.Time{}
	if err := f.modifiedTime.UnmarshalText([]byte(strings.TrimSpace(modifiedTime))); err != nil {
		return err
	}
	f.creationTime = time.Time{}
	if err := f.creationTime.UnmarshalText([]byte(strings.TrimSpace(creationTime))); err != nil {
		return err
	}
	return nil
}

func (f *ServerFile) GenerateRpcFile() (*net.RPCFile, error) {
	rpcFile, err := net.NewFile(f.fullPath)
	if err != nil {
		return nil, err
	}
	rpcFile.SetMediaPath(f.mediaPrefix)
	return rpcFile, nil
}
