package db

import (
	"os"
	"path/filepath"
	"sync"
	"time"

	pogreb "github.com/akrylysov/pogreb"
	"github.com/akrylysov/pogreb/fs"
	"github.com/rs/zerolog/log"
	"github.com/sushshring/torrxfer/pkg/crypto"
)

const threshold = 1000

// KvDB is a simple file backed key value persistent store
type KvDB interface {
	Close()
	Put(key, value string) error
	Get(key string) (string, error)
	Delete(key string) error
	Has(key string) bool
}

type kvDb struct {
	innerDb       *pogreb.DB
	calledChannel chan struct{}
	channelMux    sync.Mutex
}

var singleton *kvDb
var once sync.Once

// GetDb Creates the database or initializes it from an existing file
func GetDb(dbFileName string) (db KvDB, err error) {
	once.Do(func() {
		singleton, err = initDb(dbFileName)
		if err != nil {
			log.Debug().Err(err).Msg("Could not init DB")
			singleton = nil
		}
	})
	db = singleton
	return
}

func initDb(dbFileName string) (*kvDb, error) {
	tempDir := os.TempDir()
	dbFilePath := filepath.Join(tempDir, dbFileName)
	opts := &pogreb.Options{
		BackgroundSyncInterval:       2 * time.Minute,
		BackgroundCompactionInterval: 0,
		FileSystem:                   fs.OS,
	}
	db, err := pogreb.Open(dbFilePath, opts)
	if err != nil {
		log.Debug().Stack().Err(err).Msg("Failed to open db. Retrying with data loss")
		return nil, err
	}
	ret := &kvDb{db, make(chan struct{}, 1000), sync.Mutex{}}
	go func() {
		calledCounter := 0
		for range ret.calledChannel {
			ret.channelMux.Lock()
			calledCounter++
			if calledCounter == threshold {
				ret.innerDb.Compact()
				calledCounter = 0
			}
		}
	}()
	return ret, nil
}

func (db *kvDb) called() {
	db.calledChannel <- struct{}{}
}

func (db *kvDb) Close() {
	defer db.called()
	db.innerDb.Sync()
	db.innerDb.Close()
	if err := db.innerDb.Close(); err != nil {
		log.Debug().Stack().Err(err).Msg("Could not close DB")
	}
}

func (db *kvDb) Put(key, value string) error {
	defer db.called()
	hash, err := crypto.Hash(key)
	if err != nil {
		log.Debug().Stack().Err(err).Str("Key", key).Msg("Could not hash value")
		return err
	}
	err = db.innerDb.Put([]byte(hash), []byte(value))
	if err != nil {
		log.Debug().Stack().Err(err).Msg("Put failed")
	}
	return err
}

func (db *kvDb) Get(key string) (string, error) {
	defer db.called()
	hash, err := crypto.Hash(key)
	if err != nil {
		log.Debug().Stack().Err(err).Str("Key", key).Msg("Could not hash value")
		return "", err
	}
	value, err := db.innerDb.Get([]byte(hash))
	if err != nil {
		log.Debug().Stack().Err(err).Msg("Get failed")
	}
	return string(value), err
}

func (db *kvDb) Delete(key string) error {
	defer db.called()
	hash, err := crypto.Hash(key)
	if err != nil {
		log.Debug().Stack().Err(err).Str("Key", key).Msg("Could not hash value")
		return err
	}
	err = db.innerDb.Delete([]byte(hash))
	if err != nil {
		log.Debug().Stack().Err(err).Msg("Delete failed")
	}
	return err
}

func (db *kvDb) Has(key string) (has bool) {
	defer db.called()
	has = false
	hash, err := crypto.Hash(key)
	if err != nil {
		log.Debug().Stack().Err(err).Str("Key", key).Msg("Could not hash value")
		return
	}
	has, err = db.innerDb.Has([]byte(hash))
	if err != nil {
		has = false
	}
	return
}
