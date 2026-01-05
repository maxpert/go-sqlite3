// Copyright (C) 2019 G.J.R. Timmer <gjr.timmer@gmail.com>.
//
// Use of this source code is governed by an MIT-style
// license that can be found in the LICENSE file.

//go:build sqlite_session
// +build sqlite_session

package sqlite3

/*
#ifndef USE_LIBSQLITE3
#include "sqlite3-binding.h"
#else
#include <sqlite3.h>
#endif
#include <stdlib.h>

extern int sessionInputTrampoline(void *pIn, void *pData, int *pnData);
*/
import "C"

import (
	"bytes"
	"errors"
	"io"
	"runtime"
	"unsafe"
)

// ChangesetIterator iterates over changes in a changeset or patchset.
type ChangesetIterator struct {
	p       *C.sqlite3_changeset_iter
	owned   bool // true = user must Close(), false = SQLite owns (conflict callback)
	hasRow  bool
	err     error
	readCtx *sessionReadContext
	handle  unsafe.Pointer
}

// NewChangesetIterator creates an iterator from an io.Reader.
// The iterator reads changeset data from the provided reader and allows
// iteration through the individual changes in the changeset.
func NewChangesetIterator(r io.Reader) (*ChangesetIterator, error) {
	return NewChangesetIteratorWithFlags(r, 0)
}

// NewChangesetIteratorWithFlags creates an iterator with the specified flags.
// The flags parameter can be used to specify IteratorInvert to invert the changeset.
func NewChangesetIteratorWithFlags(r io.Reader, flags IteratorFlags) (*ChangesetIterator, error) {
	readCtx := &sessionReadContext{r: r}
	handle := newHandle(nil, readCtx)

	var p *C.sqlite3_changeset_iter
	rc := C.sqlite3changeset_start_v2_strm(
		&p,
		(*[0]byte)(C.sessionInputTrampoline),
		handle,
		C.int(flags),
	)

	if rc != C.SQLITE_OK {
		deleteHandle(handle)
		if readCtx.err != nil {
			return nil, readCtx.err
		}
		return nil, Error{Code: ErrNo(rc)}
	}

	iter := &ChangesetIterator{
		p:       p,
		owned:   true, // User-created iterators are owned by the user
		readCtx: readCtx,
		handle:  handle,
	}
	runtime.SetFinalizer(iter, (*ChangesetIterator).Close)
	return iter, nil
}

// NewChangesetIteratorBytes creates an iterator from a byte slice.
// This is a convenience function that wraps the bytes in a bytes.Reader.
func NewChangesetIteratorBytes(data []byte) (*ChangesetIterator, error) {
	return NewChangesetIterator(bytes.NewReader(data))
}

// Next advances the iterator to the next change in the changeset.
// It returns true if there is a next change, false if iteration is complete or an error occurred.
// Call Err() to check for errors after Next() returns false.
func (ci *ChangesetIterator) Next() bool {
	if ci.p == nil {
		return false
	}

	rc := C.sqlite3changeset_next(ci.p)
	if rc == C.SQLITE_ROW {
		ci.hasRow = true
		return true
	}

	ci.hasRow = false
	if rc == C.SQLITE_DONE {
		return false
	}

	// Error occurred
	if ci.readCtx != nil && ci.readCtx.err != nil {
		ci.err = ci.readCtx.err
	} else {
		ci.err = Error{Code: ErrNo(rc)}
	}
	return false
}

// Op returns information about the current change.
// It returns:
// - op: The operation type (SQLITE_INSERT, SQLITE_UPDATE, or SQLITE_DELETE)
// - tableName: The name of the table being modified
// - nCols: The number of columns in the table
// - indirect: Whether the change is indirect (e.g., from a trigger)
// - err: Any error that occurred
//
// This method should only be called when the iterator is positioned on a valid row
// (i.e., after Next() returns true).
func (ci *ChangesetIterator) Op() (op int, tableName string, nCols int, indirect bool, err error) {
	// For conflict callbacks (!owned), SQLite has already positioned the iterator
	if !ci.hasRow && ci.owned {
		return 0, "", 0, false, errors.New("iterator not on valid row")
	}

	var zTab *C.char
	var nCol C.int
	var cOp C.int
	var bIndirect C.int

	rc := C.sqlite3changeset_op(ci.p, &zTab, &nCol, &cOp, &bIndirect)
	if rc != C.SQLITE_OK {
		return 0, "", 0, false, Error{Code: ErrNo(rc)}
	}

	return int(cOp), C.GoString(zTab), int(nCol), bIndirect != 0, nil
}

