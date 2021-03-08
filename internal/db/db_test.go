package db

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/sushshring/torrxfer/pkg/crypto"
)

var kvDbTest *KvDb

func TestInitDb(t *testing.T) {
	const (
		dbFileName string = "testfile"
	)
	var err error
	kvDbTest, err = initDb(dbFileName)
	if err != nil {
		t.Error(err)
		return
	}
	t.Logf("Created Kvdb: %s", kvDbTest.innerDb.Path())

	// Validation
	tempdir := os.TempDir()
	// Validate db file is created
	if _, err := os.Stat(filepath.Join(tempdir, dbFileName)); os.IsNotExist(err) {
		t.Error(err)
		return
	}
}

func TestPut(t *testing.T) {
	const (
		testKey   string = "key"
		testValue string = "value"
	)
	if err := kvDbTest.Put(testKey, testValue); err != nil {
		t.Error(err)
		return
	}

	// Calculate key hash
	hash, err := crypto.Hash(testKey)
	if err != nil {
		t.Error(err)
		return
	}
	if !kvDbTest.innerDb.Has([]byte(hash)) {
		t.Errorf("Key not found: %s", hash)
	}
}

func TestGet(t *testing.T) {
	const (
		testKey   string = "key"
		testValue string = "value"
	)
	value, err := kvDbTest.Get(testKey)
	if err != nil {
		t.Error(err)
		return
	}
	if value != testValue {
		t.Errorf("Retrieved incorrect value. Expected: %s got %s", testValue, value)
	}
}
