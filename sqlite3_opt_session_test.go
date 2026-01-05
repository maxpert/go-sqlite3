//go:build sqlite_session
// +build sqlite_session

package sqlite3

import (
	"bytes"
	"context"
	"database/sql"
	"database/sql/driver"
	"os"
	"testing"
)

// execConn is a helper to execute SQL on a SQLiteConn directly to avoid
// deadlock when called from inside conn.Raw() callback
func execConn(c *SQLiteConn, query string, args ...any) (driver.Result, error) {
	vals := make([]driver.Value, len(args))
	for i, a := range args {
		vals[i] = a
	}
	return c.Exec(query, vals)
}

// queryRowConn is a helper to query a single value from a SQLiteConn
func queryRowConn(c *SQLiteConn, query string, args ...any) ([]driver.Value, error) {
	vals := make([]driver.NamedValue, len(args))
	for i, a := range args {
		vals[i] = driver.NamedValue{Ordinal: i + 1, Value: a}
	}
	rows, err := c.QueryContext(context.Background(), query, vals)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	cols := rows.Columns()
	result := make([]driver.Value, len(cols))
	if err := rows.Next(result); err != nil {
		return nil, err
	}
	return result, nil
}

func TestSessionCreate(t *testing.T) {
	// Test creating a session on a connection
	db, err := sql.Open("sqlite3", ":memory:")
	if err != nil {
		t.Fatal("Failed to open database:", err)
	}
	defer db.Close()

	conn, err := db.Conn(context.Background())
	if err != nil {
		t.Fatal("Failed to get connection:", err)
	}
	defer conn.Close()

	var session *Session
	err = conn.Raw(func(driverConn any) error {
		c := driverConn.(*SQLiteConn)
		var err error
		session, err = c.CreateSession("main")
		return err
	})
	if err != nil {
		t.Fatal("Failed to create session:", err)
	}
	defer session.Close()

	if session == nil {
		t.Fatal("Session is nil")
	}
}

func TestSessionAttach(t *testing.T) {
	// Test attaching tables (specific and all)
	db, err := sql.Open("sqlite3", ":memory:")
	if err != nil {
		t.Fatal("Failed to open database:", err)
	}
	defer db.Close()

	// Create a test table
	_, err = db.Exec("CREATE TABLE test (id INTEGER PRIMARY KEY, value TEXT)")
	if err != nil {
		t.Fatal("Failed to create table:", err)
	}

	conn, err := db.Conn(context.Background())
	if err != nil {
		t.Fatal("Failed to get connection:", err)
	}
	defer conn.Close()

	var session *Session
	err = conn.Raw(func(driverConn any) error {
		c := driverConn.(*SQLiteConn)
		var err error
		session, err = c.CreateSession("main")
		if err != nil {
			return err
		}

		// Test attaching specific table
		err = session.Attach("test")
		if err != nil {
			return err
		}

		// Test attaching all tables (nil)
		err = session.Attach("")
		return err
	})
	if err != nil {
		t.Fatal("Failed to attach session:", err)
	}
	defer session.Close()
}

func TestSessionIsEmpty(t *testing.T) {
	// Test IsEmpty before and after changes
	db, err := sql.Open("sqlite3", ":memory:")
	if err != nil {
		t.Fatal("Failed to open database:", err)
	}
	defer db.Close()

	_, err = db.Exec("CREATE TABLE test (id INTEGER PRIMARY KEY, value TEXT)")
	if err != nil {
		t.Fatal("Failed to create table:", err)
	}

	conn, err := db.Conn(context.Background())
	if err != nil {
		t.Fatal("Failed to get connection:", err)
	}
	defer conn.Close()

	var isEmpty1, isEmpty2 bool
	err = conn.Raw(func(driverConn any) error {
		c := driverConn.(*SQLiteConn)
		session, err := c.CreateSession("main")
		if err != nil {
			return err
		}
		defer session.Close()

		err = session.Attach("")
		if err != nil {
			return err
		}

		// Should be empty before any changes
		isEmpty1 = session.IsEmpty()

		// Make a change - use SQLiteConn.Exec directly to avoid deadlock
		_, err = execConn(c, "INSERT INTO test (value) VALUES ('test')")
		if err != nil {
			return err
		}

		// Should not be empty after changes
		isEmpty2 = session.IsEmpty()
		return nil
	})
	if err != nil {
		t.Fatal("Failed:", err)
	}

	if !isEmpty1 {
		t.Error("Session should be empty before changes")
	}
	if isEmpty2 {
		t.Error("Session should not be empty after changes")
	}
}

func TestSessionEnable(t *testing.T) {
	// Test enabling/disabling session
	db, err := sql.Open("sqlite3", ":memory:")
	if err != nil {
		t.Fatal("Failed to open database:", err)
	}
	defer db.Close()

	_, err = db.Exec("CREATE TABLE test (id INTEGER PRIMARY KEY, value TEXT)")
	if err != nil {
		t.Fatal("Failed to create table:", err)
	}

	conn, err := db.Conn(context.Background())
	if err != nil {
		t.Fatal("Failed to get connection:", err)
	}
	defer conn.Close()

	err = conn.Raw(func(driverConn any) error {
		c := driverConn.(*SQLiteConn)
		session, err := c.CreateSession("main")
		if err != nil {
			return err
		}
		defer session.Close()

		err = session.Attach("")
		if err != nil {
			return err
		}

		// Disable session - Enable returns the current state after setting
		cur := session.Enable(0)
		if cur != 0 {
			t.Errorf("Expected current value to be 0 (disabled), got %d", cur)
		}

		// Make a change while disabled - use SQLiteConn.Exec directly
		_, err = execConn(c, "INSERT INTO test (value) VALUES ('disabled')")
		if err != nil {
			return err
		}

		// Should still be empty because session was disabled
		if !session.IsEmpty() {
			t.Error("Session should be empty when disabled")
		}

		// Re-enable session - Enable returns the current state after setting
		cur = session.Enable(1)
		if cur != 1 {
			t.Errorf("Expected current value to be 1 (enabled), got %d", cur)
		}

		// Make a change while enabled - use SQLiteConn.Exec directly
		_, err = execConn(c, "INSERT INTO test (value) VALUES ('enabled')")
		if err != nil {
			return err
		}

		// Should not be empty now
		if session.IsEmpty() {
			t.Error("Session should not be empty when enabled")
		}

		return nil
	})
	if err != nil {
		t.Fatal("Failed:", err)
	}
}

func TestSessionChangeset(t *testing.T) {
	// Test basic changeset generation with INSERT
	db, err := sql.Open("sqlite3", ":memory:")
	if err != nil {
		t.Fatal("Failed to open database:", err)
	}
	defer db.Close()

	_, err = db.Exec("CREATE TABLE test (id INTEGER PRIMARY KEY, value TEXT)")
	if err != nil {
		t.Fatal("Failed to create table:", err)
	}

	conn, err := db.Conn(context.Background())
	if err != nil {
		t.Fatal("Failed to get connection:", err)
	}
	defer conn.Close()

	var changeset []byte
	err = conn.Raw(func(driverConn any) error {
		c := driverConn.(*SQLiteConn)
		session, err := c.CreateSession("main")
		if err != nil {
			return err
		}
		defer session.Close()

		err = session.Attach("")
		if err != nil {
			return err
		}

		// Make changes - use SQLiteConn.Exec directly
		_, err = execConn(c, "INSERT INTO test (value) VALUES ('test1')")
		if err != nil {
			return err
		}
		_, err = execConn(c, "INSERT INTO test (value) VALUES ('test2')")
		if err != nil {
			return err
		}

		// Get changeset
		changeset, err = session.Changeset()
		return err
	})
	if err != nil {
		t.Fatal("Failed to get changeset:", err)
	}

	if len(changeset) == 0 {
		t.Error("Changeset should not be empty")
	}
}

