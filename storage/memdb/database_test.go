package memdb 

import (
	"bytes"
	"testing"

	"github.com/invin/kkchain/storage"
)

func TestMemoryDB_PutGet(t *testing.T) {
	testPutGet(New(), t)
}

var testValues = []string{"", "a", "1251", "\x00123\x00"}

func testPutGet(db storage.Database, t *testing.T) {
	t.Parallel()

	for _, k := range testValues {
		err := db.Put([]byte(k), nil)
		if err != nil {
			t.Fatalf("put failed: %v", err)
		}
	}

	for _, k := range testValues {
		data, err := db.Get([]byte(k))
		if err != nil {
			t.Fatalf("get failed: %v", err)
		}
		if len(data) != 0 {
			t.Fatalf("get returned wrong result, got %q expected nil", string(data))
		}
	}

	_, err := db.Get([]byte("non-exist-key"))
	if err == nil {
		t.Fatalf("expect to return a not found error")
	}

	for _, v := range testValues {
		err := db.Put([]byte(v), []byte(v))
		if err != nil {
			t.Fatalf("put failed: %v", err)
		}
	}

	for _, v := range testValues {
		data, err := db.Get([]byte(v))
		if err != nil {
			t.Fatalf("get failed: %v", err)
		}
		if !bytes.Equal(data, []byte(v)) {
			t.Fatalf("get returned wrong result, got %q expected %q", string(data), v)
		}
	}

	for _, v := range testValues {
		err := db.Put([]byte(v), []byte("?"))
		if err != nil {
			t.Fatalf("put override failed: %v", err)
		}
	}

	for _, v := range testValues {
		data, err := db.Get([]byte(v))
		if err != nil {
			t.Fatalf("get failed: %v", err)
		}
		if !bytes.Equal(data, []byte("?")) {
			t.Fatalf("get returned wrong result, got %q expected ?", string(data))
		}
	}

	for _, v := range testValues {
		orig, err := db.Get([]byte(v))
		if err != nil {
			t.Fatalf("get failed: %v", err)
		}
		orig[0] = byte(0xff)
		data, err := db.Get([]byte(v))
		if err != nil {
			t.Fatalf("get failed: %v", err)
		}
		if !bytes.Equal(data, []byte("?")) {
			t.Fatalf("get returned wrong result, got %q expected ?", string(data))
		}
	}

	for _, v := range testValues {
		err := db.Delete([]byte(v))
		if err != nil {
			t.Fatalf("delete %q failed: %v", v, err)
		}
	}

	for _, v := range testValues {
		_, err := db.Get([]byte(v))
		if err == nil {
			t.Fatalf("got deleted value %q", v)
		}
	}
}