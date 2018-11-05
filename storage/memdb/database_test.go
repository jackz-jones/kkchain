package memdb

import (
	"bytes"
	"fmt"
	"os"
	"strconv"
	"sync"
	"testing"
	"time"

	"github.com/invin/kkchain/storage"
)

func TestMemoryDB_PutGet(t *testing.T) {
	testPutGet(New(), t)
}

func TestMemoryDB_SequenceExecute(t *testing.T) {
	db := New()
	t1 := time.Now()
	//如果是同一个key只会覆盖，无法测试出性能，因此需要是不同的key来测,写入同一个key需要的时间约等于不同key的一半时间
	//1千万次
	//80000000超过10分钟都出不来，4千万需要216秒，
	for i := 0; i < 10000000; i++ {
		err := db.Put([]byte("0x6766c3279a7b32e52e89b24d203dd311aaf3019f9dd182f0128d8f12ab4490c2"+strconv.Itoa(i)), []byte("0x6766c3279a7b32e52e89b24d203dd311aaf3019f9dd182f0128d8f12ab4490c2"+strconv.Itoa(i)))
		//err := db.Put([]byte(strconv.Itoa(i)), []byte(strconv.Itoa(i)))
		if err != nil {
			t.Fatalf("put failed: %v", err)
		}
	}
	//fmt.Println("sizeof  map:", unsafe.Sizeof(db.db))
	//fmt.Println("len  map:", len(db.db))
	//fmt.Println("map[0]:", string(db.db["0x6766c3279a7b32e52e89b24d203dd311aaf3019f9dd182f0128d8f12ab4490c2"+strconv.Itoa(0)]))
	//fmt.Println("map[9000000]:", string(db.db["0x6766c3279a7b32e52e89b24d203dd311aaf3019f9dd182f0128d8f12ab4490c2"+strconv.Itoa(9000000)]))
	t2 := time.Now()
	fmt.Println("spend time:", t2.Sub(t1))

}

func TestMemoryDB_ConcurrentExecute(t *testing.T) {
	db := New()
	wg := sync.WaitGroup{}
	wg.Add(2)
	t1 := time.Now()
	exec1 := func() {
		for i := 0; i < 5000000; i++ {
			err := db.Put([]byte("0x6766c3279a7b32e52e89b24d203dd311aaf3019f9dd182f0128d8f12ab4490c2"+strconv.Itoa(i)), []byte("0x6766c3279a7b32e52e89b24d203dd311aaf3019f9dd182f0128d8f12ab4490c2"))
			if err != nil {
				t.Fatalf("put failed: %v", err)
			}
		}
		wg.Done()
	}
	// exec2 := func() {
	// 	for i := 2500000; i < 5000000; i++ {
	// 		err := db.Put([]byte("0x6766c3279a7b32e52e89b24d203dd311aaf3019f9dd182f0128d8f12ab4490c2"+string(i)), []byte("0x6766c3279a7b32e52e89b24d203dd311aaf3019f9dd182f0128d8f12ab4490c2"))
	// 		if err != nil {
	// 			t.Fatalf("put failed: %v", err)
	// 		}
	// 	}
	// 	wg.Done()
	// }
	exec3 := func() {
		for i := 5000000; i < 10000000; i++ {
			err := db.Put([]byte("0x6766c3279a7b32e52e89b24d203dd311aaf3019f9dd182f0128d8f12ab4490c2"+strconv.Itoa(i)), []byte("0x6766c3279a7b32e52e89b24d203dd311aaf3019f9dd182f0128d8f12ab4490c2"))
			if err != nil {
				t.Fatalf("put failed: %v", err)
			}
		}
		wg.Done()
	}
	// exec4 := func() {
	// 	for i := 7500000; i < 10000000; i++ {
	// 		err := db.Put([]byte("0x6766c3279a7b32e52e89b24d203dd311aaf3019f9dd182f0128d8f12ab4490c2"+string(i)), []byte("0x6766c3279a7b32e52e89b24d203dd311aaf3019f9dd182f0128d8f12ab4490c2"))
	// 		if err != nil {
	// 			t.Fatalf("put failed: %v", err)
	// 		}
	// 	}
	// 	wg.Done()
	// }
	go exec1()
	//go exec2()
	go exec3()
	//go exec4()
	wg.Wait()
	t2 := time.Now()
	fmt.Println("spend time:", t2.Sub(t1))
}

