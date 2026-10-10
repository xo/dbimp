package spanner_test

import (
	"context"
	"database/sql"
	"errors"
	"testing"

	"github.com/xo/dbimp/spanner"
)

// TestIntegrationDatabaseRole holds D198: a connector with database_role makes
// sessions that run as the role, so a table that the role can read is read, and a
// table that it has no grant on is refused. The login of the DSN needs the right
// to make a role (roles/spanner.databaseAdmin), and the right to use it
// (roles/spanner.fineGrainedAccessUser, and roles/spanner.databaseRoleUser on the
// role). Without the second, the server refuses the session, and the test skips.
func TestIntegrationDatabaseRole(t *testing.T) {
	db := connect(t)
	role := name("role")
	granted, denied := name("role_granted"), name("role_denied")
	ddl(t, db, "CREATE TABLE "+granted+" (id INT64 NOT NULL) PRIMARY KEY (id)", "DROP TABLE "+granted)
	ddl(t, db, "CREATE TABLE "+denied+" (id INT64 NOT NULL) PRIMARY KEY (id)", "DROP TABLE "+denied)
	exec(t, db, "INSERT INTO "+granted+" (id) VALUES (1)")
	exec(t, db, "INSERT INTO "+denied+" (id) VALUES (1)")
	// The drops run in the reverse order of the calls, so the role goes before the
	// tables, and its grant goes before the role.
	if _, err := db.ExecContext(t.Context(), "CREATE ROLE "+role); err != nil {
		if isEmulator(t) {
			t.Skipf("the emulator refused CREATE ROLE, so it has no database roles: %v", err)
		}
		t.Fatalf("CREATE ROLE: %v", err)
	}
	t.Cleanup(func() {
		if _, err := db.ExecContext(context.WithoutCancel(t.Context()), "DROP ROLE "+role); err != nil {
			t.Errorf("cleaning up: DROP ROLE %s: %v", role, err)
		}
	})
	ddl(t, db, "GRANT SELECT ON TABLE "+granted+" TO ROLE "+role, "REVOKE SELECT ON TABLE "+granted+" FROM ROLE "+role)

	cfg, err := spanner.ParseDSN(dsn(t))
	if err != nil {
		t.Fatal(err)
	}
	cfg.DatabaseRole = role
	viaDSN, err := spanner.ParseDSN(cfg.FormatDSN())
	if err != nil || viaDSN.DatabaseRole != role {
		t.Fatalf("the DSN %q reads back as %+v: %v", cfg.FormatDSN(), viaDSN, err)
	}
	rdb := sql.OpenDB(spanner.NewConnector(*viaDSN))
	t.Cleanup(func() { rdb.Close() })

	if err := rdb.PingContext(t.Context()); err != nil {
		if serr, ok := errors.AsType[*spanner.Error](err); ok && serr.Status == "PERMISSION_DENIED" && !isEmulator(t) {
			t.Skipf("the login cannot use the role %s, which needs roles/spanner.fineGrainedAccessUser: %v", role, err)
		}
		t.Fatalf("opening a session as the role: %v", err)
	}
	if n := count(t, rdb, "SELECT COUNT(*) FROM "+granted); n != 1 {
		t.Errorf("the role read %d rows of the granted table, want 1", n)
	}
	err = failure(t, rdb, "SELECT COUNT(*) FROM "+denied)
	if err == nil {
		if isEmulator(t) {
			t.Skipf("the emulator does not enforce the grants of a database role, so the role read %s", denied)
		}
		t.Fatalf("the role read the table %s, which it has no grant on", denied)
	}
	if serr, ok := errors.AsType[*spanner.Error](err); !ok || serr.Status != "PERMISSION_DENIED" && !isEmulator(t) {
		t.Errorf("the ungranted table gave %v, want an *spanner.Error PERMISSION_DENIED", err)
	}
	// The login without the role reads both tables, and the option sets the role of
	// one statement.
	if n := count(t, db, "SELECT COUNT(*) FROM "+denied); n != 1 {
		t.Errorf("the login without a role read %d rows, want 1", n)
	}
	row := db.QueryRowContext(t.Context(), "SELECT COUNT(*) FROM "+denied, spanner.WithDatabaseRole(role))
	var n int64
	if err := row.Scan(&n); err == nil {
		t.Errorf("WithDatabaseRole(%s) read the ungranted table", role)
	}
}