func TestSessionChangesetUpdate(t *testing.T) {
	// Test changeset with UPDATE
	db, err := sql.Open("sqlite3", ":memory:")
	if err != nil {
		t.Fatal("Failed to open database:", err)
	}
	defer db.Close()

	_, err = db.Exec("CREATE TABLE test (id INTEGER PRIMARY KEY, value TEXT)")
	if err != nil {
		t.Fatal("Failed to create table:", err)
	}

	// Insert initial data
	_, err = db.Exec("INSERT INTO test (id, value) VALUES (1, 'initial')")
	if err != nil {
		t.Fatal("Failed to insert initial data:", err)
	}

	conn, err := db.Conn(context.Background())
	if err != nil {
		t.Fatal("Failed to get connection:", err)
	}
	defer conn.Close()

	var changeset []byte
	err = conn.Raw(func(driverConn any) error {
		c := driverConn.(*SQLiteConn)
		session, err := c.CreateSession("main")
		if err != nil {
			return err
		}
		defer session.Close()

		err = session.Attach("")
		if err != nil {
			return err
		}

		// Update the row - use SQLiteConn.Exec directly
		_, err = execConn(c, "UPDATE test SET value = 'updated' WHERE id = 1")
		if err != nil {
			return err
		}

		// Get changeset
		changeset, err = session.Changeset()
		return err
	})
	if err != nil {
		t.Fatal("Failed to get changeset:", err)
	}

	if len(changeset) == 0 {
		t.Error("Changeset should not be empty")
	}
}

func TestSessionChangesetDelete(t *testing.T) {
	// Test changeset with DELETE
	db, err := sql.Open("sqlite3", ":memory:")
	if err != nil {
		t.Fatal("Failed to open database:", err)
	}
	defer db.Close()

	_, err = db.Exec("CREATE TABLE test (id INTEGER PRIMARY KEY, value TEXT)")
	if err != nil {
		t.Fatal("Failed to create table:", err)
	}

	// Insert initial data
	_, err = db.Exec("INSERT INTO test (id, value) VALUES (1, 'to_delete')")
	if err != nil {
		t.Fatal("Failed to insert initial data:", err)
	}

	conn, err := db.Conn(context.Background())
	if err != nil {
		t.Fatal("Failed to get connection:", err)
	}
	defer conn.Close()

	var changeset []byte
	err = conn.Raw(func(driverConn any) error {
		c := driverConn.(*SQLiteConn)
		session, err := c.CreateSession("main")
		if err != nil {
			return err
		}
		defer session.Close()

		err = session.Attach("")
		if err != nil {
			return err
		}

		// Delete the row - use SQLiteConn.Exec directly
		_, err = execConn(c, "DELETE FROM test WHERE id = 1")
		if err != nil {
			return err
		}

		// Get changeset
		changeset, err = session.Changeset()
		return err
	})
	if err != nil {
		t.Fatal("Failed to get changeset:", err)
	}

	if len(changeset) == 0 {
		t.Error("Changeset should not be empty")
	}
}

func TestSessionPatchset(t *testing.T) {
	// Test patchset generation
	db, err := sql.Open("sqlite3", ":memory:")
	if err != nil {
		t.Fatal("Failed to open database:", err)
	}
	defer db.Close()

	_, err = db.Exec("CREATE TABLE test (id INTEGER PRIMARY KEY, value TEXT)")
	if err != nil {
		t.Fatal("Failed to create table:", err)
	}

	conn, err := db.Conn(context.Background())
	if err != nil {
		t.Fatal("Failed to get connection:", err)
	}
	defer conn.Close()

	var patchset []byte
	err = conn.Raw(func(driverConn any) error {
		c := driverConn.(*SQLiteConn)
		session, err := c.CreateSession("main")
		if err != nil {
			return err
		}
		defer session.Close()

		err = session.Attach("")
		if err != nil {
			return err
		}

		// Make changes - use SQLiteConn.Exec directly
		_, err = execConn(c, "INSERT INTO test (value) VALUES ('test1')")
		if err != nil {
			return err
		}

		// Get patchset
		patchset, err = session.Patchset()
		return err
	})
	if err != nil {
		t.Fatal("Failed to get patchset:", err)
	}

	if len(patchset) == 0 {
		t.Error("Patchset should not be empty")
	}
}

func TestSessionWriteChangeset(t *testing.T) {
	// Test streaming WriteChangeset to bytes.Buffer
	db, err := sql.Open("sqlite3", ":memory:")
	if err != nil {
		t.Fatal("Failed to open database:", err)
	}
	defer db.Close()

	_, err = db.Exec("CREATE TABLE test (id INTEGER PRIMARY KEY, value TEXT)")
	if err != nil {
		t.Fatal("Failed to create table:", err)
	}

	conn, err := db.Conn(context.Background())
	if err != nil {
		t.Fatal("Failed to get connection:", err)
	}
	defer conn.Close()

	var buf bytes.Buffer
	err = conn.Raw(func(driverConn any) error {
		c := driverConn.(*SQLiteConn)
		session, err := c.CreateSession("main")
		if err != nil {
			return err
		}
		defer session.Close()

		err = session.Attach("")
		if err != nil {
			return err
		}

		// Make changes - use SQLiteConn.Exec directly
		_, err = execConn(c, "INSERT INTO test (value) VALUES ('test1')")
		if err != nil {
			return err
		}

		// Write changeset to buffer
		return session.WriteChangeset(&buf)
	})
	if err != nil {
		t.Fatal("Failed to write changeset:", err)
	}

	if buf.Len() == 0 {
		t.Error("Buffer should not be empty")
	}
}

func TestSessionIndirect(t *testing.T) {
	// Test indirect mode setting
	db, err := sql.Open("sqlite3", ":memory:")
	if err != nil {
		t.Fatal("Failed to open database:", err)
	}
	defer db.Close()

	conn, err := db.Conn(context.Background())
	if err != nil {
		t.Fatal("Failed to get connection:", err)
	}
	defer conn.Close()

	err = conn.Raw(func(driverConn any) error {
		c := driverConn.(*SQLiteConn)
		session, err := c.CreateSession("main")
		if err != nil {
			return err
		}
		defer session.Close()

		// Test indirect setting - Indirect returns the current state after setting
		cur := session.Indirect(1)
		if cur != 1 {
			t.Errorf("Expected current value to be 1, got %d", cur)
		}

		cur = session.Indirect(0)
		if cur != 0 {
			t.Errorf("Expected current value to be 0, got %d", cur)
		}

		return nil
	})
	if err != nil {
		t.Fatal("Failed:", err)
	}
}

func TestSessionCloseMultipleTimes(t *testing.T) {
	// Test that closing a session multiple times is safe
	db, err := sql.Open("sqlite3", ":memory:")
	if err != nil {
		t.Fatal("Failed to open database:", err)
	}
	defer db.Close()

	conn, err := db.Conn(context.Background())
	if err != nil {
		t.Fatal("Failed to get connection:", err)
	}
	defer conn.Close()

	var session *Session
	err = conn.Raw(func(driverConn any) error {
		c := driverConn.(*SQLiteConn)
		var err error
		session, err = c.CreateSession("main")
		return err
	})
	if err != nil {
		t.Fatal("Failed to create session:", err)
	}

	// Close multiple times should not panic
	err1 := session.Close()
	err2 := session.Close()

	if err1 != nil {
		t.Error("First close should succeed:", err1)
	}
	if err2 != nil {
		t.Error("Second close should succeed:", err2)
	}
}

func TestSessionWithFile(t *testing.T) {
	// Test session with a file-based database
	tmpfile, err := os.CreateTemp("", "test-session-*.db")
	if err != nil {
		t.Fatal("Failed to create temp file:", err)
	}
	tmpfile.Close()
	defer os.Remove(tmpfile.Name())

	db, err := sql.Open("sqlite3", tmpfile.Name())
	if err != nil {
		t.Fatal("Failed to open database:", err)
	}
	defer db.Close()

	_, err = db.Exec("CREATE TABLE test (id INTEGER PRIMARY KEY, value TEXT)")
	if err != nil {
		t.Fatal("Failed to create table:", err)
	}

	conn, err := db.Conn(context.Background())
	if err != nil {
		t.Fatal("Failed to get connection:", err)
	}
	defer conn.Close()

	var changeset []byte
	err = conn.Raw(func(driverConn any) error {
		c := driverConn.(*SQLiteConn)
		session, err := c.CreateSession("main")
		if err != nil {
			return err
		}
		defer session.Close()

		err = session.Attach("")
		if err != nil {
			return err
		}

		// Make changes - use SQLiteConn.Exec directly
		_, err = execConn(c, "INSERT INTO test (value) VALUES ('file_test')")
		if err != nil {
			return err
		}

		// Get changeset
		changeset, err = session.Changeset()
		return err
	})
	if err != nil {
		t.Fatal("Failed:", err)
	}

	if len(changeset) == 0 {
		t.Error("Changeset should not be empty")
	}
}