func TestFile_SequenceWrite(t *testing.T) {
	//db := New()
	f, _ := os.Create("./output.txt")
	defer f.Close()
	var buf bytes.Buffer
	buf.Reset()

	// for i := 0; i < 2000000; i++ {
	// 	buf.Write([]byte("0x6766c3279a7b32e52e89b24d203dd311aaf3019f9dd182f0128d8f12ab4490ck"))
	// }
	t1 := time.Now()
	_, err := f.Write([]byte("0x6766c3279a7b32e52e89b24d203dd311aaf3019f9dd182f0128d8f12ab4490c2"))
	if err != nil {
		t.Fatalf("put failed: %v", err)
	}

	// for i := 0; i < 2000000; i++ {
	// 	//buf.Write([]byte("0x6766c3279a7b32e52e89b24d203dd311aaf3019f9dd182f0128d8f12ab4490ck"))
	// 	_, err := f.Write([]byte("0x6766c3279a7b32e52e89b24d203dd311aaf3019f9dd182f0128d8f12ab4490c2"))
	// 	if err != nil {
	// 		t.Fatalf("put failed: %v", err)
	// 	}
	// }
	//err := db.Put([]byte("a"), buf.Bytes())
	//_, err := f.Write(buf.Bytes()) //固态硬盘纯写入速度为1GB/s左右，启动读和写都是十分耗时的，因此需要减少读写速度
	//_, err := f.WriteString("0x6766c3279a7b32e52e89b24d203dd311aaf3019f9dd182f0128d8f12ab4490c2")
	//err := ioutil.WriteFile("test.txt", []byte("0x6766c3279a7b32e52e89b24d203dd311aaf3019f9dd182f0128d8f12ab4490c2"), 0644)

	t2 := time.Now()
	fmt.Println("spend time:", t2.Sub(t1))
}

func TestFile_ConcurrentWrite(t *testing.T) {
	f1, _ := os.Create("./output1.txt")
	f2, _ := os.Create("./output2.txt")
	defer f1.Close()
	defer f2.Close()
	wg := sync.WaitGroup{}
	wg.Add(2)
	t1 := time.Now()
	go func() {
		for i := 0; i < 1000000; i++ {
			_, err := f1.Write([]byte("0x6766c3279a7b32e52e89b24d203dd311aaf3019f9dd182f0128d8f12ab4490c2"))
			if err != nil {
				t.Fatalf("put failed: %v", err)
			}
		}
		wg.Done()
	}()
	go func() {
		for i := 1000000; i < 2000000; i++ {
			_, err := f2.Write([]byte("0x6766c3279a7b32e52e89b24d203dd311aaf3019f9dd182f0128d8f12ab4490c2"))
			if err != nil {
				t.Fatalf("put failed: %v", err)
			}
		}
		wg.Done()
	}()
	wg.Wait()
	t2 := time.Now()
	fmt.Println("spend time:", t2.Sub(t1))
}

//结论：单纯的并发写入内存，是基本不会加快速度的，开的协程超过cpu核数甚至会减慢速度。因为内存设备并不支持并发写入。设备支持一个出口的读写。
//     但并发写入磁盘文件是会加快速度，因为写入磁盘的时候，中间会经过一些额外的处理，是需要计算任务的。但是协程数超过cpu核数后，也不再会有性能的提高了。
//     因此想利用cpu的多核特性来提高性能，处理的函数除了IO写入之外，还要包含一些额外的计算任务，甚至从总体来讲，计算任务耗费的时间
//     比io写入的时间多很多，这时并发执行才能提现出价值来。
// 2核cpu的情况下：两个协程比单个协程快的重要因素是写入的key需要一定的cpu计算。三个协程会更慢。并且根据内存特性，写入过一次后，再写入相同kv会比第一次快。
//结果：而对于写入文件，提高性能最好的方法是减少读写次数，一次性写入。固态硬盘的写入速度能去到500MB-1GB左右
/*
liangminhongdeMacBook-Pro:memdb liangminhong$ go test -v -test.run TestMemoryDB_SequenceExecute
=== RUN   TestMemoryDB_SequenceExecute
spend time: 40.412874952s
--- PASS: TestMemoryDB_SequenceExecute (40.41s)
PASS
ok      github.com/invin/kkchain/storage/memdb  40.483s
liangminhongdeMacBook-Pro:memdb liangminhong$ go test -v -test.run TestMemoryDB_ConcurrentExecute
=== RUN   TestMemoryDB_ConcurrentExecute
spend time: 36.295862698s
--- PASS: TestMemoryDB_ConcurrentExecute (36.30s)
PASS
ok      github.com/invin/kkchain/storage/memdb  36.358s
*/

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
