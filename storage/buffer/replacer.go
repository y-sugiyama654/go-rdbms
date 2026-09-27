// storage/buffer/replacer.go
package buffer

// FrameID はバッファプール内のフレームを識別するID（0始まり）
type FrameID uint32

// Replacer は追い出し対象のフレームを選ぶポリシーのインターフェース
type Replacer interface {
	// Victim は追い出すフレームを選び、Replacerの管理から取り除いて返す
	// 追い出し候補が1つもない場合は false を返す
	Victim() (FrameID, bool)
	// Pin はフレームがピンされたことを記録し、追い出し候補から除外する
	Pin(id FrameID)
	// Unpin はフレームがアンピンされたことを記録し、追い出し候補に加える
	Unpin(id FrameID)
	// Size は現在追い出し候補になっているフレーム数を返す
	Size() int
}
