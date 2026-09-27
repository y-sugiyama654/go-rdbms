# go-rdbms

Goでリレーショナルデータベースを0から実装するブログシリーズ「[RDBMSを自作して学ぶ](https://log4me.dev/blog/rdbms-chapter1/)」のサンプルコードです。

ページ単位のディスクI/Oから始めて、バッファプール、ヒープファイル、B+木インデックス、SQLのパーサと実行エンジン、トランザクション、ロック、WALによるクラッシュリカバリまでを実装し、最終的にCLIからSQLを実行できるデータベースを作ります。

## 必要なもの

- Go 1.24 以降

## 使い方

```bash
# テスト（データ競合の検出付き）
go test -race ./...

# CLIをビルドして起動する（データは -dir で指定したディレクトリに保存される）
go build -o go-rdbms .
./go-rdbms -dir go-rdbms-data

# ベンチマーク
go test ./bench/... -bench=. -benchmem
```

CLIでは次のSQLが使えます。SQL文は `;` で終えます。

```sql
CREATE TABLE users (id INT PRIMARY KEY, name VARCHAR(20), age INT);
INSERT INTO users VALUES (1, 'Alice', 30);
SELECT name FROM users WHERE id = 1;
SELECT users.name, orders.item FROM users JOIN orders ON users.id = orders.user_id;
BEGIN; INSERT INTO users VALUES (2, 'Bob', 25); ROLLBACK;
```

`\tables` でテーブルの一覧、`\help` でヘルプ、`\q` で終了します。

## ディレクトリ構成と記事の対応

| ディレクトリ | 内容 | 記事 |
|-------------|------|------|
| `hello_database/` | ファイルにページを書いて読む最小限のコード | [第2章](https://log4me.dev/blog/rdbms-chapter2/) |
| `storage/page/`、`storage/disk/` | ページとDiskManager | [第3章](https://log4me.dev/blog/rdbms-chapter3/) |
| `storage/buffer/` | バッファプールマネージャ（LRU） | [第4章](https://log4me.dev/blog/rdbms-chapter4/) |
| `storage/page/slotted_page.go`、`storage/heap/` | スロット付きページとヒープファイル | [第5章](https://log4me.dev/blog/rdbms-chapter5/) |
| `storage/btree/` | B+木インデックス | [第6章](https://log4me.dev/blog/rdbms-chapter6/) |
| `sql/lexer/`、`sql/parser/` | 字句解析・構文解析 | [第7章](https://log4me.dev/blog/rdbms-chapter7/) |
| `sql/planner/`、`sql/executor/` | 実行計画と実行エンジン | [第8章](https://log4me.dev/blog/rdbms-chapter8/) |
| `sql/catalog/` | カタログ（テーブル定義の管理と永続化） | [第9章](https://log4me.dev/blog/rdbms-chapter9/) |
| `transaction/` | トランザクション | [第10章](https://log4me.dev/blog/rdbms-chapter10/) |
| `transaction/lock/` | ロックとデッドロック検出 | [第11章](https://log4me.dev/blog/rdbms-chapter11/) |
| `transaction/log/` | WALとリカバリ | [第12章](https://log4me.dev/blog/rdbms-chapter12/) |
| `db/`、`main.go`、`bench/` | 全体の統合・CLI・ベンチマーク | [第13章](https://log4me.dev/blog/rdbms-chapter13/) |

各章の時点のコードは、`rdbms_chapter02`〜`rdbms_chapter13` のブランチ（マージ後はそれぞれの章のコミット）で確認できます。どの章の時点でも `go vet ./...` と `go test -race ./...` が通ります。

## モジュール名について

記事中のコードに合わせて、モジュール名は `github.com/yourname/go-rdbms` にしています。このリポジトリをクローンしてビルド・テストする分には、そのまま動きます。

## 実装の範囲

学習用の実装なので、次の機能は省略しています（詳しくは[第14章](https://log4me.dev/blog/rdbms-chapter14/)）。

- UPDATE・DELETE、NULL、集約・ソート
- MVCC（ロックはテーブル単位の厳格2相ロック）
- コストベースのクエリ最適化
- ネットワーク層、レプリケーション
