package kvraft

import (
	"bytes"
	"encoding/gob"
	"go-raft-server/kvdb"
)

type KVStateMachine interface {
	Get(key []byte) ([]byte, int, Err)
	Put(key []byte, value []byte, version int) Err
	Delete(key []byte, version int) Err
}

type KVVDB struct {
	db *kvdb.KVDB
}

type ValueVersion struct {
	Value   []byte
	Version int
}

func MakeKVVDB(path string) (*KVVDB, error) {
	db, err := kvdb.MakeKVDB(path)
	if err != nil {
		return nil, err
	}
	return &KVVDB{db}, nil
}

func (kvvdb *KVVDB) Close() error {
	return kvvdb.db.Close()
}

func (kvvdb *KVVDB) Get(key []byte) ([]byte, int, Err) {
	valueVersionBytes, err := kvvdb.db.Get(key)
	if err != nil {
		return nil, 0, ErrNoKey
	}
	var valueVersion ValueVersion
	r := bytes.NewBuffer(valueVersionBytes)
	d := gob.NewDecoder(r)
	if err := d.Decode(&valueVersion); err != nil {
		panic(err)
	}
	return valueVersion.Value, valueVersion.Version, Ok
}

func (kvvdb *KVVDB) Set(key []byte, value []byte, version int) Err {
	_, ver, err := kvvdb.Get(key)
	switch err {
	case Ok:
		{
			if ver+1 != version {
				return ErrVersion
			}
			w := new(bytes.Buffer)
			e := gob.NewEncoder(w)
			if err := e.Encode(&ValueVersion{Value: value, Version: version}); err != nil {
				panic(err)
			}
			if err := kvvdb.db.Set(key, w.Bytes()); err != nil {
				panic(err)
			}
			return Ok
		}
	case ErrNoKey:
		{
			if version != 1 {
				return ErrVersion
			}
			w := new(bytes.Buffer)
			e := gob.NewEncoder(w)
			if err := e.Encode(&ValueVersion{Value: value, Version: 1}); err != nil {
				panic(err)
			}
			if err := kvvdb.db.Set(key, w.Bytes()); err != nil {
				panic(err)
			}
			return Ok
		}
	}
	panic(err)
}
func (kvvdb *KVVDB) Delete(key []byte, version int) Err {
	_, ver, err := kvvdb.Get(key)
	switch err {
	case Ok:
		{
			if ver+1 != version {
				return ErrVersion
			}
			if err := kvvdb.db.Delete(key); err != nil {
				panic(err)
			}
			return Ok
		}
	case ErrNoKey:
		{
			if version != 1 {
				return ErrVersion
			}
			if err := kvvdb.db.Delete(key); err != nil {
				panic(err)
			}
			return Ok
		}
	}
	panic(err)
}
