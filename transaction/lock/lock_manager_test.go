// transaction/lock/lock_manager_test.go
package lock_test

import (
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/yourname/go-rdbms/transaction/lock"
	"github.com/yourname/go-rdbms/transaction/txid"
)

// lockAsync は別のゴルーチンでロックを要求し、結果を返すチャネルを返す
func lockAsync(lm *lock.LockManager, tx txid.TxID, name string, mode lock.Mode) <-chan error {
	done := make(chan error, 1)
	go func() { done <- lm.Lock(tx, name, mode) }()
	return done
}

// assertBlocked はロック要求がまだ待たされていることを確かめる
func assertBlocked(t *testing.T, done <-chan error) {
	t.Helper()
	select {
	case err := <-done:
		t.Fatalf("待たされるはずのロックが返ってきた: err=%v", err)
	case <-time.After(100 * time.Millisecond):
	}
}

// assertGranted はロック要求が成功して返ってくることを確かめる
func assertGranted(t *testing.T, done <-chan error) {
	t.Helper()
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("ロックが取れるはず: %v", err)
		}
	case <-time.After(time.Second):
		t.Fatal("ロックが取れないまま待たされている")
	}
}

func TestLockManager_SharedLocksCoexist(t *testing.T) {
	lm := lock.NewLockManager()

	var wg sync.WaitGroup
	for i := 1; i <= 5; i++ {
		wg.Add(1)
		go func(tx txid.TxID) {
			defer wg.Done()
			if err := lm.Lock(tx, "USERS", lock.Shared); err != nil {
				t.Errorf("TX %d: Sロックの取得に失敗: %v", tx, err)
			}
		}(txid.TxID(i))
	}
	wg.Wait() // 誰も待たされずに全員がSロックを取れる
}

func TestLockManager_ExclusiveBlocks(t *testing.T) {
	lm := lock.NewLockManager()

	if err := lm.Lock(1, "USERS", lock.Exclusive); err != nil {
		t.Fatal(err)
	}
	done := lockAsync(lm, 2, "USERS", lock.Shared)
	assertBlocked(t, done) // TX1のXロックがあるのでTX2は待たされる

	lm.ReleaseAll(1)
	assertGranted(t, done) // TX1が解放するとTX2が進める
}

func TestLockManager_Upgrade(t *testing.T) {
	lm := lock.NewLockManager()

	// 自分しかSロックを持っていなければ、そのままXにアップグレードできる
	lm.Lock(1, "USERS", lock.Shared)
	if err := lm.Lock(1, "USERS", lock.Exclusive); err != nil {
		t.Fatalf("アップグレードに失敗: %v", err)
	}

	// Xを持っている間は他のTXのSロックは待たされる
	done := lockAsync(lm, 2, "USERS", lock.Shared)
	assertBlocked(t, done)
	lm.ReleaseAll(1)
	assertGranted(t, done)
}

func TestLockManager_Deadlock(t *testing.T) {
	lm := lock.NewLockManager()

	lm.Lock(1, "A", lock.Exclusive)
	lm.Lock(2, "B", lock.Exclusive)

	// TX1 は B を待つ（TX1 → TX2）
	done1 := lockAsync(lm, 1, "B", lock.Exclusive)
	assertBlocked(t, done1)

	// TX2 が A を要求すると TX2 → TX1 の辺ができて閉路になる
	if err := lm.Lock(2, "A", lock.Exclusive); !errors.Is(err, lock.ErrDeadlock) {
		t.Fatalf("デッドロックが検出されるはず: got %v", err)
	}

	// TX2 がロールバックしてロックを解放すると、TX1 が進める
	lm.ReleaseAll(2)
	assertGranted(t, done1)
}

func TestLockManager_UpgradeDeadlock(t *testing.T) {
	lm := lock.NewLockManager()

	// 2つのTXが同じ資源のSロックを持ったまま、両方ともXにアップグレードしようとする
	lm.Lock(1, "USERS", lock.Shared)
	lm.Lock(2, "USERS", lock.Shared)

	done1 := lockAsync(lm, 1, "USERS", lock.Exclusive)
	assertBlocked(t, done1) // TX2のSロックがあるので待つ

	if err := lm.Lock(2, "USERS", lock.Exclusive); !errors.Is(err, lock.ErrDeadlock) {
		t.Fatalf("デッドロックが検出されるはず: got %v", err)
	}
	lm.ReleaseAll(2)
	assertGranted(t, done1)
}

func TestLockManager_WaiterKeepsExclusion(t *testing.T) {
	lm := lock.NewLockManager()

	// TX2 が待っている間に TX1 が解放し、TX2 がSロックを得る
	lm.Lock(1, "USERS", lock.Exclusive)
	done2 := lockAsync(lm, 2, "USERS", lock.Shared)
	assertBlocked(t, done2)
	lm.ReleaseAll(1)
	assertGranted(t, done2)

	// TX2 がSロックを持っているので、TX3 のXロックは待たされなければならない
	done3 := lockAsync(lm, 3, "USERS", lock.Exclusive)
	assertBlocked(t, done3)
	lm.ReleaseAll(2)
	assertGranted(t, done3)
}
