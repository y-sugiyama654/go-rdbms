// hello_database/main.go
// 第2章: ファイルにページを書いて読む最小限のコード（DiskManagerの原型）
package main

import (
	"encoding/binary"
	"fmt"
	"os"
)

const PageSize = 4096

func writePage(f *os.File, pageID uint32, data []byte) error {
	offset := int64(pageID) * PageSize
	_, err := f.WriteAt(data, offset)
	return err
}

func readPage(f *os.File, pageID uint32, buf []byte) error {
	offset := int64(pageID) * PageSize
	_, err := f.ReadAt(buf, offset)
	return err
}

func main() {
	f, err := os.OpenFile("test.db", os.O_RDWR|os.O_CREATE, 0644)
	if err != nil {
		panic(err)
	}
	defer f.Close()

	// 書き込み
	page := make([]byte, PageSize)
	binary.BigEndian.PutUint32(page[0:4], 42)
	if err := writePage(f, 0, page); err != nil {
		panic(err)
	}

	// 読み込み
	buf := make([]byte, PageSize)
	if err := readPage(f, 0, buf); err != nil {
		panic(err)
	}

	val := binary.BigEndian.Uint32(buf[0:4])
	fmt.Printf("読み込んだ値: %d\n", val) // 42
}
