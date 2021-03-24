package client

import (
	"errors"
	"os"
	"sync"
	"time"

	"github.com/radovskyb/watcher"
	"github.com/rs/zerolog/log"
	"github.com/sushshring/torrxfer/internal/db"
	"github.com/sushshring/torrxfer/pkg/common"
)

const (
	clientFileDbName string = "cfdb.dat"
)

// How long to wait after a write to issue a notification.
// Variable instead of const to override during test
var writeDuration time.Duration = 2 * time.Second

// FileWatcher provides notifications when changes occur on the provided watched directory
type FileWatcher interface {
	RegisterForFileNotifications() <-chan ClientFile
	RemoveWatchedFile(file string)
	Close()
}

type fileWatcher struct {
	db                        db.KvDB
	watchedDirectory          string
	channel                   chan ClientFile
	watchedFileTransferTimers map[string]*time.Timer
	w                         *watcher.Watcher
	mediaDirectoryRoot        string
	sync.RWMutex
}

// NewFileWatcher starts watching the provided directory for any new writes and here-before unseen files
// If there is a new file, this will wait up to two minutes for any new writes, at which point it will
func NewFileWatcher(directory string, mediaDirectoryRoot string) (FileWatcher, error) {
	log.Debug().Msg("Creating file watcher")

	// Verify media directory root is valid
	if !common.IsSubdir(mediaDirectoryRoot, directory) {
		return nil, errors.New("Invalid media directory root")
	}
	innerdb, err := db.GetDb(clientFileDbName)
	if err != nil {
		log.Debug().Stack().Err(err).Msg("Failed to init db")
		return nil, err
	}
	filewatcher := &fileWatcher{
		innerdb,
		directory,
		make(chan ClientFile, 10),
		make(map[string]*time.Timer),
		nil,
		mediaDirectoryRoot,
		sync.RWMutex{}}
	// Run file watch logic thread
	go func() {
		defer close(filewatcher.channel)
		filewatcher.watcherThread()
		// Once filewatcher closes either due to error or the watcher being forcibly closed
		// this returns and closes the filewatcher channel. Any listeners will then return as well
	}()
	return filewatcher, nil
}

// RegisterForFileNotifications retuns a channel that responds with File objects
// for any new file write or update. On init, it will also send a notification for all
// the files in the folder regardless of their presence in the database
func (filewatcher *fileWatcher) RegisterForFileNotifications() <-chan ClientFile {
	return filewatcher.channel
}

// RemoveWatchedFile removes a file from the database, allowing it to be queued for a resend
// Note that even if a file is removed from the database, this does not affect the server's status.
// If it responds that a file is fully transferred already, it will not transfer the file regardless
func (filewatcher *fileWatcher) RemoveWatchedFile(file string) {
	if err := filewatcher.db.Delete(file); err != nil {
		log.Debug().Err(err).Msg("Could not remove watched file from DB")
	}
}

// Close shuts down a file watcher. All pending transfers are flushed and channels are all closed
func (filewatcher *fileWatcher) Close() {
	func() {
		filewatcher.RLock()
		defer filewatcher.RUnlock()
		for _, timer := range filewatcher.watchedFileTransferTimers {
			timer.Stop()
		}
	}()
	// Wait for one second to send any queued file objects
	time.Sleep(time.Second)
	filewatcher.w.Close()
	// At filewatcher.w close, this causes watcherThread to exit
}

