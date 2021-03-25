package client

import (
	"errors"
	"io/ioutil"
	"os"
	"path"
	"sync"
	"testing"
	"time"

	"github.com/golang-collections/collections/set"
	"github.com/rs/zerolog/log"
	"github.com/sushshring/torrxfer/internal/db"
	"github.com/sushshring/torrxfer/pkg/common"
)

// Copy File watcher constructor for direct struct use
func newFileWatcher(directory, mediaDirectoryRoot string) (*fileWatcher, error) {
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

func TestNotifyCurrentFiles(t *testing.T) {
	// Setup file watcher
	var fw *fileWatcher
	fw, err := newFileWatcher(".", "/")
	writeDuration = time.Second
	if err != nil {
		t.Error(err)
		return
	}
	waitC := make(chan struct{}, 0)
	defer close(waitC)

	// Tst timeout 5 minutes
	timer := time.NewTimer(10 * time.Second)
	go func() {
		<-timer.C
		fw.Close()
	}()

	// Let all files be added to the local db and
	go func() {
		files, err := ioutil.ReadDir(".")
		if err != nil {
			t.Error(err)
			waitC <- struct{}{}
			return
		}
		fileSet := set.New()
		for _, file := range files {
			cwd, _ := os.Getwd()
			t.Logf("Added file: %s", path.Join(cwd, file.Name()))
			fileSet.Insert(path.Join(cwd, file.Name()))
		}
		for file := range fw.RegisterForFileNotifications() {
			t.Logf("Got file: %s", file.Path)
			fw.db.Delete(file.Path)
			if !fileSet.Has(file.Path) {
				t.Errorf("Did not find file: %s", file.Path)
			}
		}
		waitC <- struct{}{}
	}()
	<-waitC
}

func TestNotifyNewFile(t *testing.T) {
	const testfileName = "testfile"
	var fw *fileWatcher
	// Setup file watcher
	fw, err := newFileWatcher(".", "/")
	writeDuration = time.Second
	if err != nil {
		t.Error(err)
		return
	}
	waitC := make(chan struct{}, 0)
	defer close(waitC)

	// Tst timeout 5 minutes
	timer := time.NewTimer(10 * time.Second)
	go func() {
		<-timer.C
		t.Log("Timer fired")
		fw.Close()
	}()

	go func() {
		time.Sleep(6 * time.Second)
		t.Log("Creating a new file")
		file, err := os.Create(testfileName)
		defer file.Close()
		if err != nil {
			t.Error(err)
			return
		}
		file.WriteString("Hello file")
	}()
	// Delete test file
	defer os.Remove(testfileName)

	// Let all files be added to the local db and
	go func() {
		files, err := ioutil.ReadDir(".")
		if err != nil {
			t.Error(err)
			waitC <- struct{}{}
			return
		}
		fileSet := set.New()
		cwd, _ := os.Getwd()
		for _, file := range files {
			t.Logf("Added file: %s", path.Join(cwd, file.Name()))
			fileSet.Insert(path.Join(cwd, file.Name()))
		}
		// Except to find testfile but it shouldn't have been written yet
		fileSet.Insert(path.Join(cwd, testfileName))
		for file := range fw.RegisterForFileNotifications() {
			t.Logf("Got file: %s", file.Path)
			fw.db.Delete(file.Path)
			if !fileSet.Has(file.Path) {
				t.Errorf("Did not find file: %s", file.Path)
			} else {
				fileSet.Remove(file.Path)
			}
		}
		if fileSet.Len() != 0 {
			t.Errorf("Did not find all files")
		}
		waitC <- struct{}{}
	}()
	<-waitC
}