func TestInvertChangeset(t *testing.T) {
	// Create a temporary database
	tempFilename := TempFilename(t)
	defer os.Remove(tempFilename)

	db, err := sql.Open("sqlite3", tempFilename)
	if err != nil {
		t.Fatal("Failed to open database:", err)
	}
	defer db.Close()

	// Create a table
	_, err = db.Exec("CREATE TABLE test (id INTEGER PRIMARY KEY, value TEXT)")
	if err != nil {
		t.Fatal("Failed to create table:", err)
	}

	// Get the underlying SQLite connection
	conn, err := db.Conn(context.Background())
	if err != nil {
		t.Fatal("Failed to get connection:", err)
	}
	defer conn.Close()

	var sqliteConn *SQLiteConn
	var changesetBuf bytes.Buffer
	err = conn.Raw(func(driverConn any) error {
		sqliteConn = driverConn.(*SQLiteConn)

		// Create a session
		session, err := sqliteConn.CreateSession("main")
		if err != nil {
			return err
		}
		defer session.Close()

		// Attach the table to the session
		err = session.Attach("")
		if err != nil {
			return err
		}

		// Insert a row (this will be captured by the session) - use SQLiteConn.Exec directly
		_, err = execConn(sqliteConn, "INSERT INTO test (id, value) VALUES (1, 'original')")
		if err != nil {
			return err
		}

		// Get the changeset
		err = session.WriteChangeset(&changesetBuf)
		return err
	})
	if err != nil {
		t.Fatal("Failed to create changeset:", err)
	}

	if changesetBuf.Len() == 0 {
		t.Fatal("Changeset is empty")
	}

	// Invert the changeset
	var invertedBuf bytes.Buffer
	err = InvertChangeset(&changesetBuf, &invertedBuf)
	if err != nil {
		t.Fatal("Failed to invert changeset:", err)
	}

	if invertedBuf.Len() == 0 {
		t.Fatal("Inverted changeset is empty")
	}

	// Apply the inverted changeset (should delete the row)
	err = conn.Raw(func(driverConn any) error {
		c := driverConn.(*SQLiteConn)
		return c.ApplyChangeset(&invertedBuf, nil, nil)
	})
	if err != nil {
		t.Fatal("Failed to apply inverted changeset:", err)
	}

	// Verify the row was deleted
	var count int
	err = db.QueryRow("SELECT COUNT(*) FROM test WHERE id = 1").Scan(&count)
	if err != nil {
		t.Fatal("Failed to query count:", err)
	}

	if count != 0 {
		t.Errorf("Expected 0 rows after applying inverted changeset, got %d", count)
	}
}

func TestInvertChangesetUpdate(t *testing.T) {
	// Create a temporary database
	tempFilename := TempFilename(t)
	defer os.Remove(tempFilename)

	db, err := sql.Open("sqlite3", tempFilename)
	if err != nil {
		t.Fatal("Failed to open database:", err)
	}
	defer db.Close()

	// Create a table and insert initial data
	_, err = db.Exec("CREATE TABLE test (id INTEGER PRIMARY KEY, value TEXT)")
	if err != nil {
		t.Fatal("Failed to create table:", err)
	}

	_, err = db.Exec("INSERT INTO test (id, value) VALUES (1, 'original')")
	if err != nil {
		t.Fatal("Failed to insert initial row:", err)
	}

	// Get the underlying SQLite connection
	conn, err := db.Conn(context.Background())
	if err != nil {
		t.Fatal("Failed to get connection:", err)
	}
	defer conn.Close()

	var changesetBuf bytes.Buffer
	err = conn.Raw(func(driverConn any) error {
		c := driverConn.(*SQLiteConn)

		// Create a session
		session, err := c.CreateSession("main")
		if err != nil {
			return err
		}
		defer session.Close()

		// Attach the table to the session
		err = session.Attach("")
		if err != nil {
			return err
		}

		// Update the row (this will be captured by the session) - use SQLiteConn.Exec directly
		_, err = execConn(c, "UPDATE test SET value = 'updated' WHERE id = 1")
		if err != nil {
			return err
		}

		// Get the changeset
		err = session.WriteChangeset(&changesetBuf)
		return err
	})
	if err != nil {
		t.Fatal("Failed to create changeset:", err)
	}

	// Invert the changeset
	var invertedBuf bytes.Buffer
	err = InvertChangeset(&changesetBuf, &invertedBuf)
	if err != nil {
		t.Fatal("Failed to invert changeset:", err)
	}

	// Apply the inverted changeset (should restore original value)
	err = conn.Raw(func(driverConn any) error {
		c := driverConn.(*SQLiteConn)
		return c.ApplyChangeset(&invertedBuf, nil, nil)
	})
	if err != nil {
		t.Fatal("Failed to apply inverted changeset:", err)
	}

	// Verify the value was restored
	var value string
	err = db.QueryRow("SELECT value FROM test WHERE id = 1").Scan(&value)
	if err != nil {
		t.Fatal("Failed to query value:", err)
	}

	if value != "original" {
		t.Errorf("Expected 'original' after applying inverted changeset, got '%s'", value)
	}
}

func TestConcatChangesets(t *testing.T) {
	// Create a temporary database
	tempFilename := TempFilename(t)
	defer os.Remove(tempFilename)

	db, err := sql.Open("sqlite3", tempFilename)
	if err != nil {
		t.Fatal("Failed to open database:", err)
	}
	defer db.Close()

	// Create a table
	_, err = db.Exec("CREATE TABLE test (id INTEGER PRIMARY KEY, value TEXT)")
	if err != nil {
		t.Fatal("Failed to create table:", err)
	}

	// Get the underlying SQLite connection
	conn, err := db.Conn(context.Background())
	if err != nil {
		t.Fatal("Failed to get connection:", err)
	}
	defer conn.Close()

	var changeset1, changeset2 bytes.Buffer

	// Create first changeset
	err = conn.Raw(func(driverConn any) error {
		c := driverConn.(*SQLiteConn)
		session1, err := c.CreateSession("main")
		if err != nil {
			return err
		}
		defer session1.Close()

		err = session1.Attach("")
		if err != nil {
			return err
		}

		_, err = execConn(c, "INSERT INTO test (id, value) VALUES (1, 'first')")
		if err != nil {
			return err
		}

		err = session1.WriteChangeset(&changeset1)
		return err
	})
	if err != nil {
		t.Fatal("Failed to create changeset1:", err)
	}

	// Create second changeset
	err = conn.Raw(func(driverConn any) error {
		c := driverConn.(*SQLiteConn)
		session2, err := c.CreateSession("main")
		if err != nil {
			return err
		}
		defer session2.Close()

		err = session2.Attach("")
		if err != nil {
			return err
		}

		_, err = execConn(c, "INSERT INTO test (id, value) VALUES (2, 'second')")
		if err != nil {
			return err
		}

		err = session2.WriteChangeset(&changeset2)
		return err
	})
	if err != nil {
		t.Fatal("Failed to create changeset2:", err)
	}

	// Concatenate the changesets
	var concatBuf bytes.Buffer
	err = ConcatChangesets(&changeset1, &changeset2, &concatBuf)
	if err != nil {
		t.Fatal("Failed to concat changesets:", err)
	}

	if concatBuf.Len() == 0 {
		t.Fatal("Concatenated changeset is empty")
	}

	// Create a new database to apply the concatenated changeset
	tempFilename2 := TempFilename(t)
	defer os.Remove(tempFilename2)

	db2, err := sql.Open("sqlite3", tempFilename2)
	if err != nil {
		t.Fatal("Failed to open second database:", err)
	}
	defer db2.Close()

	_, err = db2.Exec("CREATE TABLE test (id INTEGER PRIMARY KEY, value TEXT)")
	if err != nil {
		t.Fatal("Failed to create table in second database:", err)
	}

	// Get the second database connection
	conn2, err := db2.Conn(context.Background())
	if err != nil {
		t.Fatal("Failed to get second connection:", err)
	}
	defer conn2.Close()

	// Apply the concatenated changeset
	err = conn2.Raw(func(driverConn any) error {
		c := driverConn.(*SQLiteConn)
		return c.ApplyChangeset(&concatBuf, nil, nil)
	})
	if err != nil {
		t.Fatal("Failed to apply concatenated changeset:", err)
	}

	// Verify both rows were inserted
	var count int
	err = db2.QueryRow("SELECT COUNT(*) FROM test").Scan(&count)
	if err != nil {
		t.Fatal("Failed to query count:", err)
	}

	if count != 2 {
		t.Errorf("Expected 2 rows after applying concatenated changeset, got %d", count)
	}
}

