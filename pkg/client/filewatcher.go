package client

import (
	"sync"
	"time"

	"github.com/radovskyb/watcher"
	"github.com/rs/zerolog/log"
	"github.com/sushshring/torrxfer/internal/db"
)

const (
	clientFileDbName string = "cfdb.dat"
)

// How long to wait after a write to issue a notification.
// Variable instead of const to override during test
var writeDuration time.Duration = 2 * time.Minute

type FileWatcher struct {
	db                        *db.KvDb
	watchedDirectory          string
	channel                   chan File
	watchedFileTransferTimers map[string]*time.Timer
	w                         *watcher.Watcher
	mux                       *sync.Mutex
}

// NewFileWatcher starts watching the provided directory for any new writes and here-before unseen files
// If there is a new file, this will wait up to two minutes for any new writes, at which point it will
func NewFileWatcher(directory string) (*FileWatcher, error) {
	filewatcher := new(FileWatcher)
	innerdb, err := db.GetDb(clientFileDbName)
	if err != nil {
		log.Debug().Stack().Err(err).Msg("Failed to init db")
		return nil, err
	}
	filewatcher.db = innerdb
	filewatcher.mux = &sync.Mutex{}
	filewatcher.watchedDirectory = directory
	filewatcher.watchedFileTransferTimers = make(map[string]*time.Timer)
	// Run file watch logic thread
	go func() {
		filewatcher.channel = make(chan File, 10)
		defer close(filewatcher.channel)
		filewatcher.watcherThread()
	}()
	return filewatcher, nil
}

func (filewatcher *FileWatcher) RegisterForFileNotifications() <-chan File {
	return filewatcher.channel
}

func (filewatcher *FileWatcher) RemoveWatchedFile(file string) {
	if err := filewatcher.db.Delete(file); err != nil {
		log.Debug().Err(err).Msg("Could not remove watched file from DB")
	}
}

func (filewatcher *FileWatcher) Close() {
	{
		filewatcher.mux.Lock()
		defer filewatcher.mux.Unlock()
		for _, timer := range filewatcher.watchedFileTransferTimers {
			timer.Stop()
		}
	}
	// Wait for one second to send any queued file objects
	time.Sleep(time.Second)
	filewatcher.w.Close()
}

func (filewatcher *FileWatcher) watcherThread() {
	filewatcher.w = watcher.New()
	filewatcher.w.FilterOps(watcher.Write)
	if err := filewatcher.w.AddRecursive(filewatcher.watchedDirectory); err != nil {
		log.Debug().Stack().Err(err).Msg("Could not add directory to watcher")
		return
	}
	for path, f := range filewatcher.w.WatchedFiles() {
		// Try to transfer all watched files
		// Files that were watched previously will be skipped until they are written to
		filewatcher.handleFileEvent(path, f.Size(), f.ModTime())
	}
	if err := filewatcher.w.Start(time.Millisecond * 100); err != nil {
		log.Debug().Stack().Err(err).Msg("Could not start file watcher")
		return
	}
	for {
		select {
		case event := <-filewatcher.w.Event:
			filewatcher.handleFileEvent(event.Path, event.Size(), event.ModTime())
		case <-filewatcher.w.Closed:
			log.Info().Msg("File watcher closed")
			return
		case err := <-filewatcher.w.Error:
			// Close and delete filewatcher
			log.Info().Err(err).Msg("File watcher closed")
			return
		}
	}
}

func (filewatcher *FileWatcher) handleFileEvent(path string, size int64, modTime time.Time) {
	if !filewatcher.db.Has(path) {
		// Add the file to the database
		file := File{
			Path:         path,
			MediaPrefix:  "", // TODO: figure out the mechanism to create the media prefix
			Size:         uint64(size),
			ModifiedTime: modTime,
			WatchTime:    time.Now(),
			TransferTime: time.Unix(0, 0),
		}
		fileAsText, err := file.MarshalText()
		if err != nil {
			// Fatal error here since the file object is made from existing files, a failure here should crash the application
			log.Fatal().Stack().Err(err).Msg("Marshalling file object for db failed")
		}
		filewatcher.db.Put(path, string(fileAsText))

		// Schedule a transfer
		filewatcher.mux.Lock()
		defer filewatcher.mux.Unlock()
		filewatcher.watchedFileTransferTimers[path] = time.NewTimer(writeDuration)
		go func(filepath string) {
			<-filewatcher.watchedFileTransferTimers[filepath].C
			filewatcher.channel <- file
			filewatcher.mux.Lock()
			defer filewatcher.mux.Unlock()
			delete(filewatcher.watchedFileTransferTimers, filepath)
		}(path)
	} else {
		// Cancel the previously scheduled write and schedule another write
		filewatcher.mux.Lock()
		defer filewatcher.mux.Unlock()
		if timer, ok := filewatcher.watchedFileTransferTimers[path]; ok {
			timer.Reset(writeDuration)
		}
	}
}
