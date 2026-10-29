package tablestore_test

import (
	"context"
	"database/sql"
	"fmt"
	"log"
	"time"

	"github.com/xo/dbimp/tablestore"
)

// exampleDSN is the DSN of an instance. SQL maps a table that the API made, so an
// example starts from a table named orders that the API created with the key id.
const exampleDSN = "tablestore://key:secret@myinstance.ap-southeast-5.ots.aliyuncs.com/myinstance"

// Example maps a table, then writes and reads it with SQL. The server binds no
// argument, so the driver writes each argument into the text of the statement.
func Example() { //nolint:testableexamples // The example needs an instance, so it has no output to check.
	if err := exampleQuery(context.Background()); err != nil {
		log.Fatal(err)
	}
}

func exampleQuery(ctx context.Context) error {
	db, err := sql.Open("tablestore", exampleDSN)
	if err != nil {
		return fmt.Errorf("opening the database: %w", err)
	}
	defer db.Close()
	if _, err := db.ExecContext(ctx, "CREATE TABLE orders (id BIGINT, name MEDIUMTEXT, placed DATETIME, PRIMARY KEY(id));"); err != nil {
		return fmt.Errorf("mapping the table: %w", err)
	}
	if _, err := db.ExecContext(ctx, "INSERT INTO orders (id, name) VALUES (?, ?)", int64(1), "first"); err != nil {
		return fmt.Errorf("inserting: %w", err)
	}
	var name string
	if err := db.QueryRowContext(ctx, "SELECT name FROM orders WHERE id = ?", int64(1)).Scan(&name); err != nil {
		return fmt.Errorf("selecting: %w", err)
	}
	fmt.Println(name)
	return nil
}

// ExampleWithParameter sends the member sql_version of the request for one
// statement.
func ExampleWithParameter() { //nolint:testableexamples // The example needs an instance, so it has no output to check.
	if err := exampleParameter(context.Background()); err != nil {
		log.Fatal(err)
	}
}

func exampleParameter(ctx context.Context) error {
	db, err := sql.Open("tablestore", exampleDSN)
	if err != nil {
		return fmt.Errorf("opening the database: %w", err)
	}
	defer db.Close()
	var version string
	if err := db.QueryRowContext(ctx, "SELECT @@version", tablestore.WithParameter("sql_version", 1)).Scan(&version); err != nil {
		return fmt.Errorf("reading the version: %w", err)
	}
	fmt.Println(version)
	return nil
}

// ExampleWithOptions sets the options of every statement that a context starts.
// The server has no setting for a timeout, so the context is the way to bound a
// statement.
func ExampleWithOptions() { //nolint:testableexamples // The example needs an instance, so it has no output to check.
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	err := examplePing(tablestore.WithOptions(ctx, tablestore.WithDatabase("myinstance")))
	cancel()
	if err != nil {
		log.Fatal(err)
	}
}

func examplePing(ctx context.Context) error {
	db, err := sql.Open("tablestore", exampleDSN)
	if err != nil {
		return fmt.Errorf("opening the database: %w", err)
	}
	defer db.Close()
	return db.PingContext(ctx) //nolint:wrapcheck // The example returns the error of database/sql.
}