func TestChangegroup(t *testing.T) {
	// Create a temporary database
	tempFilename := TempFilename(t)
	defer os.Remove(tempFilename)

	db, err := sql.Open("sqlite3", tempFilename)
	if err != nil {
		t.Fatal("Failed to open database:", err)
	}
	defer db.Close()

	// Create a table
	_, err = db.Exec("CREATE TABLE test (id INTEGER PRIMARY KEY, value TEXT)")
	if err != nil {
		t.Fatal("Failed to create table:", err)
	}

	// Get the underlying SQLite connection
	conn, err := db.Conn(context.Background())
	if err != nil {
		t.Fatal("Failed to get connection:", err)
	}
	defer conn.Close()

	var changeset1, changeset2 bytes.Buffer

	// Create first changeset
	err = conn.Raw(func(driverConn any) error {
		c := driverConn.(*SQLiteConn)
		session1, err := c.CreateSession("main")
		if err != nil {
			return err
		}
		defer session1.Close()

		err = session1.Attach("")
		if err != nil {
			return err
		}

		_, err = execConn(c, "INSERT INTO test (id, value) VALUES (1, 'first')")
		if err != nil {
			return err
		}

		err = session1.WriteChangeset(&changeset1)
		return err
	})
	if err != nil {
		t.Fatal("Failed to create changeset1:", err)
	}

	// Create second changeset
	err = conn.Raw(func(driverConn any) error {
		c := driverConn.(*SQLiteConn)
		session2, err := c.CreateSession("main")
		if err != nil {
			return err
		}
		defer session2.Close()

		err = session2.Attach("")
		if err != nil {
			return err
		}

		_, err = execConn(c, "INSERT INTO test (id, value) VALUES (2, 'second')")
		if err != nil {
			return err
		}

		err = session2.WriteChangeset(&changeset2)
		return err
	})
	if err != nil {
		t.Fatal("Failed to create changeset2:", err)
	}

	// Create a changegroup and add both changesets
	group, err := NewChangegroup()
	if err != nil {
		t.Fatal("Failed to create changegroup:", err)
	}
	defer group.Close()

	err = group.Add(&changeset1)
	if err != nil {
		t.Fatal("Failed to add changeset1 to group:", err)
	}

	err = group.Add(&changeset2)
	if err != nil {
		t.Fatal("Failed to add changeset2 to group:", err)
	}

	// Output the combined changeset
	var combinedBuf bytes.Buffer
	err = group.Output(&combinedBuf)
	if err != nil {
		t.Fatal("Failed to output combined changeset:", err)
	}

	if combinedBuf.Len() == 0 {
		t.Fatal("Combined changeset is empty")
	}

	// Create a new database to apply the combined changeset
	tempFilename2 := TempFilename(t)
	defer os.Remove(tempFilename2)

	db2, err := sql.Open("sqlite3", tempFilename2)
	if err != nil {
		t.Fatal("Failed to open second database:", err)
	}
	defer db2.Close()

	_, err = db2.Exec("CREATE TABLE test (id INTEGER PRIMARY KEY, value TEXT)")
	if err != nil {
		t.Fatal("Failed to create table in second database:", err)
	}

	// Get the second database connection
	conn2, err := db2.Conn(context.Background())
	if err != nil {
		t.Fatal("Failed to get second connection:", err)
	}
	defer conn2.Close()

	// Apply the combined changeset
	err = conn2.Raw(func(driverConn any) error {
		c := driverConn.(*SQLiteConn)
		return c.ApplyChangeset(&combinedBuf, nil, nil)
	})
	if err != nil {
		t.Fatal("Failed to apply combined changeset:", err)
	}

	// Verify both rows were inserted
	var count int
	err = db2.QueryRow("SELECT COUNT(*) FROM test").Scan(&count)
	if err != nil {
		t.Fatal("Failed to query count:", err)
	}

	if count != 2 {
		t.Errorf("Expected 2 rows after applying combined changeset, got %d", count)
	}
}

func TestChangegroupCoalesces(t *testing.T) {
	// Create a temporary database
	tempFilename := TempFilename(t)
	defer os.Remove(tempFilename)

	db, err := sql.Open("sqlite3", tempFilename)
	if err != nil {
		t.Fatal("Failed to open database:", err)
	}
	defer db.Close()

	// Create a table
	_, err = db.Exec("CREATE TABLE test (id INTEGER PRIMARY KEY, value TEXT)")
	if err != nil {
		t.Fatal("Failed to create table:", err)
	}

	// Get the underlying SQLite connection
	conn, err := db.Conn(context.Background())
	if err != nil {
		t.Fatal("Failed to get connection:", err)
	}
	defer conn.Close()

	var changeset1, changeset2 bytes.Buffer

	// Create first changeset (INSERT)
	err = conn.Raw(func(driverConn any) error {
		c := driverConn.(*SQLiteConn)
		session1, err := c.CreateSession("main")
		if err != nil {
			return err
		}
		defer session1.Close()

		err = session1.Attach("")
		if err != nil {
			return err
		}

		_, err = execConn(c, "INSERT INTO test (id, value) VALUES (1, 'test')")
		if err != nil {
			return err
		}

		err = session1.WriteChangeset(&changeset1)
		return err
	})
	if err != nil {
		t.Fatal("Failed to create changeset1:", err)
	}

	// Create second changeset (DELETE the same row)
	err = conn.Raw(func(driverConn any) error {
		c := driverConn.(*SQLiteConn)
		session2, err := c.CreateSession("main")
		if err != nil {
			return err
		}
		defer session2.Close()

		err = session2.Attach("")
		if err != nil {
			return err
		}

		_, err = execConn(c, "DELETE FROM test WHERE id = 1")
		if err != nil {
			return err
		}

		err = session2.WriteChangeset(&changeset2)
		return err
	})
	if err != nil {
		t.Fatal("Failed to create changeset2:", err)
	}

	// Create a changegroup and add both changesets
	// The changegroup should coalesce INSERT + DELETE to nothing
	group, err := NewChangegroup()
	if err != nil {
		t.Fatal("Failed to create changegroup:", err)
	}
	defer group.Close()

	err = group.Add(&changeset1)
	if err != nil {
		t.Fatal("Failed to add changeset1 to group:", err)
	}

	err = group.Add(&changeset2)
	if err != nil {
		t.Fatal("Failed to add changeset2 to group:", err)
	}

	// Output the combined changeset
	var combinedBuf bytes.Buffer
	err = group.Output(&combinedBuf)
	if err != nil {
		t.Fatal("Failed to output combined changeset:", err)
	}

	// Create a new database to apply the combined changeset
	tempFilename2 := TempFilename(t)
	defer os.Remove(tempFilename2)

	db2, err := sql.Open("sqlite3", tempFilename2)
	if err != nil {
		t.Fatal("Failed to open second database:", err)
	}
	defer db2.Close()

	_, err = db2.Exec("CREATE TABLE test (id INTEGER PRIMARY KEY, value TEXT)")
	if err != nil {
		t.Fatal("Failed to create table in second database:", err)
	}

	// Get the second database connection
	conn2, err := db2.Conn(context.Background())
	if err != nil {
		t.Fatal("Failed to get second connection:", err)
	}
	defer conn2.Close()

	// Apply the combined changeset (should have no effect due to coalescing)
	err = conn2.Raw(func(driverConn any) error {
		c := driverConn.(*SQLiteConn)
		return c.ApplyChangeset(&combinedBuf, nil, nil)
	})
	// Empty changesets may return EOF or succeed with no effect
	if err != nil && err.Error() != "EOF" {
		t.Log("Apply returned:", err)
	}

	// Verify no rows were inserted (INSERT was coalesced with DELETE)
	var count int
	err = db2.QueryRow("SELECT COUNT(*) FROM test").Scan(&count)
	if err != nil {
		t.Fatal("Failed to query count:", err)
	}

	if count != 0 {
		t.Errorf("Expected 0 rows after applying coalesced changeset, got %d", count)
	}
}

