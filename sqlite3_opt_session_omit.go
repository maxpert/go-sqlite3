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
// during changeset application.
type ConflictHandler func(conflictType ConflictType, iter *ChangesetIterator) ConflictAction

// Session represents an SQLite session object.
type Session struct{}

// ChangesetIterator represents an iterator over a changeset or patchset.
type ChangesetIterator struct{}

// Changegroup combines multiple changesets into one.
type Changegroup struct{}

// Rebaser rebases changesets after conflict resolution.
type Rebaser struct{}

// =============================================================================
// Session methods
// =============================================================================

func (c *SQLiteConn) CreateSession(dbName string) (*Session, error) {
	return nil, ErrSessionNotEnabled
}

func (s *Session) Close() error {
	return nil
}

func (s *Session) Attach(tableName string) error {
	return ErrSessionNotEnabled
}

func (s *Session) AttachTable(filter TableFilter) error {
	return ErrSessionNotEnabled
}

func (s *Session) Enable(enable int) int {
	return 0
}

func (s *Session) IsEmpty() bool {
	return true
}

func (s *Session) Indirect(indirect int) int {
	return 0
}

func (s *Session) Changeset() ([]byte, error) {
	return nil, ErrSessionNotEnabled
}

func (s *Session) Patchset() ([]byte, error) {
	return nil, ErrSessionNotEnabled
}

func (s *Session) WriteChangeset(w io.Writer) error {
	return ErrSessionNotEnabled
}

func (s *Session) WritePatchset(w io.Writer) error {
	return ErrSessionNotEnabled
}

// =============================================================================
// Apply methods
// =============================================================================

func (c *SQLiteConn) ApplyChangeset(r io.Reader, filter TableFilter, conflict ConflictHandler) error {
	return ErrSessionNotEnabled
}

func (c *SQLiteConn) ApplyChangesetV2(r io.Reader, rebaseOut io.Writer, filter TableFilter, conflict ConflictHandler, flags ApplyFlags) error {
	return ErrSessionNotEnabled
}

func (c *SQLiteConn) ApplyChangesetBytes(data []byte, filter TableFilter, conflict ConflictHandler) error {
	return ErrSessionNotEnabled
}

// =============================================================================
// Iterator constructors
// =============================================================================

func NewChangesetIterator(r io.Reader) (*ChangesetIterator, error) {
	return nil, ErrSessionNotEnabled
}

func NewChangesetIteratorWithFlags(r io.Reader, flags IteratorFlags) (*ChangesetIterator, error) {
	return nil, ErrSessionNotEnabled
}

func NewChangesetIteratorBytes(data []byte) (*ChangesetIterator, error) {
	return nil, ErrSessionNotEnabled
}

// =============================================================================
// Iterator methods
// =============================================================================

func (ci *ChangesetIterator) Next() bool {
	return false
}

func (ci *ChangesetIterator) Op() (op int, tableName string, nCols int, indirect bool, err error) {
	return 0, "", 0, false, ErrSessionNotEnabled
}

func (ci *ChangesetIterator) OldValue(i int) (interface{}, error) {
	return nil, ErrSessionNotEnabled
}

func (ci *ChangesetIterator) NewValue(i int) (interface{}, error) {
	return nil, ErrSessionNotEnabled
}

func (ci *ChangesetIterator) ConflictValue(i int) (interface{}, error) {
	return nil, ErrSessionNotEnabled
}

func (ci *ChangesetIterator) FKConflicts() (int, error) {
	return 0, ErrSessionNotEnabled
}

func (ci *ChangesetIterator) PK() ([]bool, error) {
	return nil, ErrSessionNotEnabled
}

func (ci *ChangesetIterator) Err() error {
	return nil
}

func (ci *ChangesetIterator) Close() error {
	return nil
}

// =============================================================================
// Utility functions
// =============================================================================

func InvertChangeset(r io.Reader, w io.Writer) error {
	return ErrSessionNotEnabled
}

func ConcatChangesets(r1, r2 io.Reader, w io.Writer) error {
	return ErrSessionNotEnabled
}

// =============================================================================
// Changegroup
// =============================================================================

func NewChangegroup() (*Changegroup, error) {
	return nil, ErrSessionNotEnabled
}

func (g *Changegroup) Add(r io.Reader) error {
	return ErrSessionNotEnabled
}

func (g *Changegroup) Output(w io.Writer) error {
	return ErrSessionNotEnabled
}

func (g *Changegroup) Close() error {
	return nil
}

// =============================================================================
// Rebaser
// =============================================================================

func NewRebaser() (*Rebaser, error) {
	return nil, ErrSessionNotEnabled
}

func (rb *Rebaser) Configure(rebaseData []byte) error {
	return ErrSessionNotEnabled
}

func (rb *Rebaser) Rebase(r io.Reader, w io.Writer) error {
	return ErrSessionNotEnabled
}

func (rb *Rebaser) Close() {
}
