package client

import (
	"io/ioutil"
	"os"
	"path"
	"testing"
	"time"

	"github.com/golang-collections/collections/set"
)

func TestNotifyCurrentFiles(t *testing.T) {
	// Setup file watcher
	fileWatcher, err := NewFileWatcher(".", "/")
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
		fileWatcher.Close()
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
		for file := range fileWatcher.RegisterForFileNotifications() {
			t.Logf("Got file: %s", file.Path)
			fileWatcher.db.Delete(file.Path)
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
	// Setup file watcher
	fileWatcher, err := NewFileWatcher(".", "/")
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
		fileWatcher.Close()
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
		for file := range fileWatcher.RegisterForFileNotifications() {
			t.Logf("Got file: %s", file.Path)
			fileWatcher.db.Delete(file.Path)
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
