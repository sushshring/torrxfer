package client

import "time"

type File struct {
	Path         string
	MediaPrefix  string
	Size         uint64
	ModifiedTime time.Time
}
