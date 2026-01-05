/*
Package sqlite3 provides interface to SQLite3 databases.

This works as a driver for database/sql.

Installation

	go get github.com/mattn/go-sqlite3

# Supported Types

Currently, go-sqlite3 supports the following data types.

	+------------------------------+
	|go        | sqlite3           |
	|----------|-------------------|
	|nil       | null              |
	|int       | integer           |
	|int64     | integer           |
	|float64   | float             |
	|bool      | integer           |
	|[]byte    | blob              |
	|string    | text              |
	|time.Time | timestamp/datetime|
	+------------------------------+

# SQLite3 Extension

You can write your own extension module for sqlite3. For example, below is an
extension for a Regexp matcher operation.

	#include <pcre.h>
	#include <string.h>
	#include <stdio.h>
	#include <sqlite3ext.h>

	SQLITE_EXTENSION_INIT1
	static void regexp_func(sqlite3_context *context, int argc, sqlite3_value **argv) {
	  if (argc >= 2) {
	    const char *target  = (const char *)sqlite3_value_text(argv[1]);
	    const char *pattern = (const char *)sqlite3_value_text(argv[0]);
	    const char* errstr = NULL;
	    int erroff = 0;
	    int vec[500];
	    int n, rc;
	    pcre* re = pcre_compile(pattern, 0, &errstr, &erroff, NULL);
	    rc = pcre_exec(re, NULL, target, strlen(target), 0, 0, vec, 500);
	    if (rc <= 0) {
	      sqlite3_result_error(context, errstr, 0);
	      return;
	    }
	    sqlite3_result_int(context, 1);
	  }
	}

	#ifdef _WIN32
	__declspec(dllexport)
	#endif
	int sqlite3_extension_init(sqlite3 *db, char **errmsg,
	      const sqlite3_api_routines *api) {
	  SQLITE_EXTENSION_INIT2(api);
	  return sqlite3_create_function(db, "regexp", 2, SQLITE_UTF8,
	      (void*)db, regexp_func, NULL, NULL);
	}

It needs to be built as a so/dll shared library. And you need to register
the extension module like below.

	sql.Register("sqlite3_with_extensions",
		&sqlite3.SQLiteDriver{
			Extensions: []string{
				"sqlite3_mod_regexp",
			},
		})

Then, you can use this extension.

	rows, err := db.Query("select text from mytable where name regexp '^golang'")

# Connection Hook

You can hook and inject your code when the connection is established by setting
ConnectHook to get the SQLiteConn.

	sql.Register("sqlite3_with_hook_example",
			&sqlite3.SQLiteDriver{
					ConnectHook: func(conn *sqlite3.SQLiteConn) error {
						sqlite3conn = append(sqlite3conn, conn)
						return nil
					},
			})

You can also use database/sql.Conn.Raw (Go >= 1.13):

	conn, err := db.Conn(context.Background())
	// if err != nil { ... }
	defer conn.Close()
	err = conn.Raw(func (driverConn any) error {
		sqliteConn := driverConn.(*sqlite3.SQLiteConn)
		// ... use sqliteConn
	})
	// if err != nil { ... }

# Go SQlite3 Extensions

If you want to register Go functions as SQLite extension functions
you can make a custom driver by calling RegisterFunction from
ConnectHook.

	regex = func(re, s string) (bool, error) {
		return regexp.MatchString(re, s)
	}
	sql.Register("sqlite3_extended",
			&sqlite3.SQLiteDriver{
					ConnectHook: func(conn *sqlite3.SQLiteConn) error {
						return conn.RegisterFunc("regexp", regex, true)
					},
			})

You can then use the custom driver by passing its name to sql.Open.

	var i int
	conn, err := sql.Open("sqlite3_extended", "./foo.db")
	if err != nil {
		panic(err)
	}
	err = db.QueryRow(`SELECT regexp("foo.*", "seafood")`).Scan(&i)
	if err != nil {
		panic(err)
	}

See the documentation of RegisterFunc for more details.

# Session Extension

The Session Extension provides change tracking and synchronization capabilities
for SQLite databases. It allows you to record changes made to database tables,
generate changesets, and apply those changesets to other databases.

To enable the Session Extension, build with the sqlite_session tag:

	go build -tags "sqlite_session"

# Creating a Session and Capturing Changes

A session tracks changes made to specified tables. Here's how to create a session
and capture changes:

	import (
		"database/sql"
		"github.com/mattn/go-sqlite3"
	)

	// Get access to the SQLiteConn through the ConnectHook
	var sqliteConn *sqlite3.SQLiteConn
	sql.Register("sqlite3_with_session",
		&sqlite3.SQLiteDriver{
			ConnectHook: func(conn *sqlite3.SQLiteConn) error {
				sqliteConn = conn
				return nil
			},
		})

	db, err := sql.Open("sqlite3_with_session", "./mydb.db")
	if err != nil {
		panic(err)
	}
	defer db.Close()

	// Ensure connection is established
	db.Ping()

	// Create a session on the "main" database
	session, err := sqliteConn.CreateSession("main")
	if err != nil {
		panic(err)
	}
	defer session.Close()

	// Attach tables to track (empty string attaches all tables)
	err = session.Attach("users")
	if err != nil {
		panic(err)
	}

	// Make changes to the database
	db.Exec("INSERT INTO users (name) VALUES ('Alice')")
	db.Exec("UPDATE users SET name = 'Bob' WHERE id = 1")

	// Generate the changeset
	changeset, err := session.Changeset()
	if err != nil {
		panic(err)
	}
	// changeset contains the binary representation of all recorded changes

# Streaming Changeset to an io.Writer

For large changesets, streaming to an io.Writer is more memory efficient:

	import "os"

	// Create output file
	file, err := os.Create("changes.bin")
	if err != nil {
		panic(err)
	}
	defer file.Close()

	// Stream changeset directly to file
	err = session.WriteChangeset(file)
	if err != nil {
		panic(err)
	}

# Applying Changeset from an io.Reader

Apply a changeset to synchronize another database:

	import (
		"bytes"
		"os"
	)

	// Open the changeset file
	file, err := os.Open("changes.bin")
	if err != nil {
		panic(err)
	}
	defer file.Close()

	// Apply changeset with optional filter and conflict handler
	err = sqliteConn.ApplyChangeset(file, nil, nil)
	if err != nil {
		panic(err)
	}

	// Or apply from a byte slice
	err = sqliteConn.ApplyChangesetBytes(changeset, nil, nil)
	if err != nil {
		panic(err)
	}

# Conflict Handling

When applying changesets, conflicts may occur. You can provide a ConflictHandler
callback to decide how to handle each conflict. The handler receives the conflict
type and a ChangesetIterator that provides access to all details about the
conflicting change:

	// ConflictType values:
	// - ConflictData: Row exists but with different values
	// - ConflictNotFound: Row to UPDATE/DELETE was not found
	// - ConflictConflict: INSERT would cause duplicate primary key
	// - ConflictConstraint: Change violates a constraint
	// - ConflictForeignKey: Foreign key violation

	// ConflictAction values:
	// - ConflictActionOmit: Skip this change
	// - ConflictActionAbort: Abort the entire operation
	// - ConflictActionReplace: Replace conflicting row (for Data/Conflict only)

	// ConflictHandler signature:
	// type ConflictHandler func(conflictType ConflictType, iter *ChangesetIterator) ConflictAction

	// Iterator methods available in conflict handler:
	// - iter.Op()            - returns (op, tableName, nCols, indirect, err)
	//                          indirect is true if change came from trigger/FK action
	// - iter.PK()            - returns ([]bool, err) where true means column is primary key
	// - iter.OldValue(i)     - old value for DELETE/UPDATE operations
	// - iter.NewValue(i)     - new value for INSERT/UPDATE operations
	// - iter.ConflictValue(i)- conflicting DB row value (DATA/CONFLICT types only)
	// - iter.FKConflicts()   - FK violation count (FOREIGN_KEY type only)

	conflictHandler := func(ct sqlite3.ConflictType, iter *sqlite3.ChangesetIterator) sqlite3.ConflictAction {
		// Get operation details including the indirect flag
		op, tableName, nCols, indirect, _ := iter.Op()

		// Get primary key information
		pk, _ := iter.PK()

		// Log conflict details
		fmt.Printf("Conflict on table %s, op=%d, indirect=%v\n", tableName, op, indirect)

		switch ct {
		case sqlite3.ConflictData:
			// Existing row has different values - examine the differences
			for i := 0; i < nCols; i++ {
				existing, _ := iter.ConflictValue(i)
				incoming, _ := iter.NewValue(i)
				if pk[i] {
					fmt.Printf("  PK column %d: %v\n", i, existing)
				} else {
					fmt.Printf("  Column %d: existing=%v, incoming=%v\n", i, existing, incoming)
				}
			}
			return sqlite3.ConflictActionReplace
		case sqlite3.ConflictNotFound:
			// Row to update/delete not found - skip it
			return sqlite3.ConflictActionOmit
		case sqlite3.ConflictConflict:
			// Duplicate primary key on insert - examine and replace
			for i := 0; i < nCols; i++ {
				existing, _ := iter.ConflictValue(i)
				incoming, _ := iter.NewValue(i)
				fmt.Printf("  Column %d: existing=%v, incoming=%v\n", i, existing, incoming)
			}
			return sqlite3.ConflictActionReplace
		case sqlite3.ConflictForeignKey:
			// Foreign key violation - check FK conflict count
			fkCount, _ := iter.FKConflicts()
			fmt.Printf("  FK violations: %d\n", fkCount)
			return sqlite3.ConflictActionAbort
		default:
			// For other constraints, abort
			return sqlite3.ConflictActionAbort
		}
	}

	// Optional table filter to apply only to specific tables
	tableFilter := func(tableName string) bool {
		return tableName == "users" || tableName == "orders"
	}

	err = sqliteConn.ApplyChangeset(file, tableFilter, conflictHandler)

# Iterating Over Changeset Contents

You can inspect individual changes in a changeset using ChangesetIterator:

	iter, err := sqlite3.NewChangesetIterator(bytes.NewReader(changeset))
	if err != nil {
		panic(err)
	}
	defer iter.Close()

	for iter.Next() {
		// Op() returns 5 values: operation type, table name, column count,
		// indirect flag, and error. The indirect flag is true if the change
		// came from a trigger or foreign key action rather than direct SQL.
		op, tableName, nCols, indirect, err := iter.Op()
		if err != nil {
			panic(err)
		}

		// PK() returns a boolean slice indicating which columns are part
		// of the primary key. Each element corresponds to a column index.
		pk, err := iter.PK()
		if err != nil {
			panic(err)
		}

		// Build a string of primary key column indices
		var pkCols []int
		for i, isPK := range pk {
			if isPK {
				pkCols = append(pkCols, i)
			}
		}

		switch op {
		case sqlite3.SQLITE_INSERT:
			fmt.Printf("INSERT into %s (indirect=%v, pk_cols=%v)\n", tableName, indirect, pkCols)
			for i := 0; i < nCols; i++ {
				newVal, _ := iter.NewValue(i)
				if pk[i] {
					fmt.Printf("  Column %d (PK): %v\n", i, newVal)
				} else {
					fmt.Printf("  Column %d: %v\n", i, newVal)
				}
			}
		case sqlite3.SQLITE_DELETE:
			fmt.Printf("DELETE from %s (indirect=%v, pk_cols=%v)\n", tableName, indirect, pkCols)
			for i := 0; i < nCols; i++ {
				oldVal, _ := iter.OldValue(i)
				if pk[i] {
					fmt.Printf("  Column %d (PK) was: %v\n", i, oldVal)
				} else {
					fmt.Printf("  Column %d was: %v\n", i, oldVal)
				}
			}
		case sqlite3.SQLITE_UPDATE:
			fmt.Printf("UPDATE %s (indirect=%v, pk_cols=%v)\n", tableName, indirect, pkCols)
			for i := 0; i < nCols; i++ {
				oldVal, _ := iter.OldValue(i)
				newVal, _ := iter.NewValue(i)
				if pk[i] {
					fmt.Printf("  Column %d (PK): %v -> %v\n", i, oldVal, newVal)
				} else {
					fmt.Printf("  Column %d: %v -> %v\n", i, oldVal, newVal)
				}
			}
		}
	}

	if err := iter.Err(); err != nil {
		panic(err)
	}

# Coalescing Changesets with Changegroup

A Changegroup combines multiple changesets into one, coalescing changes where possible.
For example, if one changeset inserts a row and another deletes it, the combined
changeset will contain neither operation:

	group, err := sqlite3.NewChangegroup()
	if err != nil {
		panic(err)
	}
	defer group.Close()

	// Add multiple changesets
	err = group.Add(bytes.NewReader(changeset1))
	if err != nil {
		panic(err)
	}
	err = group.Add(bytes.NewReader(changeset2))
	if err != nil {
		panic(err)
	}

	// Output the combined changeset
	var combined bytes.Buffer
	err = group.Output(&combined)
	if err != nil {
		panic(err)
	}
	// combined.Bytes() contains the coalesced changeset

# Concatenating and Inverting Changesets

You can concatenate changesets without coalescing, or invert a changeset:

	// Concatenate two changesets (preserves all operations)
	var concatenated bytes.Buffer
	err = sqlite3.ConcatChangesets(
		bytes.NewReader(changeset1),
		bytes.NewReader(changeset2),
		&concatenated,
	)

	// Invert a changeset (INSERT becomes DELETE, etc.)
	var inverted bytes.Buffer
	err = sqlite3.InvertChangeset(
		bytes.NewReader(changeset),
		&inverted,
	)

# Rebaser for Synchronization

The Rebaser is used in collaborative scenarios where multiple users make concurrent
changes. After conflicts are resolved, the rebaser transforms changesets:

	// Apply changeset and capture rebase data
	var rebaseData bytes.Buffer
	err = sqliteConn.ApplyChangesetV2(
		bytes.NewReader(changeset),
		&rebaseData, // receives rebase information
		nil,         // no filter
		conflictHandler,
		0,           // flags
	)

	// Create rebaser and configure with rebase data
	rebaser, err := sqlite3.NewRebaser()
	if err != nil {
		panic(err)
	}
	defer rebaser.Close()

	err = rebaser.Configure(rebaseData.Bytes())
	if err != nil {
		panic(err)
	}

	// Rebase another changeset
	var rebased bytes.Buffer
	err = rebaser.Rebase(
		bytes.NewReader(otherChangeset),
		&rebased,
	)

# Session Control Methods

Additional session control methods are available:

	// Enable/disable change tracking (1=enable, 0=disable, -1=query)
	currentState := session.Enable(1)

	// Check if session has recorded any changes
	if session.IsEmpty() {
		fmt.Println("No changes recorded")
	}

	// Set indirect mode to capture trigger/FK changes (1=enable, 0=disable)
	session.Indirect(1)

	// Generate a patchset (more compact than changeset for UPDATEs)
	patchset, err := session.Patchset()

	// Stream patchset to writer
	err = session.WritePatchset(file)

	// Attach tables using a filter function
	err = session.AttachTable(func(tableName string) bool {
		return tableName != "system_tables"
	})
*/
package sqlite3
