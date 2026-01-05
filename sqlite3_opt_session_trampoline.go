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
*/
import "C"
import (
	"io"
	"unsafe"
)

// sessionInputTrampoline is called by SQLite to read changeset data.
// This is used by changeset iterators and utility functions like
// sqlite3changeset_invert_strm, sqlite3changeset_concat_strm, etc.
//
//export sessionInputTrampoline
func sessionInputTrampoline(pIn unsafe.Pointer, pData unsafe.Pointer, pnData *C.int) C.int {
	if pIn == nil || pData == nil || pnData == nil {
		return C.SQLITE_MISUSE
	}

	handle := pIn
	val := lookupHandle(handle)

	// Try sessionReadContext first
	if readCtx, ok := val.(*sessionReadContext); ok {
		n := int(*pnData)
		buf := (*[1 << 30]byte)(pData)[:n:n]

		nRead, err := readCtx.r.Read(buf)
		*pnData = C.int(nRead)

		if err != nil {
			if err == io.EOF {
				// If we read some data before EOF, return it successfully
				// SQLite will call us again and we'll return 0 bytes
				if nRead > 0 {
					readCtx.err = nil
					return C.SQLITE_OK
				}
				// EOF with no data means end of stream
				readCtx.err = nil
				return C.SQLITE_OK
			}
			readCtx.err = err
			return C.SQLITE_IOERR_READ
		}
		return C.SQLITE_OK
	}

	// Try sessionApplyContext
	if applyCtx, ok := val.(*sessionApplyContext); ok {
		n := int(*pnData)
		buf := (*[1 << 30]byte)(pData)[:n:n]

		nRead, err := applyCtx.readCtx.r.Read(buf)
		*pnData = C.int(nRead)

		if err != nil {
			if err == io.EOF {
				applyCtx.readCtx.err = nil
				return C.SQLITE_OK
			}
			applyCtx.readCtx.err = err
			return C.SQLITE_IOERR_READ
		}
		return C.SQLITE_OK
	}

	return C.SQLITE_MISUSE
}

// sessionOutputTrampoline is called by SQLite to write changeset data.
// This is used by utility functions like sqlite3changeset_invert_strm,
// sqlite3changeset_concat_strm, etc.
//
//export sessionOutputTrampoline
func sessionOutputTrampoline(pOut unsafe.Pointer, pData unsafe.Pointer, nData C.int) C.int {
	if pOut == nil || pData == nil {
		return C.SQLITE_MISUSE
	}

	handle := pOut
	writeCtx := lookupHandle(handle).(*sessionWriteContext)

	n := int(nData)
	buf := (*[1 << 30]byte)(pData)[:n:n]

	_, err := writeCtx.w.Write(buf)
	if err != nil {
		writeCtx.err = err
		return C.SQLITE_IOERR_WRITE
	}

	return C.SQLITE_OK
}

// sessionFilterTrampoline is called by SQLite to determine if a table should be tracked.
// This is used by Session.AttachTable and changeset application.
//
//export sessionFilterTrampoline
func sessionFilterTrampoline(pCtx unsafe.Pointer, zTab *C.char) C.int {
	if pCtx == nil || zTab == nil {
		return 0
	}

	hval := lookupHandleVal(pCtx)

	// The context might be a Session (for AttachTable) or sessionApplyContext (for ApplyChangeset)
	var filter TableFilter
	switch v := hval.val.(type) {
	case TableFilter:
		filter = v
	case *sessionApplyContext:
		filter = v.filter
	default:
		return C.int(0) // Don't filter
	}

	if filter == nil {
		return C.int(1) // Include all tables
	}

	tableName := C.GoString(zTab)
	if filter(tableName) {
		return C.int(1) // Include this table
	}
	return C.int(0) // Exclude this table
}

// sessionConflictTrampoline is called by SQLite when a conflict occurs during changeset application.
//
//export sessionConflictTrampoline
func sessionConflictTrampoline(pCtx unsafe.Pointer, eConflict C.int, pIter unsafe.Pointer) C.int {
	if pCtx == nil {
		return C.int(ConflictActionAbort)
	}

	ctx := lookupHandle(pCtx).(*sessionApplyContext)
	if ctx.conflict == nil {
		return C.int(ConflictActionAbort)
	}

	// Wrap the C iterator - NOT owned by us (SQLite owns it during apply)
	iter := &ChangesetIterator{
		p:      (*C.sqlite3_changeset_iter)(pIter),
		owned:  false, // SQLite owns this iterator, not us
		hasRow: true,  // Already positioned on the conflicting change
	}

	action := ctx.conflict(ConflictType(eConflict), iter)
	return C.int(action)
}
