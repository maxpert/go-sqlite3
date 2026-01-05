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
extern int sessionFilterTrampoline(void *pCtx, const char *zTab);
extern int sessionConflictTrampoline(void *pCtx, int eConflict, void *pIter);
*/
import "C"

import (
	"bytes"
	"io"
	"unsafe"
)

// ApplyChangeset applies changeset from reader to database.
// The filter callback determines which tables should be included (nil = all tables).
// The conflict callback handles conflicts during application (nil = abort on conflict).
func (c *SQLiteConn) ApplyChangeset(r io.Reader, filter TableFilter, conflict ConflictHandler) error {
	return c.applyChangesetInternal(r, nil, filter, conflict, 0)
}

// ApplyChangesetV2 applies changeset with flags, optionally outputs rebase data.
// The rebaseOut writer receives rebase information if not nil.
// The flags parameter controls application behavior (e.g., ApplyNoSavepoint, ApplyInvert).
func (c *SQLiteConn) ApplyChangesetV2(r io.Reader, rebaseOut io.Writer, filter TableFilter, conflict ConflictHandler, flags ApplyFlags) error {
	return c.applyChangesetInternal(r, rebaseOut, filter, conflict, flags)
}

// ApplyChangesetBytes applies changeset from byte slice.
// This is a convenience wrapper around ApplyChangeset.
func (c *SQLiteConn) ApplyChangesetBytes(data []byte, filter TableFilter, conflict ConflictHandler) error {
	return c.ApplyChangeset(bytes.NewReader(data), filter, conflict)
}

// applyChangesetInternal is the internal implementation for applying changesets.
func (c *SQLiteConn) applyChangesetInternal(r io.Reader, rebaseOut io.Writer, filter TableFilter, conflict ConflictHandler, flags ApplyFlags) error {
	readCtx := &sessionReadContext{r: r}
	applyCtx := &sessionApplyContext{
		readCtx:  readCtx,
		filter:   filter,
		conflict: conflict,
		conn:     c,
	}

	handle := newHandle(c, applyCtx)
	defer deleteHandle(handle)

	// Set up filter callback pointer
	var filterFn *[0]byte
	if filter != nil {
		filterFn = (*[0]byte)(C.sessionFilterTrampoline)
	}

	// Set up conflict callback pointer
	var conflictFn *[0]byte
	if conflict != nil {
		conflictFn = (*[0]byte)(C.sessionConflictTrampoline)
	}

	// Set up rebase output if requested
	var ppRebase *unsafe.Pointer
	var pnRebase *C.int
	var rebasePtr unsafe.Pointer
	var rebaseSize C.int

	if rebaseOut != nil {
		ppRebase = &rebasePtr
		pnRebase = &rebaseSize
	}

	rc := C.sqlite3changeset_apply_v2_strm(
		c.db,
		(*[0]byte)(C.sessionInputTrampoline),
		handle,
		filterFn,
		conflictFn,
		handle,
		ppRebase,
		pnRebase,
		C.int(flags),
	)

	// Handle rebase output
	if rebaseOut != nil && rebasePtr != nil && rebaseSize > 0 {
		rebaseData := C.GoBytes(rebasePtr, rebaseSize)
		C.sqlite3_free(rebasePtr)
		if _, err := rebaseOut.Write(rebaseData); err != nil {
			return err
		}
	}

	// Check for I/O errors first (they take precedence)
	if readCtx.err != nil {
		return readCtx.err
	}

	if rc != C.SQLITE_OK {
		return c.lastError()
	}
	return nil
}