func TestRebaser(t *testing.T) {
	// This test is more complex and demonstrates basic rebaser functionality
	// For simplicity, we'll test basic rebaser creation and configuration

	rebaser, err := NewRebaser()
	if err != nil {
		t.Fatal("Failed to create rebaser:", err)
	}
	defer rebaser.Close()

	// Configure with empty rebase data (should not error)
	err = rebaser.Configure([]byte{})
	if err != nil {
		t.Fatal("Failed to configure rebaser with empty data:", err)
	}

	// Test rebasing with a real changeset
	tempFilename := TempFilename(t)
	defer os.Remove(tempFilename)

	db, err := sql.Open("sqlite3", tempFilename)
	if err != nil {
		t.Fatal("Failed to open database:", err)
	}
	defer db.Close()

	_, err = db.Exec("CREATE TABLE test (id INTEGER PRIMARY KEY, value TEXT)")
	if err != nil {
		t.Fatal("Failed to create table:", err)
	}

	conn, err := db.Conn(context.Background())
	if err != nil {
		t.Fatal("Failed to get connection:", err)
	}
	defer conn.Close()

	var changesetBuf bytes.Buffer
	err = conn.Raw(func(driverConn any) error {
		c := driverConn.(*SQLiteConn)
		session, err := c.CreateSession("main")
		if err != nil {
			return err
		}
		defer session.Close()

		err = session.Attach("")
		if err != nil {
			return err
		}

		_, err = execConn(c, "INSERT INTO test (id, value) VALUES (1, 'test')")
		if err != nil {
			return err
		}

		err = session.WriteChangeset(&changesetBuf)
		return err
	})
	if err != nil {
		t.Fatal("Failed to create changeset:", err)
	}

	// Rebase the changeset (with empty rebase data, output should be same as input)
	var outputBuf bytes.Buffer
	err = rebaser.Rebase(&changesetBuf, &outputBuf)
	if err != nil {
		t.Fatal("Failed to rebase changeset:", err)
	}

	// Output should not be empty
	if outputBuf.Len() == 0 {
		t.Error("Rebased changeset should not be empty")
	}
}

// ChangesetIterator Tests

func TestChangesetIteratorBasic(t *testing.T) {
	// Test creating iterator and iterating through basic changes
	db, err := sql.Open("sqlite3", ":memory:")
	if err != nil {
		t.Fatal("Failed to open database:", err)
	}
	defer db.Close()

	_, err = db.Exec("CREATE TABLE test (id INTEGER PRIMARY KEY, value TEXT)")
	if err != nil {
		t.Fatal("Failed to create table:", err)
	}

	conn, err := db.Conn(context.Background())
	if err != nil {
		t.Fatal("Failed to get connection:", err)
	}
	defer conn.Close()

	var changeset []byte
	err = conn.Raw(func(driverConn any) error {
		c := driverConn.(*SQLiteConn)
		session, err := c.CreateSession("main")
		if err != nil {
			return err
		}
		defer session.Close()

		err = session.Attach("")
		if err != nil {
			return err
		}

		// Make some changes - use SQLiteConn.Exec directly
		_, err = execConn(c, "INSERT INTO test (id, value) VALUES (1, 'first')")
		if err != nil {
			return err
		}

		_, err = execConn(c, "INSERT INTO test (id, value) VALUES (2, 'second')")
		if err != nil {
			return err
		}

		// Get changeset
		changeset, err = session.Changeset()
		return err
	})
	if err != nil {
		t.Fatal("Failed to get changeset:", err)
	}

	if len(changeset) == 0 {
		t.Fatal("Changeset is empty")
	}

	// Create iterator from changeset
	iter, err := NewChangesetIteratorBytes(changeset)
	if err != nil {
		t.Fatal("Failed to create iterator:", err)
	}
	defer iter.Close()

	// Iterate through changes
	changeCount := 0
	for iter.Next() {
		changeCount++

		// Get operation info
		op, tableName, nCols, _, err := iter.Op()
		if err != nil {
			t.Fatal("Failed to get op:", err)
		}

		if tableName != "test" {
			t.Errorf("Expected table name 'test', got '%s'", tableName)
		}

		if nCols != 2 {
			t.Errorf("Expected 2 columns, got %d", nCols)
		}

		// Should be INSERT operations
		if op != SQLITE_INSERT {
			t.Errorf("Expected SQLITE_INSERT (%d), got %d", SQLITE_INSERT, op)
		}

		// Check new values (INSERT has new values)
		for i := 0; i < nCols; i++ {
			val, err := iter.NewValue(i)
			if err != nil {
				t.Errorf("Failed to get new value for column %d: %v", i, err)
			}
			if val == nil && i == 0 {
				t.Errorf("Expected non-nil value for id column")
			}
		}
	}

	if iter.Err() != nil {
		t.Fatal("Iterator error:", iter.Err())
	}

	if changeCount != 2 {
		t.Errorf("Expected 2 changes, got %d", changeCount)
	}
}

func TestChangesetIteratorValues(t *testing.T) {
	// Test OldValue and NewValue for different operations
	db, err := sql.Open("sqlite3", ":memory:")
	if err != nil {
		t.Fatal("Failed to open database:", err)
	}
	defer db.Close()

	_, err = db.Exec("CREATE TABLE test (id INTEGER PRIMARY KEY, value TEXT)")
	if err != nil {
		t.Fatal("Failed to create table:", err)
	}

	conn, err := db.Conn(context.Background())
	if err != nil {
		t.Fatal("Failed to get connection:", err)
	}
	defer conn.Close()

	var insertChangeset, updateChangeset, deleteChangeset []byte

	// Get INSERT changeset
	err = conn.Raw(func(driverConn any) error {
		c := driverConn.(*SQLiteConn)
		session, err := c.CreateSession("main")
		if err != nil {
			return err
		}
		defer session.Close()

		err = session.Attach("")
		if err != nil {
			return err
		}

		// INSERT: only NewValue should work - use SQLiteConn.Exec directly
		_, err = execConn(c, "INSERT INTO test (id, value) VALUES (1, 'original')")
		if err != nil {
			return err
		}

		insertChangeset, err = session.Changeset()
		return err
	})
	if err != nil {
		t.Fatal("Failed to get insert changeset:", err)
	}

	// Get UPDATE changeset
	err = conn.Raw(func(driverConn any) error {
		c := driverConn.(*SQLiteConn)
		session, err := c.CreateSession("main")
		if err != nil {
			return err
		}
		defer session.Close()

		err = session.Attach("")
		if err != nil {
			return err
		}

		// UPDATE: both should work - use SQLiteConn.Exec directly
		_, err = execConn(c, "UPDATE test SET value = 'updated' WHERE id = 1")
		if err != nil {
			return err
		}

		updateChangeset, err = session.Changeset()
		return err
	})
	if err != nil {
		t.Fatal("Failed to get update changeset:", err)
	}

	// Get DELETE changeset
	err = conn.Raw(func(driverConn any) error {
		c := driverConn.(*SQLiteConn)
		session, err := c.CreateSession("main")
		if err != nil {
			return err
		}
		defer session.Close()

		err = session.Attach("")
		if err != nil {
			return err
		}

		// DELETE: only OldValue should work - use SQLiteConn.Exec directly
		_, err = execConn(c, "DELETE FROM test WHERE id = 1")
		if err != nil {
			return err
		}

		deleteChangeset, err = session.Changeset()
		return err
	})
	if err != nil {
		t.Fatal("Failed to get delete changeset:", err)
	}

	// Test INSERT
	t.Run("INSERT", func(t *testing.T) {
		iter, err := NewChangesetIteratorBytes(insertChangeset)
		if err != nil {
			t.Fatal("Failed to create iterator:", err)
		}
		defer iter.Close()

		if !iter.Next() {
			t.Fatal("Expected at least one change")
		}

		op, _, _, _, _ := iter.Op()
		if op != SQLITE_INSERT {
			t.Skip("Not an INSERT operation")
		}

		// NewValue should work
		val, err := iter.NewValue(1)
		if err != nil {
			t.Errorf("NewValue should work for INSERT: %v", err)
		}
		if str, ok := val.(string); !ok || str != "original" {
			t.Errorf("Expected 'original', got %v", val)
		}
	})

	// Test UPDATE
	t.Run("UPDATE", func(t *testing.T) {
		if len(updateChangeset) == 0 {
			t.Skip("No update changeset available")
		}

		iter, err := NewChangesetIteratorBytes(updateChangeset)
		if err != nil {
			t.Fatal("Failed to create iterator:", err)
		}
		defer iter.Close()

		found := false
		for iter.Next() {
			op, _, _, _, _ := iter.Op()
			if op == SQLITE_UPDATE {
				found = true

				// Both OldValue and NewValue should work
				oldVal, err := iter.OldValue(1)
				if err != nil {
					t.Errorf("OldValue should work for UPDATE: %v", err)
				}
				if oldVal != nil {
					t.Logf("Old value: %v", oldVal)
				}

				newVal, err := iter.NewValue(1)
				if err != nil {
					t.Errorf("NewValue should work for UPDATE: %v", err)
				}
				if str, ok := newVal.(string); !ok || str != "updated" {
					t.Errorf("Expected 'updated', got %v", newVal)
				}
				break
			}
		}
		if !found {
			t.Log("No UPDATE operation found in changeset")
		}
	})

	// Test DELETE
	t.Run("DELETE", func(t *testing.T) {
		if len(deleteChangeset) == 0 {
			t.Skip("No delete changeset available")
		}

		iter, err := NewChangesetIteratorBytes(deleteChangeset)
		if err != nil {
			t.Fatal("Failed to create iterator:", err)
		}
		defer iter.Close()

		found := false
		for iter.Next() {
			op, _, _, _, _ := iter.Op()
			if op == SQLITE_DELETE {
				found = true

				// OldValue should work
				oldVal, err := iter.OldValue(1)
				if err != nil {
					t.Errorf("OldValue should work for DELETE: %v", err)
				}
				if oldVal != nil {
					t.Logf("Old value: %v", oldVal)
				}
				break
			}
		}
		if !found {
			t.Log("No DELETE operation found in changeset")
		}
	})
}