// OldValue returns the old value for column i in the current change.
// This is valid for DELETE and UPDATE operations, but returns an error for INSERT.
// The returned value is converted to the appropriate Go type (int64, float64, []byte, string, or nil).
func (ci *ChangesetIterator) OldValue(i int) (interface{}, error) {
	var pValue *C.sqlite3_value
	rc := C.sqlite3changeset_old(ci.p, C.int(i), &pValue)
	if rc != C.SQLITE_OK {
		return nil, Error{Code: ErrNo(rc)}
	}
	return sqliteValueToGo(pValue), nil
}

// NewValue returns the new value for column i in the current change.
// This is valid for INSERT and UPDATE operations, but returns an error for DELETE.
// The returned value is converted to the appropriate Go type (int64, float64, []byte, string, or nil).
func (ci *ChangesetIterator) NewValue(i int) (interface{}, error) {
	var pValue *C.sqlite3_value
	rc := C.sqlite3changeset_new(ci.p, C.int(i), &pValue)
	if rc != C.SQLITE_OK {
		return nil, Error{Code: ErrNo(rc)}
	}
	return sqliteValueToGo(pValue), nil
}

// ConflictValue returns the conflicting value for column i.
// This is only valid when called from within a conflict handler during changeset application.
func (ci *ChangesetIterator) ConflictValue(i int) (interface{}, error) {
	var pValue *C.sqlite3_value
	rc := C.sqlite3changeset_conflict(ci.p, C.int(i), &pValue)
	if rc != C.SQLITE_OK {
		return nil, Error{Code: ErrNo(rc)}
	}
	return sqliteValueToGo(pValue), nil
}

// FKConflicts returns the number of foreign key constraint violations.
// This is only valid when called from a conflict handler with ConflictForeignKey.
func (ci *ChangesetIterator) FKConflicts() (int, error) {
	var nOut C.int
	rc := C.sqlite3changeset_fk_conflicts(ci.p, &nOut)
	if rc != C.SQLITE_OK {
		return 0, Error{Code: ErrNo(rc)}
	}
	return int(nOut), nil
}

// PK returns which columns are part of the primary key.
// Returns a boolean slice where true indicates the column is a PK column.
func (ci *ChangesetIterator) PK() ([]bool, error) {
	var pabPK *C.uchar
	var nCol C.int

	rc := C.sqlite3changeset_pk(ci.p, &pabPK, &nCol)
	if rc != C.SQLITE_OK {
		return nil, Error{Code: ErrNo(rc)}
	}

	// Convert C array to Go slice
	result := make([]bool, int(nCol))
	pkSlice := (*[1 << 20]C.uchar)(unsafe.Pointer(pabPK))[:nCol:nCol]
	for i := 0; i < int(nCol); i++ {
		result[i] = pkSlice[i] != 0
	}

	return result, nil
}

// Err returns any error that occurred during iteration.
// This should be called after Next() returns false to distinguish between
// normal completion and an error.
func (ci *ChangesetIterator) Err() error {
	return ci.err
}

// Close releases the resources associated with the iterator.
// It is safe to call Close multiple times.
// The iterator is automatically closed by the finalizer if not explicitly closed.
// If the iterator is not owned (e.g., from a conflict callback), Close is a no-op.
func (ci *ChangesetIterator) Close() error {
	// Only finalize if we own the iterator
	if ci.p != nil && ci.owned {
		rc := C.sqlite3changeset_finalize(ci.p)
		ci.p = nil
		runtime.SetFinalizer(ci, nil)

		if ci.handle != nil {
			deleteHandle(ci.handle)
			ci.handle = nil
		}

		if ci.readCtx != nil && ci.readCtx.err != nil {
			return ci.readCtx.err
		}
		if rc != C.SQLITE_OK {
			return Error{Code: ErrNo(rc)}
		}
	}
	return nil
}

// sqliteValueToGo converts a sqlite3_value to the appropriate Go type.
// Returns int64 for INTEGER, float64 for FLOAT, []byte for BLOB,
// string for TEXT, and nil for NULL.
func sqliteValueToGo(v *C.sqlite3_value) interface{} {
	if v == nil {
		return nil
	}
	switch C.sqlite3_value_type(v) {
	case C.SQLITE_INTEGER:
		return int64(C.sqlite3_value_int64(v))
	case C.SQLITE_FLOAT:
		return float64(C.sqlite3_value_double(v))
	case C.SQLITE_BLOB:
		n := C.sqlite3_value_bytes(v)
		p := C.sqlite3_value_blob(v)
		return C.GoBytes(p, n)
	case C.SQLITE_TEXT:
		return C.GoString((*C.char)(unsafe.Pointer(C.sqlite3_value_text(v))))
	case C.SQLITE_NULL:
		return nil
	default:
		return nil
	}
}
