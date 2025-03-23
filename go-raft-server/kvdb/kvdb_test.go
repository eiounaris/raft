package kvdb

import (
	"os"
	"testing"
)

func TestKVDB(t *testing.T) {

	dir, err := os.MkdirTemp("", "example")
	if err != nil {
		t.Error(err)
	}

	defer os.RemoveAll(dir)

	db, err := MakeKVDB(dir)
	if err != nil {
		t.Error(err)
	}

	if value, err := db.Get([]byte("myname")); value != nil || err == nil {
		t.Error()
	}

	if err := db.Set([]byte("myname"), []byte("zhangsan")); err != nil {
		t.Error(err)
	}

	if value, err := db.Get([]byte("myname")); string(value) != "zhangsan" || err != nil {
		t.Error()
	}

	if err := db.Delete([]byte("myname")); err != nil {
		t.Error(err)
	}

	if value, err := db.Get([]byte("myname")); value != nil || err == nil {
		t.Error()
	}

	if err := db.Set([]byte("myname"), []byte{}); err != nil {
		t.Error(err)
	}

	if value, err := db.Get([]byte("myname")); string(value) != "" || err != nil {
		t.Error()
	}

	if err := db.Delete([]byte("myname")); err != nil {
		t.Error(err)
	}

	if err := db.Close(); err != nil {
		t.Error(err)
	}
}