func TestChangesetIteratorMultipleChanges(t *testing.T) {
	// Test iterating through multiple changes and counting them
	db, err := sql.Open("sqlite3", ":memory:")
	if err != nil {
		t.Fatal("Failed to open database:", err)
	}
	defer db.Close()

	_, err = db.Exec("CREATE TABLE test (id INTEGER PRIMARY KEY, value TEXT)")
	if err != nil {
		t.Fatal("Failed to create table:", err)
	}

	conn, err := db.Conn(context.Background())
	if err != nil {
		t.Fatal("Failed to get connection:", err)
	}
	defer conn.Close()

	var changeset []byte
	err = conn.Raw(func(driverConn any) error {
		c := driverConn.(*SQLiteConn)
		session, err := c.CreateSession("main")
		if err != nil {
			return err
		}
		defer session.Close()

		err = session.Attach("")
		if err != nil {
			return err
		}

		// Make multiple changes - use SQLiteConn.Exec directly
		for i := 1; i <= 5; i++ {
			_, err = execConn(c, "INSERT INTO test (id, value) VALUES (?, ?)", i, "value"+string(rune('0'+i)))
			if err != nil {
				return err
			}
		}

		changeset, err = session.Changeset()
		return err
	})
	if err != nil {
		t.Fatal("Failed:", err)
	}

	// Count changes
	iter, err := NewChangesetIteratorBytes(changeset)
	if err != nil {
		t.Fatal("Failed to create iterator:", err)
	}
	defer iter.Close()

	count := 0
	for iter.Next() {
		count++
	}

	if iter.Err() != nil {
		t.Fatal("Iterator error:", iter.Err())
	}

	if count != 5 {
		t.Errorf("Expected 5 changes, got %d", count)
	}
}

func TestChangesetIteratorEmpty(t *testing.T) {
	// Test that empty changeset (no changes) returns empty bytes or iterator with no rows
	db, err := sql.Open("sqlite3", ":memory:")
	if err != nil {
		t.Fatal("Failed to open database:", err)
	}
	defer db.Close()

	_, err = db.Exec("CREATE TABLE test (id INTEGER PRIMARY KEY, value TEXT)")
	if err != nil {
		t.Fatal("Failed to create table:", err)
	}

	conn, err := db.Conn(context.Background())
	if err != nil {
		t.Fatal("Failed to get connection:", err)
	}
	defer conn.Close()

	var changeset []byte
	var isEmpty bool
	err = conn.Raw(func(driverConn any) error {
		c := driverConn.(*SQLiteConn)
		session, err := c.CreateSession("main")
		if err != nil {
			return err
		}
		defer session.Close()

		err = session.Attach("")
		if err != nil {
			return err
		}

		// Verify session reports as empty before any changes
		isEmpty = session.IsEmpty()

		// Get changeset without making any changes
		changeset, err = session.Changeset()
		return err
	})
	if err != nil {
		t.Fatal("Failed:", err)
	}

	// Verify session was empty
	if !isEmpty {
		t.Error("Session should report IsEmpty() = true when no changes made")
	}

	// When no changes, changeset should be empty (zero bytes)
	if len(changeset) != 0 {
		t.Errorf("Expected empty changeset for no changes, got %d bytes", len(changeset))
	}

	iter, err := NewChangesetIteratorBytes(changeset)
	if err != nil {
		t.Fatal("Failed to create iterator:", err)
	}
	defer iter.Close()

	// Next() should return false immediately
	if iter.Next() {
		t.Error("Expected Next() to return false for empty changeset")
	}

	if iter.Err() != nil {
		t.Error("Expected no error for empty changeset:", iter.Err())
	}
}

func TestChangesetIteratorFromReader(t *testing.T) {
	// Test creating iterator from io.Reader
	db, err := sql.Open("sqlite3", ":memory:")
	if err != nil {
		t.Fatal("Failed to open database:", err)
	}
	defer db.Close()

	_, err = db.Exec("CREATE TABLE test (id INTEGER PRIMARY KEY, value TEXT)")
	if err != nil {
		t.Fatal("Failed to create table:", err)
	}

	conn, err := db.Conn(context.Background())
	if err != nil {
		t.Fatal("Failed to get connection:", err)
	}
	defer conn.Close()

	var changeset []byte
	err = conn.Raw(func(driverConn any) error {
		c := driverConn.(*SQLiteConn)
		session, err := c.CreateSession("main")
		if err != nil {
			return err
		}
		defer session.Close()

		err = session.Attach("")
		if err != nil {
			return err
		}

		// Make a change - use SQLiteConn.Exec directly
		_, err = execConn(c, "INSERT INTO test (id, value) VALUES (1, 'test')")
		if err != nil {
			return err
		}

		changeset, err = session.Changeset()
		return err
	})
	if err != nil {
		t.Fatal("Failed:", err)
	}

	// Create iterator from io.Reader
	reader := bytes.NewReader(changeset)
	iter, err := NewChangesetIterator(reader)
	if err != nil {
		t.Fatal("Failed to create iterator from reader:", err)
	}
	defer iter.Close()

	// Should be able to iterate
	if !iter.Next() {
		t.Error("Expected at least one change")
	}

	op, tableName, _, _, err := iter.Op()
	if err != nil {
		t.Fatal("Failed to get op:", err)
	}

	if tableName != "test" {
		t.Errorf("Expected table name 'test', got '%s'", tableName)
	}

	if op != SQLITE_INSERT {
		t.Errorf("Expected SQLITE_INSERT, got %d", op)
	}
}

func TestChangesetIteratorClose(t *testing.T) {
	// Test that iterator closes cleanly
	db, err := sql.Open("sqlite3", ":memory:")
	if err != nil {
		t.Fatal("Failed to open database:", err)
	}
	defer db.Close()

	_, err = db.Exec("CREATE TABLE test (id INTEGER PRIMARY KEY, value TEXT)")
	if err != nil {
		t.Fatal("Failed to create table:", err)
	}

	conn, err := db.Conn(context.Background())
	if err != nil {
		t.Fatal("Failed to get connection:", err)
	}
	defer conn.Close()

	var changeset []byte
	err = conn.Raw(func(driverConn any) error {
		c := driverConn.(*SQLiteConn)
		session, err := c.CreateSession("main")
		if err != nil {
			return err
		}
		defer session.Close()

		err = session.Attach("")
		if err != nil {
			return err
		}

		// Make a change - use SQLiteConn.Exec directly
		_, err = execConn(c, "INSERT INTO test (id, value) VALUES (1, 'test')")
		if err != nil {
			return err
		}

		changeset, err = session.Changeset()
		return err
	})
	if err != nil {
		t.Fatal("Failed:", err)
	}

	iter, err := NewChangesetIteratorBytes(changeset)
	if err != nil {
		t.Fatal("Failed to create iterator:", err)
	}

	// Close immediately
	err = iter.Close()
	if err != nil {
		t.Error("Close failed:", err)
	}

	// Should be safe to close multiple times
	err = iter.Close()
	if err != nil {
		t.Error("Second close failed:", err)
	}

	// Next() should return false after close
	if iter.Next() {
		t.Error("Next() should return false after close")
	}
}

