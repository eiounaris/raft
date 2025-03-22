package kvdb

import (
	badger "github.com/dgraph-io/badger/v4"
)

type KVDB struct {
	db *badger.DB
}

func MakeKVDB(path string) (*KVDB, error) {
	db, err := badger.Open(badger.DefaultOptions(path))
	if err != nil {
		return nil, err
	}
	return &KVDB{db}, nil
}

func (kvdb *KVDB) Close() error {
	return kvdb.db.Close()
}

func (kvdb *KVDB) Get(key []byte) ([]byte, error) {
	var valCopy []byte
	err := kvdb.db.View(func(txn *badger.Txn) error {
		item, err := txn.Get(key)
		if err != nil {
			return err
		}
		valCopy, err = item.ValueCopy(nil)
		return err
	})
	if err != nil {
		return nil, err
	}
	return valCopy, nil
}

func (kvdb *KVDB) Set(key []byte, value []byte) error {
	err := kvdb.db.Update(func(txn *badger.Txn) error {
		return txn.Set(key, value)
	})
	return err
}

func (kvdb *KVDB) Delete(key []byte) error {
	err := kvdb.db.Update(func(txn *badger.Txn) error {
		return txn.Delete(key)
	})
	return err
}
