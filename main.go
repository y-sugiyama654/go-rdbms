// main.go
package main

import (
	"bufio"
	"flag"
	"fmt"
	"os"
	"strings"
	"unicode/utf8"

	"github.com/yourname/go-rdbms/db"
	"github.com/yourname/go-rdbms/sql/executor"
)

func main() {
	dir := flag.String("dir", "go-rdbms-data", "データを置くディレクトリ")
	flag.Parse()

	database, err := db.Open(db.Config{Dir: *dir})
	if err != nil {
		fmt.Fprintf(os.Stderr, "データベースを開けませんでした: %v\n", err)
		os.Exit(1)
	}
	defer database.Close()
	session := database.NewSession()
	defer session.Close()

	if database.Recovered() {
		fmt.Println("前回は正常に終了しなかったため、リカバリを行いました。")
	}
	fmt.Println("go-rdbms へようこそ！ SQL文は ; で終えてください（\\help でヘルプ、\\q で終了）")

	scanner := bufio.NewScanner(os.Stdin)
	var buf strings.Builder
	for {
		switch {
		case buf.Len() > 0:
			fmt.Print("      -> ")
		case session.InTransaction():
			fmt.Print("go-rdbms*> ") // * はトランザクションの途中であることを表す
		default:
			fmt.Print("go-rdbms> ")
		}
		if !scanner.Scan() {
			fmt.Println()
			return
		}
		line := strings.TrimSpace(scanner.Text())

		// \ で始まる行はメタコマンド
		if buf.Len() == 0 && strings.HasPrefix(line, `\`) {
			if quit := handleMetaCommand(database, line); quit {
				return
			}
			continue
		}
		if line == "" {
			continue
		}

		// ; が来るまで複数行を1つの文としてためる
		buf.WriteString(line)
		buf.WriteString(" ")
		if !strings.HasSuffix(line, ";") {
			continue
		}
		sql := buf.String()
		buf.Reset()

		result, err := session.Exec(sql)
		if err != nil {
			fmt.Printf("エラー: %v\n", err)
			continue
		}
		printResult(result)
	}
}

// handleMetaCommand はメタコマンドを処理する。終了する場合は true を返す
func handleMetaCommand(database *db.DB, cmd string) bool {
	switch cmd {
	case `\q`:
		return true
	case `\tables`:
		tables := database.ListTables()
		if len(tables) == 0 {
			fmt.Println("テーブルはありません")
		}
		for _, t := range tables {
			fmt.Println(t)
		}
	case `\help`:
		fmt.Println(`\tables  テーブルの一覧を表示する`)
		fmt.Println(`\help    このヘルプを表示する`)
		fmt.Println(`\q       終了する`)
	default:
		fmt.Printf("不明なコマンドです: %s\n", cmd)
	}
	return false
}

// printResult は実行結果を表形式で表示する
func printResult(r *executor.Result) {
	switch {
	case r.Message != "":
		fmt.Println(r.Message)
		return
	case r.Columns == nil:
		fmt.Printf("%d行を挿入しました\n", r.RowsAffected)
		return
	}

	// 各列の幅を、見出しと値のうち最も長いものに合わせる
	widths := make([]int, len(r.Columns))
	for i, c := range r.Columns {
		widths[i] = utf8.RuneCountInString(c)
	}
	cells := make([][]string, len(r.Rows))
	for i, row := range r.Rows {
		cells[i] = make([]string, len(row))
		for j, v := range row {
			cells[i][j] = v.String()
			widths[j] = max(widths[j], utf8.RuneCountInString(cells[i][j]))
		}
	}

	printRow(r.Columns, widths)
	seps := make([]string, len(widths))
	for i, w := range widths {
		seps[i] = strings.Repeat("-", w)
	}
	fmt.Println(strings.Join(seps, "-+-"))
	for _, row := range cells {
		printRow(row, widths)
	}
	fmt.Printf("(%d行)\n", len(r.Rows))
}

func printRow(values []string, widths []int) {
	padded := make([]string, len(values))
	for i, v := range values {
		padded[i] = v + strings.Repeat(" ", widths[i]-utf8.RuneCountInString(v))
	}
	fmt.Println(strings.TrimRight(strings.Join(padded, " | "), " "))
}
