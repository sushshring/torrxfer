package client

import (
	"errors"
	"os"
	"sync"
	"time"

	"github.com/radovskyb/watcher"
	"github.com/rs/zerolog/log"
	"github.com/sushshring/torrxfer/pkg/common"
)

// How long to wait after a write to issue a notification.
// Variable instead of const to override during test
const writeDuration time.Duration = 10 * time.Second

// FileWatcher provides notifications when changes occur on the provided watched directory
type FileWatcher interface {
	RegisterForFileNotifications() <-chan *File
	Close()
}

type fileWatcher struct {
	watchedDirectory                 string
	activeFilesMap                   map[string]chan *File
	outgoingFileNotificationChannels []chan *File
	w                                *watcher.Watcher
	mediaDirectoryRoot               string
	sync.RWMutex
}

// NewFileWatcher starts watching the provided directory for any new writes and here-before unseen files
// If there is a new file, this will wait up to two minutes for any new writes, at which point it will
func NewFileWatcher(directory string, mediaDirectoryRoot string) (FileWatcher, error) {
	log.Trace().Msg("Creating file watcher")

	// Verify media directory root is valid
	if !common.IsSubdir(mediaDirectoryRoot, directory) {
		return nil, errors.New("invalid media directory root")
	}
	filewatcher := &fileWatcher{
		directory,
		make(map[string]chan *File),
		make([]chan *File, 0),
		nil,
		mediaDirectoryRoot,
		sync.RWMutex{}}
	// Run file watch logic thread
	go func() {
		defer func() {
			filewatcher.RLock()
			defer filewatcher.RUnlock()
			for _, channel := range filewatcher.outgoingFileNotificationChannels {
				close(channel)
			}
		}()
		filewatcher.watcherThread()
		// Once filewatcher closes either due to error or the watcher being forcibly closed
		// this returns and closes the filewatcher channel. Any listeners will then return as well
	}()
	return filewatcher, nil
}

// RegisterForFileNotifications retuns a channel that responds with File objects
// for any new file write or update. On init, it will also send a notification for all
// the files in the folder regardless of their presence in the database
func (filewatcher *fileWatcher) RegisterForFileNotifications() <-chan *File {
	channel := make(chan *File, 10)
	filewatcher.Lock()
	defer filewatcher.Unlock()
	filewatcher.outgoingFileNotificationChannels = append(filewatcher.outgoingFileNotificationChannels, channel)
	return channel
}

// Close shuts down a file watcher. All pending transfers are flushed and channels are all closed
func (filewatcher *fileWatcher) Close() {
	filewatcher.w.Close()
	// At filewatcher.w close, this causes watcherThread to exit
}

func (filewatcher *fileWatcher) watcherThread() {
	log.Debug().Msg("Starting watch thread")

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
		log.Trace().Str("File name", path).Msg("Found file")
		clientFile, err := NewClientFile(path, filewatcher.mediaDirectoryRoot)
		// If this generates an error, this file is in a weird state and a notification shouldn't be queued
		if err != nil {
			log.Debug().Err(err).Msg("Could not generate file representation")
			continue
		}
		err = filewatcher.handleFileEvent(clientFile.Path, int64(clientFile.Size), clientFile.ModifiedTime)
		if err != nil {
			common.LogErrorStack(err, "Failure in handling file")
			continue
		}
	}

	// Handle watcher events
	go func() {
		for {
			select {
			case event := <-filewatcher.w.Event:
				log.Trace().Str("File", event.Path).Msg("Got new file write")
				filewatcher.handleFileEvent(event.Path, event.Size(), event.ModTime())
			case <-filewatcher.w.Closed:
				log.Trace().Msg("File watcher closed")
				return
			case err := <-filewatcher.w.Error:
				// Close and delete filewatcher
				log.Debug().Err(err).Msg("File watcher closed")
				return
			}
		}
	}()
	if err := filewatcher.w.Start(time.Millisecond * 100); err != nil {
		log.Debug().Stack().Err(err).Msg("Could not start file watcher")
		return
	}
}

// Handles file create and write events. If a file is already watched, this waits for the next write
// This assumes that the thread to transfer file was already kicked off when the file was created. In case there
// is write to a previously watched file, the event will be ignored
func (filewatcher *fileWatcher) handleFileEvent(path string, size int64, modTime time.Time) error {
	log.Trace().Str("File", path).Int64("Size", size).Time("Modified at", modTime).Msg("Handling file")
	stat, err := os.Stat(path)
	if err != nil {
		log.Debug().Err(err).Msg("Could not stat file details. Skipping")
		return err
	}
	if stat.IsDir() {
		// Skip directories
		return nil
	}
	file, err := NewClientFile(path, filewatcher.mediaDirectoryRoot)
	if err != nil {
		log.Debug().Err(err).Msg("Could not generate file representation")
		return err
	}
	// If this is the first time this file is getting queued, run the handler thread
	if _, ok := filewatcher.activeFilesMap[file.Path]; !ok {
		log.Trace().Str("File path", file.Path).Msg("Saw file first time. Setting up channels")
		func() {
			madeChannel := make(chan *File, 5)
			filewatcher.Lock()
			defer filewatcher.Unlock()
			filewatcher.activeFilesMap[file.Path] = madeChannel
			go filewatcher.fileEventHandlerThread(madeChannel)
		}()
	}

	file.WatchTime = time.Now()
	file.TransferTime = time.Unix(0, 0)

	filewatcher.RLock()
	channel := filewatcher.activeFilesMap[file.Path]
	filewatcher.RUnlock()

	channel <- file

	return nil
}

func (filewatcher *fileWatcher) fileEventHandlerThread(fileUpdatesChannel chan *File) {
	log.Trace().Msg("Starting file write wait timer")
	waitChannel := make(chan struct{})
	var timer *time.Timer
	timer = time.NewTimer(writeDuration)
	var file *File
	for {
		select {
		case updatedFile, ok := <-fileUpdatesChannel:
			if ok {
				log.Trace().Str("File path", updatedFile.Path).Msg("Received file details. Queueing transfer")
				file = updatedFile
				if !timer.Stop() {
					<-timer.C
				}
				timer = time.AfterFunc(writeDuration, func() {
					waitChannel <- struct{}{}
				})
			}
		case <-waitChannel:
			log.Trace().Str("File path", file.Path).Msg("Finished waiting. Sending notification")
			filewatcher.notifyFileAvailable(file)
			close(fileUpdatesChannel)
			filewatcher.Lock()
			defer filewatcher.Unlock()
			delete(filewatcher.activeFilesMap, file.Path)
			return
		}
	}
}

func (filewatcher *fileWatcher) notifyFileAvailable(file *File) {
	log.Trace().Str("File path", file.Path).Msg("Starting file transfer after waiting for write")
	filewatcher.RLock()
	defer filewatcher.RUnlock()
	for _, notificationChannel := range filewatcher.outgoingFileNotificationChannels {
		notificationChannel <- file
	}
}
