package client

import (
	"errors"
	"io/ioutil"
	"os"
	"path"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/golang-collections/collections/set"
	"github.com/rs/zerolog/log"
	"github.com/sushshring/torrxfer/internal/db"
	"github.com/sushshring/torrxfer/pkg/common"
)

const testFileDbName = "tfdb.dat"
const testWatchDir = "testdir"

func setup(createFiles bool) error {
	tmpDir := os.TempDir()
	testWatchDirPath := filepath.Join(tmpDir, testWatchDir)
	err := os.MkdirAll(testWatchDirPath, 0777)
	if err != nil {
		return err
	}
	if createFiles {
		_, err = os.CreateTemp(testWatchDirPath, "NewFile")
		if err != nil {
			return err
		}
		_, err = os.CreateTemp(testWatchDirPath, "NewFile")
		if err != nil {
			return err
		}
		_, err = os.CreateTemp(testWatchDirPath, "NewFile")
		if err != nil {
			return err
		}
	}
	return nil
}

func cleanup() {
	tempDir := os.TempDir()
	dbFilePath := filepath.Join(tempDir, testFileDbName)
	os.RemoveAll(dbFilePath)
	os.RemoveAll(filepath.Join(tempDir, testWatchDir))
}

// Copy File watcher constructor for direct struct use
func newFileWatcher(directory, mediaDirectoryRoot string) (*fileWatcher, error) {
	// Verify media directory root is valid
	if !common.IsSubdir(mediaDirectoryRoot, directory) {
		return nil, errors.New("Invalid media directory root")
	}
	innerdb, err := db.GetDb(testFileDbName)
	if err != nil {
		log.Debug().Stack().Err(err).Msg("Failed to init db")
		return nil, err
	}
	filewatcher := &fileWatcher{
		innerdb,
		directory,
		make(chan File, 10),
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
	err := setup(true)
	if err != nil {
		t.Error(err)
	}
	t.Cleanup(cleanup)
	testWatchDirPath := filepath.Join(os.TempDir(), testWatchDir)
	fw, err := newFileWatcher(filepath.Join(os.TempDir(), testWatchDir), os.TempDir())
	if err != nil {
		t.Error(err)
		return
	}
	waitC := make(chan struct{})
	defer close(waitC)

	// Test timeout 5 minutes
	timer := time.NewTimer(15 * time.Second)
	go func() {
		<-timer.C
		fw.Close()
	}()

	// Let all files be added to the local db and
	go func() {
		files, err := ioutil.ReadDir(testWatchDirPath)
		if err != nil {
			t.Error(err)
			waitC <- struct{}{}
			return
		}
		fileSet := set.New()
		for _, file := range files {
			if file.IsDir() {
				continue
			}
			t.Logf("Added file: %s", path.Join(testWatchDirPath, file.Name()))
			path, err := common.CleanPath(path.Join(testWatchDirPath, file.Name()))
			if err != nil {
				t.Error(err)
			}
			fileSet.Insert(path)
		}
		for file := range fw.RegisterForFileNotifications() {
			t.Logf("Got file: %s", file.Path)
			fw.db.Delete(file.Path)
			if !fileSet.Has(file.Path) {
				t.Errorf("Did not find file: %s", file.Path)
			}
			fileSet.Remove(file.Path)
		}
		if fileSet.Len() != 0 {
			fileSet.Do(func(element interface{}) {
				t.Logf("File in set: %s", element)
			})
			t.Errorf("Did not find all files")
		}
		waitC <- struct{}{}
	}()
	<-waitC
}

func TestNotifyNewFile(t *testing.T) {
	const testfileName = "testfile"
	err := setup(false)
	if err != nil {
		t.Error(err)
	}
	t.Cleanup(cleanup)
	testWatchDirPath := filepath.Join(os.TempDir(), testWatchDir)
	fw, err := newFileWatcher(filepath.Join(os.TempDir(), testWatchDir), os.TempDir())
	if err != nil {
		t.Error(err)
		return
	}
	waitC := make(chan struct{})
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
		file, err := os.Create(filepath.Join(testWatchDirPath, testfileName))
		if err != nil {
			t.Error(err)
			return
		}
		defer file.Close()
		file.WriteString("Hello file")
	}()

	// Let all files be added to the local db and
	go func() {
		fileSet := set.New()
		// Expect to find testfile but it shouldn't have been written yet
		p, _ := common.CleanPath(path.Join(testWatchDirPath, testfileName))
		fileSet.Insert(p)
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
			fileSet.Do(func(element interface{}) {
				t.Logf("File in set: %s", element)
			})
			t.Errorf("Did not find all files")
		}
		waitC <- struct{}{}
	}()
	<-waitC
}
