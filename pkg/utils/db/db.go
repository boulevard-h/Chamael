package db

import (
	"database/sql"
	"fmt"
	"log"
	"os"
	"strings"

	_ "github.com/mattn/go-sqlite3"
)

func SaveTxsToSQL(txs []string, filename string) {
	if _, err := os.Stat(filename); err == nil {
		err := os.Remove(filename)
		if err != nil {
			log.Fatalf("Error deleting SQLite database file: %v\n", err)
		}
		//log.Printf("Existing database file '%s' removed.\n", filename)
	}

	db, err := sql.Open("sqlite3", filename)
	if err != nil {
		log.Fatalf("Error opening SQLite database: %v\n", err)
	}
	defer db.Close()

	// 开启事务
	tx, err := db.Begin()
	if err != nil {
		log.Fatalf("Error beginning transaction: %v\n", err)
	}

	// Create the table for transactions
	createTableSQL := `
	CREATE TABLE IF NOT EXISTS transactions (
	    id INTEGER PRIMARY KEY,
		tx TEXT NOT NULL
	);`
	_, err = tx.Exec(createTableSQL)
	if err != nil {
		tx.Rollback()
		log.Fatalf("Error creating table: %v\n", err)
	}

	// Clear the table before inserting new data
	clearTableSQL := `DELETE FROM transactions;`
	_, err = tx.Exec(clearTableSQL)
	if err != nil {
		tx.Rollback()
		log.Fatalf("Error clearing table: %v\n", err)
	}

	// 准备批量插入语句
	insertSQL := `INSERT INTO transactions (tx) VALUES (?)`
	stmt, err := tx.Prepare(insertSQL)
	if err != nil {
		tx.Rollback()
		log.Fatalf("Error preparing insert statement: %v\n", err)
	}
	defer stmt.Close()

	// 批量插入数据
	for _, txData := range txs {
		_, err = stmt.Exec(txData)
		if err != nil {
			tx.Rollback()
			log.Fatalf("Error inserting transaction: %v\n", err)
		}
	}

	// 提交事务
	err = tx.Commit()
	if err != nil {
		log.Fatalf("Error committing transaction: %v\n", err)
	}
}

func LoadAndDeleteTxsFromDB(dbPath string, limit int) ([]string, error) {
	db, err := sql.Open("sqlite3", dbPath)
	if err != nil {
		return nil, fmt.Errorf("failed to open database: %v", err)
	}
	defer db.Close()

	// 设置一些性能优化参数
	_, err = db.Exec("PRAGMA journal_mode = WAL")
	if err != nil {
		return nil, fmt.Errorf("failed to set WAL mode: %v", err)
	}
	_, err = db.Exec("PRAGMA synchronous = NORMAL")
	if err != nil {
		return nil, fmt.Errorf("failed to set synchronous mode: %v", err)
	}

	// 开启事务
	tx, err := db.Begin()
	if err != nil {
		return nil, fmt.Errorf("failed to begin transaction: %v", err)
	}

	// 使用 LIMIT 限制返回的事务数量
	rows, err := tx.Query("SELECT id, tx FROM transactions LIMIT ?", limit)
	if err != nil {
		tx.Rollback()
		return nil, fmt.Errorf("failed to query database: %v", err)
	}
	defer rows.Close()

	var txs []string
	var txIDs []int

	// 读取数据库中的事务
	for rows.Next() {
		var id int
		var txData string
		if err := rows.Scan(&id, &txData); err != nil {
			tx.Rollback()
			return nil, fmt.Errorf("failed to scan row: %v", err)
		}
		txs = append(txs, txData)
		txIDs = append(txIDs, id)
	}

	if err := rows.Err(); err != nil {
		tx.Rollback()
		return nil, fmt.Errorf("error iterating rows: %v", err)
	}

	// 批量删除已读取的事务
	if len(txIDs) > 0 {
		// 分批处理删除操作，每批最多处理 500 个 ID
		batchSize := 500
		for i := 0; i < len(txIDs); i += batchSize {
			end := i + batchSize
			if end > len(txIDs) {
				end = len(txIDs)
			}

			// 构建当前批次的 IN 查询参数
			placeholders := make([]string, end-i)
			args := make([]interface{}, end-i)
			for j := range placeholders {
				placeholders[j] = "?"
				args[j] = txIDs[i+j]
			}
			deleteSQL := fmt.Sprintf("DELETE FROM transactions WHERE id IN (%s)", strings.Join(placeholders, ","))

			_, err = tx.Exec(deleteSQL, args...)
			if err != nil {
				tx.Rollback()
				return nil, fmt.Errorf("failed to delete transactions batch: %v", err)
			}
		}
	}

	// 提交事务
	err = tx.Commit()
	if err != nil {
		return nil, fmt.Errorf("failed to commit transaction: %v", err)
	}

	return txs, nil
}
