// Copyright (C) 2019 G.J.R. Timmer <gjr.timmer@gmail.com>.
//
// Use of this source code is governed by an MIT-style
// license that can be found in the LICENSE file.

//go:build sqlite_session
// +build sqlite_session

package sqlite3

/*
#cgo CFLAGS: -DSQLITE_ENABLE_SESSION
#cgo CFLAGS: -DSQLITE_ENABLE_PREUPDATE_HOOK
#cgo LDFLAGS: -lm

#ifndef USE_LIBSQLITE3
#include "sqlite3-binding.h"
#else
#include <sqlite3.h>
#endif
#include <stdlib.h>
#include <string.h>

extern int sessionOutputTrampoline(void *pOut, const void *pData, int nData);
extern int sessionFilterTrampoline(void *pCtx, const char *zTab);
*/
import "C"
import (
	"errors"
	"io"
	"runtime"
	"unsafe"
)

// ConflictType represents the type of conflict detected during changeset application.
type ConflictType int

const (
	// ConflictData is returned when the conflict handler is invoked with CHANGESET_DATA
	// as the second argument when processing a DELETE or UPDATE change if a row with
	// the required PRIMARY KEY fields is present in the database, but one or more other
	// (non primary-key) fields modified by the update do not contain the expected
	// "before" values.
	ConflictData ConflictType = C.SQLITE_CHANGESET_DATA

	// ConflictNotFound is returned when the conflict handler is invoked with
	// CHANGESET_NOTFOUND as the second argument when processing a DELETE or UPDATE
	// change if a row with the required PRIMARY KEY fields is not present in the database.
	ConflictNotFound ConflictType = C.SQLITE_CHANGESET_NOTFOUND

	// ConflictConflict is returned when the conflict handler is invoked with
	// CHANGESET_CONFLICT as the second argument when processing an INSERT change
	// if the operation would result in duplicate primary key values.
	ConflictConflict ConflictType = C.SQLITE_CHANGESET_CONFLICT

	// ConflictConstraint is returned when any change is made as part of a changeset
	// that cannot be applied due to a constraint violation.
	ConflictConstraint ConflictType = C.SQLITE_CHANGESET_CONSTRAINT

	// ConflictForeignKey is returned when applying a changeset leaves the database
	// in a state containing foreign key violations.
	ConflictForeignKey ConflictType = C.SQLITE_CHANGESET_FOREIGN_KEY
)

// ConflictAction represents the action to take when a conflict occurs.
type ConflictAction int

const (
	// ConflictActionOmit causes the conflicting change to be omitted.
	ConflictActionOmit ConflictAction = C.SQLITE_CHANGESET_OMIT

	// ConflictActionAbort causes the application to abort and return SQLITE_ABORT.
	ConflictActionAbort ConflictAction = C.SQLITE_CHANGESET_ABORT

	// ConflictActionReplace causes the conflicting row to be removed and the
	// change re-applied. Only valid for SQLITE_CHANGESET_DATA and
	// SQLITE_CHANGESET_CONFLICT conflicts.
	ConflictActionReplace ConflictAction = C.SQLITE_CHANGESET_REPLACE
)

// ApplyFlags represents flags that can be passed to changeset apply functions.
type ApplyFlags int

const (
	// ApplyNoSavepoint disables the implicit savepoint around changeset application.
	ApplyNoSavepoint ApplyFlags = C.SQLITE_CHANGESETAPPLY_NOSAVEPOINT

	// ApplyInvert causes the changeset to be applied in reverse.
	ApplyInvert ApplyFlags = C.SQLITE_CHANGESETAPPLY_INVERT

	// ApplyIgnoreNoop ignores no-op UPDATE changes when applying a changeset.
	ApplyIgnoreNoop ApplyFlags = C.SQLITE_CHANGESETAPPLY_IGNORENOOP
)

// IteratorFlags represents flags that can be passed to changeset iterator functions.
type IteratorFlags int

const (
	// IteratorInvert causes the iterator to report changes as if they were inverted.
	IteratorInvert IteratorFlags = C.SQLITE_CHANGESETSTART_INVERT
)

// TableFilter is a callback function that determines whether changes to a
// specific table should be included in the session or applied from a changeset.
// Return true to include changes for the table, false to exclude them.
type TableFilter func(tableName string) bool

// ConflictHandler is a callback function invoked when a conflict is detected
// during changeset application. It receives the conflict type and an iterator
// providing access to the change data and conflict values.
// The iterator is only valid within the callback - do not store or use it outside.
type ConflictHandler func(conflictType ConflictType, iter *ChangesetIterator) ConflictAction

// Session represents an SQLite session object used for tracking changes
// to database tables.
type Session struct {
	s    *C.sqlite3_session
	conn *SQLiteConn
}

// CreateSession creates a new session object for tracking database changes.
// The dbName parameter specifies the database to track (typically "main").
func (c *SQLiteConn) CreateSession(dbName string) (*Session, error) {
	if dbName == "" {
		dbName = "main"
	}

	cDbName := C.CString(dbName)
	defer C.free(unsafe.Pointer(cDbName))

	var session *C.sqlite3_session
	rv := C.sqlite3session_create(c.db, cDbName, &session)
	if rv != C.SQLITE_OK {
		return nil, Error{Code: ErrNo(rv)}
	}

	s := &Session{s: session, conn: c}
	runtime.SetFinalizer(s, (*Session).Close)
	return s, nil
}