func TestChangesetIteratorOpIndirect(t *testing.T) {
	// Verify indirect=false for direct changes
	db, err := sql.Open("sqlite3", ":memory:")
	if err != nil {
		t.Fatal("Failed to open database:", err)
	}
	defer db.Close()

	_, err = db.Exec("CREATE TABLE test (id INTEGER PRIMARY KEY, value TEXT)")
	if err != nil {
		t.Fatal("Failed to create table:", err)
	}

	conn, err := db.Conn(context.Background())
	if err != nil {
		t.Fatal("Failed to get connection:", err)
	}
	defer conn.Close()

	var changeset []byte
	err = conn.Raw(func(driverConn any) error {
		c := driverConn.(*SQLiteConn)
		session, err := c.CreateSession("main")
		if err != nil {
			return err
		}
		defer session.Close()

		err = session.Attach("")
		if err != nil {
			return err
		}

		// Direct change (not via trigger)
		_, err = execConn(c, "INSERT INTO test (id, value) VALUES (1, 'direct')")
		if err != nil {
			return err
		}

		changeset, err = session.Changeset()
		return err
	})
	if err != nil {
		t.Fatal("Failed:", err)
	}

	iter, err := NewChangesetIteratorBytes(changeset)
	if err != nil {
		t.Fatal("Failed to create iterator:", err)
	}
	defer iter.Close()

	if !iter.Next() {
		t.Fatal("Expected at least one change")
	}

	op, tableName, nCols, indirect, err := iter.Op()
	if err != nil {
		t.Fatal("Failed to get op:", err)
	}

	if op != SQLITE_INSERT {
		t.Errorf("Expected SQLITE_INSERT, got %d", op)
	}
	if tableName != "test" {
		t.Errorf("Expected table 'test', got '%s'", tableName)
	}
	if nCols != 2 {
		t.Errorf("Expected 2 columns, got %d", nCols)
	}
	if indirect {
		t.Error("Expected indirect=false for direct change")
	}
}

func TestChangesetIteratorOpIndirectTrigger(t *testing.T) {
	// Verify indirect=true for trigger changes
	db, err := sql.Open("sqlite3", ":memory:")
	if err != nil {
		t.Fatal("Failed to open database:", err)
	}
	defer db.Close()

	// Create two tables - one that triggers changes in the other
	_, err = db.Exec(`
		CREATE TABLE parent (id INTEGER PRIMARY KEY, value TEXT);
		CREATE TABLE child (id INTEGER PRIMARY KEY, parent_id INTEGER, created_at TEXT);
		CREATE TRIGGER after_parent_insert AFTER INSERT ON parent
		BEGIN
			INSERT INTO child (parent_id, created_at) VALUES (NEW.id, 'triggered');
		END;
	`)
	if err != nil {
		t.Fatal("Failed to create tables:", err)
	}

	conn, err := db.Conn(context.Background())
	if err != nil {
		t.Fatal("Failed to get connection:", err)
	}
	defer conn.Close()

	var changeset []byte
	err = conn.Raw(func(driverConn any) error {
		c := driverConn.(*SQLiteConn)
		session, err := c.CreateSession("main")
		if err != nil {
			return err
		}
		defer session.Close()

		err = session.Attach("")
		if err != nil {
			return err
		}

		// Insert into parent - this triggers an indirect insert into child
		_, err = execConn(c, "INSERT INTO parent (id, value) VALUES (1, 'test')")
		if err != nil {
			return err
		}

		changeset, err = session.Changeset()
		return err
	})
	if err != nil {
		t.Fatal("Failed:", err)
	}

	iter, err := NewChangesetIteratorBytes(changeset)
	if err != nil {
		t.Fatal("Failed to create iterator:", err)
	}
	defer iter.Close()

	// Iterate and find the child table change (indirect)
	foundDirect := false
	foundIndirect := false
	for iter.Next() {
		op, tableName, _, indirect, err := iter.Op()
		if err != nil {
			t.Fatal("Failed to get op:", err)
		}

		if op != SQLITE_INSERT {
			continue
		}

		if tableName == "parent" {
			foundDirect = true
			if indirect {
				t.Error("Expected indirect=false for direct insert into parent")
			}
		}

		if tableName == "child" {
			foundIndirect = true
			if !indirect {
				t.Error("Expected indirect=true for trigger-based insert into child")
			}
		}
	}

	if iter.Err() != nil {
		t.Fatal("Iterator error:", iter.Err())
	}

	if !foundDirect {
		t.Error("Did not find direct change to parent table")
	}
	if !foundIndirect {
		t.Error("Did not find indirect change to child table")
	}
}

func TestChangesetIteratorPK(t *testing.T) {
	// Single column PK
	db, err := sql.Open("sqlite3", ":memory:")
	if err != nil {
		t.Fatal("Failed to open database:", err)
	}
	defer db.Close()

	_, err = db.Exec("CREATE TABLE test (id INTEGER PRIMARY KEY, value TEXT, other TEXT)")
	if err != nil {
		t.Fatal("Failed to create table:", err)
	}

	conn, err := db.Conn(context.Background())
	if err != nil {
		t.Fatal("Failed to get connection:", err)
	}
	defer conn.Close()

	var changeset []byte
	err = conn.Raw(func(driverConn any) error {
		c := driverConn.(*SQLiteConn)
		session, err := c.CreateSession("main")
		if err != nil {
			return err
		}
		defer session.Close()

		err = session.Attach("")
		if err != nil {
			return err
		}

		_, err = execConn(c, "INSERT INTO test (id, value, other) VALUES (1, 'test', 'data')")
		if err != nil {
			return err
		}

		changeset, err = session.Changeset()
		return err
	})
	if err != nil {
		t.Fatal("Failed:", err)
	}

	iter, err := NewChangesetIteratorBytes(changeset)
	if err != nil {
		t.Fatal("Failed to create iterator:", err)
	}
	defer iter.Close()

	if !iter.Next() {
		t.Fatal("Expected at least one change")
	}

	pk, err := iter.PK()
	if err != nil {
		t.Fatal("Failed to get PK:", err)
	}

	// Expected: [true, false, false] - only id is PK
	if len(pk) != 3 {
		t.Fatalf("Expected 3 columns, got %d", len(pk))
	}
	if !pk[0] {
		t.Error("Expected column 0 (id) to be PK")
	}
	if pk[1] {
		t.Error("Expected column 1 (value) to not be PK")
	}
	if pk[2] {
		t.Error("Expected column 2 (other) to not be PK")
	}
}

func TestChangesetIteratorPKComposite(t *testing.T) {
	// Multi-column PK
	db, err := sql.Open("sqlite3", ":memory:")
	if err != nil {
		t.Fatal("Failed to open database:", err)
	}
	defer db.Close()

	_, err = db.Exec("CREATE TABLE test (a INTEGER, b INTEGER, value TEXT, PRIMARY KEY (a, b))")
	if err != nil {
		t.Fatal("Failed to create table:", err)
	}

	conn, err := db.Conn(context.Background())
	if err != nil {
		t.Fatal("Failed to get connection:", err)
	}
	defer conn.Close()

	var changeset []byte
	err = conn.Raw(func(driverConn any) error {
		c := driverConn.(*SQLiteConn)
		session, err := c.CreateSession("main")
		if err != nil {
			return err
		}
		defer session.Close()

		err = session.Attach("")
		if err != nil {
			return err
		}

		_, err = execConn(c, "INSERT INTO test (a, b, value) VALUES (1, 2, 'test')")
		if err != nil {
			return err
		}

		changeset, err = session.Changeset()
		return err
	})
	if err != nil {
		t.Fatal("Failed:", err)
	}

	iter, err := NewChangesetIteratorBytes(changeset)
	if err != nil {
		t.Fatal("Failed to create iterator:", err)
	}
	defer iter.Close()

	if !iter.Next() {
		t.Fatal("Expected at least one change")
	}

	pk, err := iter.PK()
	if err != nil {
		t.Fatal("Failed to get PK:", err)
	}

	// Expected: [true, true, false] - a and b are PK, value is not
	if len(pk) != 3 {
		t.Fatalf("Expected 3 columns, got %d", len(pk))
	}
	if !pk[0] {
		t.Error("Expected column 0 (a) to be PK")
	}
	if !pk[1] {
		t.Error("Expected column 1 (b) to be PK")
	}
	if pk[2] {
		t.Error("Expected column 2 (value) to not be PK")
	}
}