func (filewatcher *fileWatcher) watcherThread() {
	var err error
	log.Debug().Msg("Starting watch thread in few seconds")
	time.Sleep(time.Second * 5)
	filewatcher.w = watcher.New()
	filewatcher.w.IgnoreHiddenFiles(true)
	filewatcher.w.FilterOps(watcher.Write, watcher.Create, watcher.Chmod)
	if err := filewatcher.w.AddRecursive(filewatcher.watchedDirectory); err != nil {
		log.Debug().Stack().Err(err).Msg("Could not add directory to watcher")
		return
	}
	// On system initialization, sends a notification for all watched files.
	// This handles a case where a previous file was modified
	watchedFiles := filewatcher.w.WatchedFiles()
	for path, f := range watchedFiles {
		if f.IsDir() {
			continue
		}
		log.Debug().Str("File name", path).Msg("Found file")

		var clientFile *ClientFile
		// Try to transfer all watched files
		if filewatcher.db.Has(path) {
			// Since DB already has the file, mark this as a potential write to existing file event.
			log.Debug().Str("File path", path).Msg("Already started transferring file. Query server for incomplete send")
			_, err := filewatcher.db.Get(path)
			if err != nil {
				// Could not get file details. Delete the data from the DB and let the file event
				// handler generate metadata for the file from scratch. This is more expensive, but keeps the server alive
				log.Debug().Err(err).Msg("Failed to retrieve prior send file data to db. Removing and setting transfer timer")
				// Best effort delete
				filewatcher.db.Delete(path)
				// Treat this as a new file event
				filewatcher.handleFileEvent(path, f.Size(), f.ModTime())
				continue
			}
			clientFile, err = NewClientFile(path, filewatcher.mediaDirectoryRoot)
			func() {
				// Schedule a transfer
				filewatcher.Lock()
				defer filewatcher.Unlock()
				filewatcher.watchedFileTransferTimers[clientFile.Path] = time.NewTimer(writeDuration)
			}()
			go filewatcher.transferFile(clientFile)
		} else {
			// New file that was created without the file watcher running
			log.Debug().Str("File path", path).Msg("Brand new file. Starting transfer")
			clientFile, err = NewClientFile(path, filewatcher.mediaDirectoryRoot)
			if err != nil {
				log.Debug().Err(err).Msg("Could not generate file representation")
				return
			}
			// Treat it as a file created event
			filewatcher.handleFileEvent(path, int64(clientFile.Size), clientFile.ModifiedTime)
		}
	}

	// Handle watcher events
	go func() {
		for {
			select {
			case event := <-filewatcher.w.Event:
				log.Debug().Str("File", event.Path).Msg("Got new file write")
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
	}()
	if err := filewatcher.w.Start(time.Millisecond * 100); err != nil {
		log.Debug().Stack().Err(err).Msg("Could not start file watcher")
		return
	}
}

// this method depends on the watch timer for file writes. Since it is a 1 buffered channel,
// this method is not reentrant
// Handles file create and write events. If a file is already watched, the timer is reset to wait for the next write
// This assumes that the thread to transfer file was already kicked off when the file was created. In case there
// is write to a previously watched file, the event will be ignoreds
// This function is also not meant to handle file send on system initialization.
func (filewatcher *fileWatcher) handleFileEvent(path string, size int64, modTime time.Time) {
	log.Debug().Str("File", path).Int64("Size", size).Time("Modified at", modTime).Msg("Handling file. ")
	stat, err := os.Stat(path)
	if err != nil {
		log.Debug().Err(err).Msg("Could not stat file details. Skipping")
		return
	}
	if stat.IsDir() {
		// Skip directories
		return
	}
	if !filewatcher.db.Has(path) {
		// Add the file to the database
		file, err := NewClientFile(path, filewatcher.mediaDirectoryRoot)
		if err != nil {
			log.Debug().Err(err).Msg("Could not generate file representation")
			return
		}
		file.WatchTime = time.Now()
		file.TransferTime = time.Unix(0, 0)
		fileAsText, err := file.MarshalText()
		if err != nil {
			// Fatal error here since the file object is made from existing files, a failure here should crash the application
			log.Fatal().Stack().Err(err).Msg("Marshalling file object for db failed")
		}
		filewatcher.db.Put(file.Path, string(fileAsText))

		// Schedule a transfer
		filewatcher.Lock()
		defer filewatcher.Unlock()
		filewatcher.watchedFileTransferTimers[file.Path] = time.NewTimer(writeDuration)

		// Wait for the scheduled transfer on a new goroutine and emit the file update
		go filewatcher.transferFile(file)
	} else {
		// Cancel the previously scheduled write and schedule another write
		filewatcher.Lock()
		defer filewatcher.Unlock()
		if timer, ok := filewatcher.watchedFileTransferTimers[path]; ok {
			timer.Reset(writeDuration)
		}
	}
}

func (filewatcher *fileWatcher) transferFile(file *ClientFile) {
	log.Debug().Str("File path", file.Path).Msg("Starting transfer after waiting for write timer")
	filewatcher.Lock()
	defer filewatcher.Unlock()

	// Wait for write timer to get called
	<-filewatcher.watchedFileTransferTimers[file.Path].C
	log.Debug().Str("File path", file.Path).Msg("Starting file transfer after waiting for write")
	filewatcher.channel <- *file
	delete(filewatcher.watchedFileTransferTimers, file.Path)
}