// Close closes the session and releases all resources.
func (s *Session) Close() error {
	if s.s == nil {
		return nil
	}

	C.sqlite3session_delete(s.s)
	s.s = nil
	runtime.SetFinalizer(s, nil)
	return nil
}

// Attach attaches a table to the session for tracking changes.
// If tableName is empty, all tables in the database are attached.
func (s *Session) Attach(tableName string) error {
	if s.s == nil {
		return errors.New("session is closed")
	}

	var rv C.int
	if tableName == "" {
		// Attach all tables
		rv = C.sqlite3session_attach(s.s, nil)
	} else {
		cTableName := C.CString(tableName)
		defer C.free(unsafe.Pointer(cTableName))
		rv = C.sqlite3session_attach(s.s, cTableName)
	}

	if rv != C.SQLITE_OK {
		return Error{Code: ErrNo(rv)}
	}
	return nil
}

// Enable enables or disables change tracking for the session.
// Pass 1 to enable, 0 to disable, or -1 to query without changing.
// Returns the current enabled state (1 for enabled, 0 for disabled).
func (s *Session) Enable(enable int) int {
	if s.s == nil {
		return 0
	}
	return int(C.sqlite3session_enable(s.s, C.int(enable)))
}

// IsEmpty returns true if the session has recorded no changes.
func (s *Session) IsEmpty() bool {
	if s.s == nil {
		return true
	}
	return C.sqlite3session_isempty(s.s) != 0
}

// Indirect sets or queries the indirect flag for the session.
// When set to 1, changes made by triggers and foreign key actions are also captured.
// Pass 1 to enable indirect mode, 0 to disable, or -1 to query without changing.
// Returns the current indirect state (1 for indirect, 0 for direct).
func (s *Session) Indirect(indirect int) int {
	if s.s == nil {
		return 0
	}
	return int(C.sqlite3session_indirect(s.s, C.int(indirect)))
}

// Changeset generates a changeset containing all changes recorded by the session.
// The returned byte slice contains the binary changeset data.
func (s *Session) Changeset() ([]byte, error) {
	if s.s == nil {
		return nil, errors.New("session is closed")
	}

	var pnChangeset C.int
	var ppChangeset unsafe.Pointer
	rv := C.sqlite3session_changeset(s.s, &pnChangeset, &ppChangeset)
	if rv != C.SQLITE_OK {
		return nil, Error{Code: ErrNo(rv)}
	}

	// Copy the changeset data to Go memory
	changeset := C.GoBytes(ppChangeset, pnChangeset)
	C.sqlite3_free(ppChangeset)

	return changeset, nil
}

// Patchset generates a patchset containing all changes recorded by the session.
// Patchsets are more compact than changesets for UPDATE operations as they only
// store the changed columns rather than the full before and after values.
func (s *Session) Patchset() ([]byte, error) {
	if s.s == nil {
		return nil, errors.New("session is closed")
	}

	var pnPatchset C.int
	var ppPatchset unsafe.Pointer
	rv := C.sqlite3session_patchset(s.s, &pnPatchset, (*unsafe.Pointer)(unsafe.Pointer(&ppPatchset)))
	if rv != C.SQLITE_OK {
		return nil, Error{Code: ErrNo(rv)}
	}

	// Copy the patchset data to Go memory
	patchset := C.GoBytes(ppPatchset, pnPatchset)
	C.sqlite3_free(ppPatchset)

	return patchset, nil
}

// WriteChangeset streams the changeset to an io.Writer.
// This is more memory efficient than Changeset() for large changesets.
func (s *Session) WriteChangeset(w io.Writer) error {
	if s.s == nil {
		return errors.New("session is closed")
	}

	ctx := &sessionWriteContext{w: w}
	handle := newHandle(s.conn, ctx)
	defer func() {
		handleLock.Lock()
		delete(handleVals, handle)
		handleLock.Unlock()
		C.free(handle)
	}()

	rv := C.sqlite3session_changeset_strm(
		s.s,
		(*[0]byte)(C.sessionOutputTrampoline),
		handle,
	)
	if rv != C.SQLITE_OK {
		if ctx.err != nil {
			return ctx.err
		}
		return Error{Code: ErrNo(rv)}
	}
	return ctx.err
}

// WritePatchset streams the patchset to an io.Writer.
// This is more memory efficient than Patchset() for large patchsets.
func (s *Session) WritePatchset(w io.Writer) error {
	if s.s == nil {
		return errors.New("session is closed")
	}

	ctx := &sessionWriteContext{w: w}
	handle := newHandle(s.conn, ctx)
	defer func() {
		handleLock.Lock()
		delete(handleVals, handle)
		handleLock.Unlock()
		C.free(handle)
	}()

	rv := C.sqlite3session_patchset_strm(
		s.s,
		(*[0]byte)(C.sessionOutputTrampoline),
		handle,
	)
	if rv != C.SQLITE_OK {
		if ctx.err != nil {
			return ctx.err
		}
		return Error{Code: ErrNo(rv)}
	}
	return ctx.err
}

// AttachTable attaches tables to the session using a filter callback.
// The filter function is called for each table and should return true to track
// changes to that table, or false to ignore it.
func (s *Session) AttachTable(filter TableFilter) error {
	if s.s == nil {
		return errors.New("session is closed")
	}

	handle := newHandle(s.conn, filter)
	// Note: We don't delete the handle here because it needs to persist
	// for the lifetime of the session. It will be cleaned up when the
	// connection is closed.

	C.sqlite3session_table_filter(
		s.s,
		(*[0]byte)(C.sessionFilterTrampoline),
		handle,
	)
	return nil
}
