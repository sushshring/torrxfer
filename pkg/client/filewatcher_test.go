package client

import (
	"errors"
	"io/ioutil"
	"os"
	"path"
	"path/filepath"
	"testing"
	"time"

	"github.com/golang-collections/collections/set"
	"github.com/rs/zerolog"
	"github.com/rs/zerolog/log"
	"github.com/sushshring/torrxfer/pkg/common"
)

// const testFileDbName = "tfdb.dat"
const testWatchDir = "testdir"

func setup(t *testing.T, createFiles bool) error {
	t.Helper()
	common.ConfigureLogging(zerolog.TraceLevel, true, os.Stdout)
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
	os.RemoveAll(filepath.Join(tempDir, testWatchDir))
}

func TestNotifyCurrentFiles(t *testing.T) {
	// Setup file watcher
	var totalErr error
	err := setup(t, true)
	if err != nil {
		t.Error(err)
	}
	t.Cleanup(cleanup)
	testWatchDirPath := filepath.Join(os.TempDir(), testWatchDir)
	fw, err := NewFileWatcher(filepath.Join(os.TempDir(), testWatchDir), os.TempDir())
	if err != nil {
		t.Error(err)
		return
	}
	waitC := make(chan struct{})
	defer close(waitC)

	// Test timeout 20 seconds
	totalErr = errors.New("failed after timer expire")
	time.AfterFunc(20*time.Second, func() {
		fw.Close()
	})

	// Let all files be added to the local db and
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
		cleanPath, err := common.CleanPath(path.Join(testWatchDirPath, file.Name()))
		if err != nil {
			t.Error(err)
		}
		fileSet.Insert(cleanPath)
	}
	for file := range fw.RegisterForFileNotifications() {
		t.Logf("Got file: %s", file.Path)
		if !fileSet.Has(file.Path) {
			t.Errorf("Did not find file: %s", file.Path)
		}
		fileSet.Remove(file.Path)
		totalErr = nil
	}
	if fileSet.Len() != 0 {
		fileSet.Do(func(element interface{}) {
			t.Logf("File in set: %s", element)
		})
		t.Errorf("Did not find all files")
	}
	if totalErr != nil {
		t.Error(totalErr)
	}
}

func TestNotifyNewFile(t *testing.T) {
	const testfileName = "testfile"
	var totalErr error
	err := setup(t, false)
	if err != nil {
		t.Error(err)
	}
	t.Cleanup(cleanup)
	testWatchDirPath := filepath.Join(os.TempDir(), testWatchDir)
	fw, err := NewFileWatcher(filepath.Join(os.TempDir(), testWatchDir), os.TempDir())
	if err != nil {
		t.Error(err)
		return
	}
	waitC := make(chan struct{})
	defer close(waitC)

	// Test timeout 20 seconds
	totalErr = errors.New("failed after timer expire")
	time.AfterFunc(20*time.Second, func() {
		fw.Close()
	})

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
	for file := range fw.RegisterForFileNotifications() {
		t.Logf("Got file: %s", file.Path)
		testFilePath := filepath.Join(testWatchDirPath, testfileName)
		if p, err := common.CleanPath(testFilePath); err != nil {
			t.Error(err)
		} else if p != file.Path {
			t.Errorf("Expected path: %s, got file path: %s", p, file.Path)
		}
		totalErr = nil
	}
	if totalErr != nil {
		t.Error(totalErr)
	}
}

func TestResetTimerOnNewWrite(t *testing.T) {
	const testfileName = "testfile"
	err := setup(t, false)
	if err != nil {
		t.Error(err)
	}
	t.Cleanup(cleanup)
	testWatchDirPath := filepath.Join(os.TempDir(), testWatchDir)
	fw, err := NewFileWatcher(filepath.Join(os.TempDir(), testWatchDir), os.TempDir())
	if err != nil {
		t.Error(err)
		return
	}
	waitC := make(chan struct{})
	defer close(waitC)

	// Test timeout 60 seconds
	totalErr := errors.New("failed after timer expire")
	time.AfterFunc(1*time.Minute, func() {
		fw.Close()
	})

	go func() {
		time.Sleep(6 * time.Second)
		t.Log("Creating a new file")
		file, err := os.Create(filepath.Join(testWatchDirPath, testfileName))
		if err != nil {
			t.Error(err)
			return
		}
		defer file.Close()
		file.WriteString("Hello file\n")
		// Wait for a bit and write again
		// This assumes a flush will occur
		time.Sleep(8 * time.Second)
		file.WriteString("Hello another bit\n")

		// Wait another 5 seconds
		time.Sleep(8 * time.Second)
		file.WriteString("Hello a third bit\n")
	}()

	// Let all files be added to the local db
	for file := range fw.RegisterForFileNotifications() {
		t.Logf("Got file: %s", file.Path)
		testFilePath := filepath.Join(testWatchDirPath, testfileName)
		if p, err := common.CleanPath(testFilePath); err != nil {
			t.Error(err)
		} else if p != file.Path {
			t.Errorf("Expected path: %s, got file path: %s", p, file.Path)
		}
		content, err := os.ReadFile(testFilePath)
		if err != nil {
			t.Error(err)
		}

		expectedContent := "Hello file\nHello another bit\nHello a third bit\n"
		if string(content) != expectedContent {
			log.Error().Str("Expected", expectedContent).Str("Got", string(content)).Msg("Received non-matching content")
			t.Error(errors.New("Content does not match"))
		}
		totalErr = nil
	}
	if totalErr != nil {
		t.Error(totalErr)
	}
}
