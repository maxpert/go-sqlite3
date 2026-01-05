// Copyright (C) 2019 G.J.R. Timmer <gjr.timmer@gmail.com>.
//
// Use of this source code is governed by an MIT-style
// license that can be found in the LICENSE file.

//go:build !sqlite_session && cgo
// +build !sqlite_session,cgo

package sqlite3

import (
	"errors"
	"io"
)

var ErrSessionNotEnabled = errors.New("SQLite session extension not enabled")

// ConflictType represents the type of conflict detected during changeset application.
type ConflictType int

const (
	ConflictData       ConflictType = 1
	ConflictNotFound   ConflictType = 2
	ConflictConflict   ConflictType = 3
	ConflictConstraint ConflictType = 4
	ConflictForeignKey ConflictType = 5
)

// ConflictAction represents the action to take when a conflict occurs.
type ConflictAction int

const (
	ConflictActionOmit    ConflictAction = 0
	ConflictActionAbort   ConflictAction = 1
	ConflictActionReplace ConflictAction = 2
)

// ApplyFlags represents flags that can be passed to changeset apply functions.
type ApplyFlags int

const (
	ApplyNoSavepoint ApplyFlags = 0x0001
	ApplyInvert      ApplyFlags = 0x0002
	ApplyIgnoreNoop  ApplyFlags = 0x0004
)

// IteratorFlags represents flags that can be passed to changeset iterator functions.
type IteratorFlags int

const (
	IteratorInvert IteratorFlags = 0x0002
)

// TableFilter is a callback function that determines whether changes to a
// specific table should be included in the session or applied from a changeset.
type TableFilter func(tableName string) bool

// ConflictHandler is a callback function invoked when a conflict is detected
// during changeset application. It receives the conflict type and an iterator
// providing access to the change data and conflict values.
// The iterator is only valid within the callback - do not store or use it outside.
type ConflictHandler func(conflictType ConflictType, iter *ChangesetIterator) ConflictAction

// Session represents an SQLite session object used for tracking changes
// to database tables.
type Session struct{}

// ChangesetIterator represents an iterator over a changeset or patchset.
type ChangesetIterator struct{}

// CreateSession returns an error indicating that the session extension is not enabled.
func (c *SQLiteConn) CreateSession(dbName string) (*Session, error) {
	return nil, ErrSessionNotEnabled
}

// Close is a no-op when the session extension is not enabled.
func (s *Session) Close() error {
	return nil
}

// Attach returns an error indicating that the session extension is not enabled.
func (s *Session) Attach(tableName string) error {
	return ErrSessionNotEnabled
}

// Enable returns 0 when the session extension is not enabled.
func (s *Session) Enable(enable int) int {
	return 0
}

// IsEmpty returns true when the session extension is not enabled.
func (s *Session) IsEmpty() bool {
	return true
}

// Indirect returns 0 when the session extension is not enabled.
func (s *Session) Indirect(indirect int) int {
	return 0
}

// Changeset returns an error indicating that the session extension is not enabled.
func (s *Session) Changeset() ([]byte, error) {
	return nil, ErrSessionNotEnabled
}

// Patchset returns an error indicating that the session extension is not enabled.
func (s *Session) Patchset() ([]byte, error) {
	return nil, ErrSessionNotEnabled
}

// WriteChangeset returns an error indicating that the session extension is not enabled.
func (s *Session) WriteChangeset(w io.Writer) error {
	return ErrSessionNotEnabled
}

// WritePatchset returns an error indicating that the session extension is not enabled.
func (s *Session) WritePatchset(w io.Writer) error {
	return ErrSessionNotEnabled
}

// AttachTable returns an error indicating that the session extension is not enabled.
func (s *Session) AttachTable(filter TableFilter) error {
	return ErrSessionNotEnabled
}

// ApplyChangeset returns an error indicating that the session extension is not enabled.
func (c *SQLiteConn) ApplyChangeset(changeset []byte, filter TableFilter, conflict ConflictHandler, flags ApplyFlags) error {
	return ErrSessionNotEnabled
}

// ApplyChangesetStream returns an error indicating that the session extension is not enabled.
func (c *SQLiteConn) ApplyChangesetStream(r io.Reader, filter TableFilter, conflict ConflictHandler, flags ApplyFlags) error {
	return ErrSessionNotEnabled
}

// ChangesetIteratorStart returns an error indicating that the session extension is not enabled.
func ChangesetIteratorStart(changeset []byte, flags IteratorFlags) (*ChangesetIterator, error) {
	return nil, ErrSessionNotEnabled
}

// Next returns an error indicating that the session extension is not enabled.
func (it *ChangesetIterator) Next() (bool, error) {
	return false, ErrSessionNotEnabled
}

// Close is a no-op when the session extension is not enabled.
func (it *ChangesetIterator) Close() error {
	return nil
}

// Op returns an error indicating that the session extension is not enabled.
func (it *ChangesetIterator) Op() (tableName string, numCols int, opType int, indirect bool, err error) {
	return "", 0, 0, false, ErrSessionNotEnabled
}

// PK returns an error indicating that the session extension is not enabled.
func (it *ChangesetIterator) PK() ([]bool, error) {
	return nil, ErrSessionNotEnabled
}
