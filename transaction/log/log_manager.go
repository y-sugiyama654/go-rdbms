// transaction/log/log_manager.go
package log

import (
	"bufio"
	"encoding/binary"
	"errors"
	"fmt"
	"hash/crc32"
	"io"
	"os"
	"sync"
)

// ログファイル上では、1件のレコードを次の形式で書く
//
//	[本体の長さ(4B)][本体のCRC32(4B)][本体]
//
// 書き込みの途中でクラッシュすると末尾のレコードが中途半端に残るが、
// 長さが足りない・CRCが合わないことで検出して捨てられる
const frameHeaderSize = 8

// LogManager はWALファイルへの追記・フラッシュ・読み出しを管理する
type LogManager struct {
	mu         sync.Mutex
	path       string
	file       *os.File
	buf        []byte // まだファイルに書いていないログ
	nextLSN    LSN
	flushedLSN LSN // このLSNまでのログはfsync済み
}

// NewLogManager はWALファイルを開く。既存のログがあれば最後のLSNの続きから番号を振る
func NewLogManager(path string) (*LogManager, error) {
	f, err := os.OpenFile(path, os.O_RDWR|os.O_CREATE, 0644)
	if err != nil {
		return nil, fmt.Errorf("log: %s を開けません: %w", path, err)
	}
	records, validEnd, err := readRecords(f)
	if err != nil {
		f.Close()
		return nil, err
	}
	// 末尾の壊れたレコードを切り捨て、続きはその位置から書く
	if err := f.Truncate(validEnd); err != nil {
		f.Close()
		return nil, err
	}
	if _, err := f.Seek(validEnd, io.SeekStart); err != nil {
		f.Close()
		return nil, err
	}

	lm := &LogManager{path: path, file: f, nextLSN: 1}
	if len(records) > 0 {
		lm.nextLSN = records[len(records)-1].LSN + 1
	}
	lm.flushedLSN = lm.nextLSN - 1
	return lm, nil
}

// Append はログレコードにLSNを振ってメモリ上のバッファに追加し、そのLSNを返す
// この時点ではまだディスクに書かれていない
func (lm *LogManager) Append(rec *LogRecord) LSN {
	lm.mu.Lock()
	defer lm.mu.Unlock()

	rec.LSN = lm.nextLSN
	lm.nextLSN++
	lm.buf = appendFrame(lm.buf, rec)
	return rec.LSN
}

// appendFrame はレコードを [長さ][CRC32][本体] の形式でbufの後ろに付け足す
func appendFrame(buf []byte, rec *LogRecord) []byte {
	body := rec.encode()
	buf = binary.BigEndian.AppendUint32(buf, uint32(len(body)))
	buf = binary.BigEndian.AppendUint32(buf, crc32.ChecksumIEEE(body))
	return append(buf, body...)
}

// FlushTo はlsnまでのログがディスクに永続化されていることを保証する
func (lm *LogManager) FlushTo(lsn LSN) error {
	lm.mu.Lock()
	defer lm.mu.Unlock()

	if lsn <= lm.flushedLSN {
		return nil // もうfsync済み
	}
	return lm.flushLocked()
}

// Flush はバッファにあるログをすべてディスクに永続化する
func (lm *LogManager) Flush() error {
	lm.mu.Lock()
	defer lm.mu.Unlock()
	return lm.flushLocked()
}

// flushLocked はバッファの内容をすべてファイルに書いてfsyncする（lm.muを持った状態で呼ぶ）
func (lm *LogManager) flushLocked() error {
	if len(lm.buf) > 0 {
		if _, err := lm.file.Write(lm.buf); err != nil {
			return fmt.Errorf("log: 書き込みに失敗しました: %w", err)
		}
		lm.buf = lm.buf[:0]
	}
	if err := lm.file.Sync(); err != nil {
		return fmt.Errorf("log: fsyncに失敗しました: %w", err)
	}
	lm.flushedLSN = lm.nextLSN - 1
	return nil
}

// ReadAll はログファイルの全レコードを先頭から読む（リカバリ用）
func (lm *LogManager) ReadAll() ([]*LogRecord, error) {
	lm.mu.Lock()
	defer lm.mu.Unlock()

	if err := lm.flushLocked(); err != nil {
		return nil, err
	}
	f, err := os.Open(lm.path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	records, _, err := readRecords(f)
	return records, err
}

// Truncate はログを捨て、CHECKPOINTレコード1件だけのログに置き換える
// チェックポイント（すべての変更がデータファイルに反映済み）のときにだけ呼ぶこと。
// LSNは続きの番号を使うので、データファイル上のページLSNより必ず大きくなる
func (lm *LogManager) Truncate() error {
	lm.mu.Lock()
	defer lm.mu.Unlock()

	rec := &LogRecord{LSN: lm.nextLSN, Type: RecordCheckpoint}
	lm.nextLSN++

	// 新しいログを一時ファイルに書いてfsyncし、リネームで差し替える。
	// 途中で落ちても、古いログか新しいログのどちらかが必ず残る
	tmp := lm.path + ".tmp"
	f, err := os.Create(tmp)
	if err != nil {
		return err
	}
	if _, err := f.Write(appendFrame(nil, rec)); err != nil {
		f.Close()
		return err
	}
	if err := f.Sync(); err != nil {
		f.Close()
		return err
	}
	if err := os.Rename(tmp, lm.path); err != nil {
		f.Close()
		return err
	}

	lm.file.Close()
	lm.file = f // 以降はこの新しいファイルの末尾に追記する
	lm.buf = lm.buf[:0]
	lm.flushedLSN = rec.LSN
	return nil
}

// Close はバッファを書き出してファイルを閉じる
func (lm *LogManager) Close() error {
	lm.mu.Lock()
	defer lm.mu.Unlock()

	if err := lm.flushLocked(); err != nil {
		return err
	}
	return lm.file.Close()
}

// readRecords はファイルの先頭からレコードを読み、正しく読めた末尾の位置も返す
func readRecords(f *os.File) ([]*LogRecord, int64, error) {
	if _, err := f.Seek(0, io.SeekStart); err != nil {
		return nil, 0, err
	}
	r := bufio.NewReader(f)
	var records []*LogRecord
	var offset int64
	header := make([]byte, frameHeaderSize)
	for {
		// io.ReadFull は指定した長さを読み切るまで読む（足りなければエラー）
		if _, err := io.ReadFull(r, header); err != nil {
			if errors.Is(err, io.EOF) || errors.Is(err, io.ErrUnexpectedEOF) {
				return records, offset, nil
			}
			return nil, 0, err
		}
		body := make([]byte, binary.BigEndian.Uint32(header[0:4]))
		if _, err := io.ReadFull(r, body); err != nil {
			return records, offset, nil // 書きかけのレコード
		}
		if crc32.ChecksumIEEE(body) != binary.BigEndian.Uint32(header[4:8]) {
			return records, offset, nil // 壊れたレコード
		}
		rec, err := decodeRecord(body)
		if err != nil {
			return records, offset, nil
		}
		records = append(records, rec)
		offset += int64(frameHeaderSize + len(body))
	}
}
