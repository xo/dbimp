package maxcompute_test

import (
	"context"
	"database/sql"
	"fmt"
	"log"

	"github.com/xo/dbimp/maxcompute"
)

// count opens a project of MaxCompute and counts the rows of a table. The host
// names the region, and the path names the project. A key of the query that starts
// with odps. is a setting of the server for every statement.
func count(dsn string) (int64, error) {
	db, err := sql.Open(maxcompute.Name, dsn)
	if err != nil {
		return 0, fmt.Errorf("opening the project: %w", err)
	}
	defer db.Close()
	var n int64
	if err := db.QueryRowContext(context.Background(), "SELECT COUNT(*) FROM mytable").Scan(&n); err != nil {
		return 0, fmt.Errorf("counting the rows: %w", err)
	}
	return n, nil
}

func Example() { //nolint:testableexamples // The example needs a project of MaxCompute.
	n, err := count("maxcompute://<access key id>:<access key secret>@service.ap-southeast-5.maxcompute.aliyun.com/myproject")
	if err != nil {
		log.Fatal(err)
	}
	fmt.Println(n)
}

// run runs a statement of two parts in script mode, which the caller sets, because
// the driver never counts the statements of a query.
func run(dsn string) error {
	db, err := sql.Open(maxcompute.Name, dsn)
	if err != nil {
		return fmt.Errorf("opening the project: %w", err)
	}
	defer db.Close()
	script := maxcompute.WithParameter("odps.sql.submit.mode", "script")
	if _, err := db.ExecContext(context.Background(), "SET odps.sql.type.system.odps2=true; INSERT INTO t SELECT 1", script); err != nil {
		return fmt.Errorf("running the script: %w", err)
	}
	return nil
}

func ExampleWithParameter() { //nolint:testableexamples // The example needs a project of MaxCompute.
	if err := run("maxcompute://id:secret@service.ap-southeast-5.maxcompute.aliyun.com/myproject"); err != nil {
		log.Fatal(err)
	}
}
