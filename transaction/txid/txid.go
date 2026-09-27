// transaction/txid/txid.go

// Package txid はトランザクションIDの型だけを持つ小さなパッケージ。
// transaction・lock・log の各パッケージから参照するため、循環importを避けて独立させている
package txid

// TxID はトランザクションを一意に識別するID（1から単調増加）
type TxID uint32

// Invalid は無効なトランザクションIDを表す
const Invalid TxID = 0
