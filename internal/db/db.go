package db

import (
	"os"
	"path/filepath"
	"sync"

	"github.com/prologic/bitcask"
	"github.com/rs/zerolog/log"
	"github.com/sushshring/torrxfer/pkg/crypto"
)

const threshold = 1000

type KvDb struct {
	innerDb       *bitcask.Bitcask
	calledChannel chan byte
	channelMux    sync.Mutex
}

var kvDb *KvDb
var once sync.Once

// GetDb Creates the database or initializes it from an existing file
func GetDb(dbFileName string) (db *KvDb, err error) {
	once.Do(func() {
		kvDb, err = initDb(dbFileName)
		if err != nil {
			log.Debug().Err(err).Msg("Could not init DB")
			kvDb = nil
		}
	})
	db = kvDb
	return db, nil
}

func initDb(dbFileName string) (*KvDb, error) {
	tempDir := os.TempDir()
	dbFilePath := filepath.Join(tempDir, dbFileName)
	db, err := bitcask.Open(dbFilePath)
	if err != nil {
		log.Debug().Stack().Err(err).Msg("Failed to open db")
		return nil, err
	}
	ret := new(KvDb)
	ret.innerDb = db
	ret.calledChannel = make(chan byte, 10)
	ret.channelMux = sync.Mutex{}
	go func() {
		calledCounter := 0
		for {
			ret.channelMux.Lock()
			defer ret.channelMux.Unlock()
			<-ret.calledChannel
			calledCounter++
			if calledCounter == threshold {
				ret.innerDb.Merge()
			}
		}
	}()
	return ret, nil
}

func (db *KvDb) called() {
	db.calledChannel <- byte(0)
}

func (db *KvDb) Close() {
	if err := db.innerDb.Close(); err != nil {
		log.Debug().Stack().Err(err).Msg("Could not close DB")
	}
}

func (db *KvDb) Put(key, value string) error {
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

func (db *KvDb) Get(key string) (string, error) {
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

func (db *KvDb) Delete(key string) error {
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

func (db *KvDb) Has(key string) bool {
	return db.innerDb.Has([]byte(key))
}
