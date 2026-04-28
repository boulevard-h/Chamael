package db

import (
	"database/sql"
	"fmt"
	"os"
	"strings"

	_ "github.com/mattn/go-sqlite3"
)

func SaveTxsToSQL(txs []string, filename string) error {
	if _, err := os.Stat(filename); err == nil {
		err := os.Remove(filename)
		if err != nil {
			return fmt.Errorf("delete sqlite database file %q failed: %w", filename, err)
		}
	}

	db, err := sql.Open("sqlite3", filename)
	if err != nil {
		return fmt.Errorf("open sqlite database %q failed: %w", filename, err)
	}
	defer db.Close()

	// Start a transaction.
	tx, err := db.Begin()
	if err != nil {
		return fmt.Errorf("begin transaction for %q failed: %w", filename, err)
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
		return fmt.Errorf("create transactions table in %q failed: %w", filename, err)
	}

	// Clear the table before inserting new data
	clearTableSQL := `DELETE FROM transactions;`
	_, err = tx.Exec(clearTableSQL)
	if err != nil {
		tx.Rollback()
		return fmt.Errorf("clear transactions table in %q failed: %w", filename, err)
	}

	// Prepare the batch insert statement.
	insertSQL := `INSERT INTO transactions (tx) VALUES (?)`
	stmt, err := tx.Prepare(insertSQL)
	if err != nil {
		tx.Rollback()
		return fmt.Errorf("prepare insert statement for %q failed: %w", filename, err)
	}
	defer stmt.Close()

	// Insert data in batches.
	for _, txData := range txs {
		_, err = stmt.Exec(txData)
		if err != nil {
			tx.Rollback()
			return fmt.Errorf("insert transaction into %q failed: %w", filename, err)
		}
	}

	// Commit the transaction.
	err = tx.Commit()
	if err != nil {
		return fmt.Errorf("commit transaction for %q failed: %w", filename, err)
	}
	return nil
}

func LoadAndDeleteTxsFromDB(dbPath string, limit int) ([]string, error) {
	db, err := sql.Open("sqlite3", dbPath)
	if err != nil {
		return nil, fmt.Errorf("failed to open database: %v", err)
	}
	defer db.Close()

	// Apply performance-related pragmas.
	_, err = db.Exec("PRAGMA journal_mode = WAL")
	if err != nil {
		return nil, fmt.Errorf("failed to set WAL mode: %v", err)
	}
	_, err = db.Exec("PRAGMA synchronous = NORMAL")
	if err != nil {
		return nil, fmt.Errorf("failed to set synchronous mode: %v", err)
	}

	// Start a transaction.
	tx, err := db.Begin()
	if err != nil {
		return nil, fmt.Errorf("failed to begin transaction: %v", err)
	}

	// Limit the number of returned transactions.
	rows, err := tx.Query("SELECT id, tx FROM transactions LIMIT ?", limit)
	if err != nil {
		tx.Rollback()
		return nil, fmt.Errorf("failed to query database: %v", err)
	}
	defer rows.Close()

	var txs []string
	var txIDs []int

	// Read transactions from the database.
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

	// Delete the transactions that were read.
	if len(txIDs) > 0 {
		// Delete in batches of at most 500 IDs.
		batchSize := 500
		for i := 0; i < len(txIDs); i += batchSize {
			end := i + batchSize
			if end > len(txIDs) {
				end = len(txIDs)
			}

			// Build IN-query arguments for the current batch.
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

	// Commit the transaction.
	err = tx.Commit()
	if err != nil {
		return nil, fmt.Errorf("failed to commit transaction: %v", err)
	}

	return txs, nil
}
