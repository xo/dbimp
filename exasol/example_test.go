package exasol_test

import (
	"context"
	"database/sql"
	"fmt"
	"log"
	"time"

	"github.com/xo/dbimp"
	"github.com/xo/dbimp/exasol"
)

// Open a database with a DSN, run a query with arguments, and read each row. The
// server binds the arguments, so the driver prepares the statement for it.
func Example() { //nolint:testableexamples // The example needs a server, so it has no output to check.
	ctx := context.Background()
	if err := exampleQuery(ctx); err != nil {
		log.Fatal(err)
	}
}

func exampleQuery(ctx context.Context) error {
	db, err := sql.Open("exasol", "exasol://sys:exasol@localhost:8563?validateservercertificate=false")
	if err != nil {
		return err
	}
	defer db.Close()
	rows, err := db.QueryContext(ctx, "SELECT ID, NAME, CREATED FROM SHOP.ORDERS WHERE ID > ? AND NAME = ?", 100, "Ann")
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		// An integer column of 18 digits or fewer is an int64, a text is a
		// string, and a TIMESTAMP is a dbimp.LocalDateTime, which names no zone.
		var (
			id      int64
			name    sql.Null[string]
			created dbimp.LocalDateTime
		)
		if err := rows.Scan(&id, &name, &created); err != nil {
			return err
		}
		fmt.Println(id, name.V, created)
	}
	return rows.Err()
}

// A transaction turns autocommit off while it lasts. A context that ends stops
// the statement on the server, and the connection stays usable.
func Example_transaction() { //nolint:testableexamples // The example needs a server, so it has no output to check.
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	if err := exampleTransaction(ctx); err != nil {
		log.Print(err)
	}
}

func exampleTransaction(ctx context.Context) error {
	db, err := sql.Open("exasol", "exasol://sys:exasol@localhost:8563/SHOP?validateservercertificate=false")
	if err != nil {
		return err
	}
	defer db.Close()
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	// Rollback after Commit returns sql.ErrTxDone, which nothing needs to read.
	defer tx.Rollback()
	if _, err := tx.ExecContext(ctx, "UPDATE ORDERS SET NAME = ? WHERE ID = ?", "Bob", 101); err != nil {
		return err
	}
	return tx.Commit()
}

// The options of a statement go in its arguments, or in its context.
func Example_options() { //nolint:testableexamples // The example needs a server, so it has no output to check.
	ctx := context.Background()
	db, err := sql.Open("exasol", "exasol://sys:exasol@localhost:8563?validateservercertificate=false")
	if err != nil {
		log.Print(err)
		return
	}
	defer db.Close()
	// Stop the statement after ten seconds, and read three rows at most.
	rows, err := db.QueryContext(ctx, "SELECT * FROM SHOP.ORDERS",
		exasol.WithTimeout(10*time.Second),
		exasol.WithParameter("attributes", map[string]any{"resultSetMaxRows": 3}))
	if err != nil {
		log.Print(err)
		return
	}
	defer rows.Close()
	for rows.Next() {
		// Read each row here.
	}
	if err := rows.Err(); err != nil {
		log.Print(err)
	}
}
