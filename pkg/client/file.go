package client

import (
	"errors"
	"fmt"
	"time"
)

const delimiter string = "*?*"

type File struct {
	Path         string
	MediaPrefix  string
	Size         uint64
	ModifiedTime time.Time
	WatchTime    time.Time
	TransferTime time.Time
}

func (f *File) getBytes() (bytes [][]byte) {
	bytes = make([][]byte, 0)
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
	bytes = append(bytes,
		[]byte(f.Path),
		[]byte(delimiter),
		[]byte(f.MediaPrefix),
		[]byte(delimiter),
		[]byte(fmt.Sprintf("%d", f.Size)),
		[]byte(delimiter),
		modifiedTime,
		[]byte(delimiter),
		watchTime,
		[]byte(delimiter),
		transferTime)
	return bytes
}

func (f *File) MarshalText() (text []byte, err error) {
	err = nil
	bytes := f.getBytes()
	if bytes == nil {
		err = errors.New("Failed to marshal file object")
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
	}
	return text, err
}

func (f *File) UnmarshalText(text []byte) error {
	var path, mediaPrefix, modifiedTime, watchTime, transferTime, d string
	var size int
	formatString := fmt.Sprintf("%%s %s %%s %s %%d %s %%s %s %%s %s %%s", delimiter, delimiter, delimiter, delimiter, delimiter)
	_, err := fmt.Sscanf(string(text), formatString, path, d, mediaPrefix, size, d, modifiedTime, d, watchTime, d, transferTime)
	if err != nil {
		return err
	}
	f.Path = path
	f.MediaPrefix = mediaPrefix
	f.Size = uint64(size)
	f.ModifiedTime = time.Time{}
	if err := f.ModifiedTime.UnmarshalText([]byte(modifiedTime)); err != nil {
		return err
	}
	f.WatchTime = time.Unix(0, 0)
	if err := f.WatchTime.UnmarshalText([]byte(watchTime)); err != nil {
		f.WatchTime = time.Unix(0, 0)
	}
	f.TransferTime = time.Unix(0, 0)
	if err := f.TransferTime.UnmarshalText([]byte(transferTime)); err != nil {
		f.TransferTime = time.Unix(0, 0)
	}
	return nil
}