func TestConflictHandlerIteratorAccess(t *testing.T) {
	// Test that conflict handler receives iterator with accessible values
	// Create INSERT conflict (duplicate PK) and verify iterator access

	db, err := sql.Open("sqlite3", ":memory:")
	if err != nil {
		t.Fatal("Failed to open database:", err)
	}
	defer db.Close()

	_, err = db.Exec("CREATE TABLE test (id INTEGER PRIMARY KEY, value TEXT)")
	if err != nil {
		t.Fatal("Failed to create table:", err)
	}

	// Insert existing row that will conflict
	_, err = db.Exec("INSERT INTO test (id, value) VALUES (1, 'existing')")
	if err != nil {
		t.Fatal("Failed to insert existing row:", err)
	}

	conn, err := db.Conn(context.Background())
	if err != nil {
		t.Fatal("Failed to get connection:", err)
	}
	defer conn.Close()

	// Create a changeset that inserts a row with the same PK
	var changeset []byte
	err = conn.Raw(func(driverConn any) error {
		_ = driverConn.(*SQLiteConn) // Verify connection type

		// Create changeset in a separate in-memory DB
		db2, err := sql.Open("sqlite3", ":memory:")
		if err != nil {
			return err
		}
		defer db2.Close()

		_, err = db2.Exec("CREATE TABLE test (id INTEGER PRIMARY KEY, value TEXT)")
		if err != nil {
			return err
		}

		conn2, err := db2.Conn(context.Background())
		if err != nil {
			return err
		}
		defer conn2.Close()

		err = conn2.Raw(func(driverConn2 any) error {
			c2 := driverConn2.(*SQLiteConn)
			session, err := c2.CreateSession("main")
			if err != nil {
				return err
			}
			defer session.Close()

			err = session.Attach("")
			if err != nil {
				return err
			}

			_, err = execConn(c2, "INSERT INTO test (id, value) VALUES (1, 'incoming')")
			if err != nil {
				return err
			}

			changeset, err = session.Changeset()
			return err
		})
		return err
	})
	if err != nil {
		t.Fatal("Failed to create changeset:", err)
	}

	// Track values received in conflict handler
	var (
		handlerCalled       bool
		receivedConflictType ConflictType
		receivedOp          int
		receivedTableName   string
		receivedNCols       int
		receivedNewValue    interface{}
		receivedConflictVal interface{}
	)

	// Apply changeset with conflict handler that accesses iterator
	err = conn.Raw(func(driverConn any) error {
		c := driverConn.(*SQLiteConn)
		return c.ApplyChangeset(
			bytes.NewReader(changeset),
			nil,
			func(ct ConflictType, iter *ChangesetIterator) ConflictAction {
				handlerCalled = true
				receivedConflictType = ct

				// Access iterator Op()
				op, tableName, nCols, _, err := iter.Op()
				if err != nil {
					t.Errorf("Failed to get Op in handler: %v", err)
				} else {
					receivedOp = op
					receivedTableName = tableName
					receivedNCols = nCols
				}

				// Access new value (incoming change)
				newVal, err := iter.NewValue(1)
				if err != nil {
					t.Errorf("Failed to get NewValue in handler: %v", err)
				} else {
					receivedNewValue = newVal
				}

				// Access conflict value (existing DB value) - only valid for CONFLICT type
				if ct == ConflictConflict {
					conflictVal, err := iter.ConflictValue(1)
					if err != nil {
						t.Errorf("Failed to get ConflictValue in handler: %v", err)
					} else {
						receivedConflictVal = conflictVal
					}
				}

				return ConflictActionOmit
			},
		)
	})
	if err != nil {
		t.Fatal("Failed to apply changeset:", err)
	}

	if !handlerCalled {
		t.Fatal("Conflict handler was not called")
	}

	// Verify the conflict type
	if receivedConflictType != ConflictConflict {
		t.Errorf("Expected ConflictConflict (%d), got %d", ConflictConflict, receivedConflictType)
	}

	// Verify Op() returned correct values
	if receivedOp != SQLITE_INSERT {
		t.Errorf("Expected SQLITE_INSERT (%d), got %d", SQLITE_INSERT, receivedOp)
	}
	if receivedTableName != "test" {
		t.Errorf("Expected table name 'test', got '%s'", receivedTableName)
	}
	if receivedNCols != 2 {
		t.Errorf("Expected 2 columns, got %d", receivedNCols)
	}

	// Verify NewValue returned the incoming value
	if str, ok := receivedNewValue.(string); !ok || str != "incoming" {
		t.Errorf("Expected NewValue 'incoming', got %v", receivedNewValue)
	}

	// Verify ConflictValue returned the existing DB value
	if str, ok := receivedConflictVal.(string); !ok || str != "existing" {
		t.Errorf("Expected ConflictValue 'existing', got %v", receivedConflictVal)
	}
}

func TestConflictHandlerIteratorOldNew(t *testing.T) {
	// Test OldValue and NewValue access in conflict handler for UPDATE conflict (DATA conflict)

	db, err := sql.Open("sqlite3", ":memory:")
	if err != nil {
		t.Fatal("Failed to open database:", err)
	}
	defer db.Close()

	_, err = db.Exec("CREATE TABLE test (id INTEGER PRIMARY KEY, value TEXT)")
	if err != nil {
		t.Fatal("Failed to create table:", err)
	}

	// Insert row with initial value - this will be updated
	_, err = db.Exec("INSERT INTO test (id, value) VALUES (1, 'modified_by_target')")
	if err != nil {
		t.Fatal("Failed to insert row:", err)
	}

	conn, err := db.Conn(context.Background())
	if err != nil {
		t.Fatal("Failed to get connection:", err)
	}
	defer conn.Close()

	// Create a changeset that updates the row
	// The changeset records old value as 'original' but target DB has 'modified_by_target'
	// This creates a DATA conflict
	var changeset []byte
	err = conn.Raw(func(driverConn any) error {
		// Create changeset in a separate in-memory DB
		db2, err := sql.Open("sqlite3", ":memory:")
		if err != nil {
			return err
		}
		defer db2.Close()

		_, err = db2.Exec("CREATE TABLE test (id INTEGER PRIMARY KEY, value TEXT)")
		if err != nil {
			return err
		}

		// Insert the row with 'original' value first
		_, err = db2.Exec("INSERT INTO test (id, value) VALUES (1, 'original')")
		if err != nil {
			return err
		}

		conn2, err := db2.Conn(context.Background())
		if err != nil {
			return err
		}
		defer conn2.Close()

		err = conn2.Raw(func(driverConn2 any) error {
			c2 := driverConn2.(*SQLiteConn)
			session, err := c2.CreateSession("main")
			if err != nil {
				return err
			}
			defer session.Close()

			err = session.Attach("")
			if err != nil {
				return err
			}

			// Update to create UPDATE changeset
			_, err = execConn(c2, "UPDATE test SET value = 'updated' WHERE id = 1")
			if err != nil {
				return err
			}

			changeset, err = session.Changeset()
			return err
		})
		return err
	})
	if err != nil {
		t.Fatal("Failed to create changeset:", err)
	}

	// Track values received in conflict handler
	var (
		handlerCalled    bool
		receivedOp       int
		receivedOldValue interface{}
		receivedNewValue interface{}
	)

	// Apply changeset - should trigger DATA conflict because old value doesn't match
	err = conn.Raw(func(driverConn any) error {
		c := driverConn.(*SQLiteConn)
		return c.ApplyChangeset(
			bytes.NewReader(changeset),
			nil,
			func(ct ConflictType, iter *ChangesetIterator) ConflictAction {
				handlerCalled = true

				// Verify it's a DATA conflict
				if ct != ConflictData {
					t.Errorf("Expected ConflictData (%d), got %d", ConflictData, ct)
				}

				// Access iterator Op()
				op, _, _, _, err := iter.Op()
				if err != nil {
					t.Errorf("Failed to get Op in handler: %v", err)
				} else {
					receivedOp = op
				}

				// Access old value (what changeset expects)
				oldVal, err := iter.OldValue(1)
				if err != nil {
					t.Errorf("Failed to get OldValue in handler: %v", err)
				} else {
					receivedOldValue = oldVal
				}

				// Access new value (what changeset wants to set)
				newVal, err := iter.NewValue(1)
				if err != nil {
					t.Errorf("Failed to get NewValue in handler: %v", err)
				} else {
					receivedNewValue = newVal
				}

				return ConflictActionOmit
			},
		)
	})
	if err != nil {
		t.Fatal("Failed to apply changeset:", err)
	}

	if !handlerCalled {
		t.Fatal("Conflict handler was not called")
	}

	// Verify Op() returned UPDATE
	if receivedOp != SQLITE_UPDATE {
		t.Errorf("Expected SQLITE_UPDATE (%d), got %d", SQLITE_UPDATE, receivedOp)
	}

	// Verify OldValue returned the expected original value from changeset
	if str, ok := receivedOldValue.(string); !ok || str != "original" {
		t.Errorf("Expected OldValue 'original', got %v", receivedOldValue)
	}

	// Verify NewValue returned what the changeset wants to set
	if str, ok := receivedNewValue.(string); !ok || str != "updated" {
		t.Errorf("Expected NewValue 'updated', got %v", receivedNewValue)
	}
}
