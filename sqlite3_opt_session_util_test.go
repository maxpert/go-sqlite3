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

// execConnUtil is a helper to execute SQL on a SQLiteConn directly
func execConnUtil(c *SQLiteConn, query string, args ...any) (driver.Result, error) {
	vals := make([]driver.Value, len(args))
	for i, a := range args {
		vals[i] = a
	}
	return c.Exec(query, vals)
}

func TestUtilInvertSimple(t *testing.T) {
	// Just test InvertChangeset without applying
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

		_, err = execConnUtil(c, "INSERT INTO test (id, value) VALUES (1, 'original')")
		if err != nil {
			return err
		}

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

	t.Log("InvertChangeset succeeded!")
	t.Logf("Original changeset size: %d", changesetBuf.Len())
	t.Logf("Inverted changeset size: %d", invertedBuf.Len())
}
