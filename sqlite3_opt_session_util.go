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
extern int sessionOutputTrampoline(void *pOut, const void *pData, int nData);
*/
import "C"

import (
	"io"
	"runtime"
	"unsafe"
)

// InvertChangeset inverts a changeset, writing result to w.
// An inverted changeset undoes the original changes.
// For example, if the original changeset contains an INSERT,
// the inverted changeset will contain a DELETE.
func InvertChangeset(r io.Reader, w io.Writer) error {
	readCtx := &sessionReadContext{r: r}
	writeCtx := &sessionWriteContext{w: w}

	readHandle := newHandle(nil, readCtx)
	defer deleteHandle(readHandle)

	writeHandle := newHandle(nil, writeCtx)
	defer deleteHandle(writeHandle)

	rc := C.sqlite3changeset_invert_strm(
		(*[0]byte)(C.sessionInputTrampoline),
		readHandle,
		(*[0]byte)(C.sessionOutputTrampoline),
		writeHandle,
	)

	if readCtx.err != nil {
		return readCtx.err
	}
	if writeCtx.err != nil {
		return writeCtx.err
	}
	if rc != C.SQLITE_OK {
		return Error{Code: ErrNo(rc)}
	}
	return nil
}

// ConcatChangesets concatenates two changesets, writing result to w.
// The concatenated changeset contains all changes from both input changesets
// in the order they appear.
func ConcatChangesets(r1, r2 io.Reader, w io.Writer) error {
	readCtx1 := &sessionReadContext{r: r1}
	readCtx2 := &sessionReadContext{r: r2}
	writeCtx := &sessionWriteContext{w: w}

	readHandle1 := newHandle(nil, readCtx1)
	defer deleteHandle(readHandle1)

	readHandle2 := newHandle(nil, readCtx2)
	defer deleteHandle(readHandle2)

	writeHandle := newHandle(nil, writeCtx)
	defer deleteHandle(writeHandle)

	rc := C.sqlite3changeset_concat_strm(
		(*[0]byte)(C.sessionInputTrampoline),
		readHandle1,
		(*[0]byte)(C.sessionInputTrampoline),
		readHandle2,
		(*[0]byte)(C.sessionOutputTrampoline),
		writeHandle,
	)

	if readCtx1.err != nil {
		return readCtx1.err
	}
	if readCtx2.err != nil {
		return readCtx2.err
	}
	if writeCtx.err != nil {
		return writeCtx.err
	}
	if rc != C.SQLITE_OK {
		return Error{Code: ErrNo(rc)}
	}
	return nil
}

// Changegroup combines multiple changesets into one.
// Unlike concatenation, a changegroup can coalesce changes.
// For example, if one changeset INSERTs a row and another DELETEs it,
// the combined changeset will contain neither operation.
type Changegroup struct {
	p *C.sqlite3_changegroup
}

// NewChangegroup creates a new changegroup.
func NewChangegroup() (*Changegroup, error) {
	var p *C.sqlite3_changegroup
	rc := C.sqlite3changegroup_new(&p)
	if rc != C.SQLITE_OK {
		return nil, Error{Code: ErrNo(rc)}
	}

	g := &Changegroup{p: p}
	runtime.SetFinalizer(g, (*Changegroup).Close)
	return g, nil
}

// Add adds a changeset from reader to the group.
// The changeset is merged with any changesets previously added.
func (g *Changegroup) Add(r io.Reader) error {
	if g.p == nil {
		return Error{Code: ErrNo(C.SQLITE_MISUSE)}
	}

	readCtx := &sessionReadContext{r: r}
	handle := newHandle(nil, readCtx)
	defer deleteHandle(handle)

	rc := C.sqlite3changegroup_add_strm(
		g.p,
		(*[0]byte)(C.sessionInputTrampoline),
		handle,
	)

	if readCtx.err != nil {
		return readCtx.err
	}
	if rc != C.SQLITE_OK {
		return Error{Code: ErrNo(rc)}
	}
	return nil
}

// Output writes the combined changeset to the writer.
// This changeset represents all changes added to the group,
// with operations coalesced where possible.
func (g *Changegroup) Output(w io.Writer) error {
	if g.p == nil {
		return Error{Code: ErrNo(C.SQLITE_MISUSE)}
	}

	writeCtx := &sessionWriteContext{w: w}
	handle := newHandle(nil, writeCtx)
	defer deleteHandle(handle)

	rc := C.sqlite3changegroup_output_strm(
		g.p,
		(*[0]byte)(C.sessionOutputTrampoline),
		handle,
	)

	if writeCtx.err != nil {
		return writeCtx.err
	}
	if rc != C.SQLITE_OK {
		return Error{Code: ErrNo(rc)}
	}
	return nil
}

// Close releases changegroup resources.
// It is safe to call Close multiple times.
func (g *Changegroup) Close() error {
	if g.p != nil {
		C.sqlite3changegroup_delete(g.p)
		g.p = nil
		runtime.SetFinalizer(g, nil)
	}
	return nil
}

// Rebaser rebases changesets after conflict resolution.
// A rebaser is used in collaborative scenarios where multiple users
// may be making concurrent changes. When conflicts occur, the rebaser
// can transform a changeset so it can be applied after conflict resolution.
type Rebaser struct {
	p *C.sqlite3_rebaser
}

// NewRebaser creates a new rebaser.
func NewRebaser() (*Rebaser, error) {
	var p *C.sqlite3_rebaser
	rc := C.sqlite3rebaser_create(&p)
	if rc != C.SQLITE_OK {
		return nil, Error{Code: ErrNo(rc)}
	}

	r := &Rebaser{p: p}
	runtime.SetFinalizer(r, (*Rebaser).Close)
	return r, nil
}

// Configure configures the rebaser with rebase data.
// The rebase data is typically obtained from ApplyChangesetV2
// when the SQLITE_CHANGESETAPPLY_FKNOACTION flag is used.
func (rb *Rebaser) Configure(rebaseData []byte) error {
	if rb.p == nil {
		return Error{Code: ErrNo(C.SQLITE_MISUSE)}
	}

	if len(rebaseData) == 0 {
		return nil
	}

	rc := C.sqlite3rebaser_configure(
		rb.p,
		C.int(len(rebaseData)),
		unsafe.Pointer(&rebaseData[0]),
	)
	if rc != C.SQLITE_OK {
		return Error{Code: ErrNo(rc)}
	}
	return nil
}

// Rebase rebases a changeset according to configured rebase data.
// The rebased changeset is written to the writer.
// The rebased changeset will contain the same logical changes but
// may have different row IDs or other internal details to avoid conflicts.
func (rb *Rebaser) Rebase(r io.Reader, w io.Writer) error {
	if rb.p == nil {
		return Error{Code: ErrNo(C.SQLITE_MISUSE)}
	}

	readCtx := &sessionReadContext{r: r}
	writeCtx := &sessionWriteContext{w: w}

	readHandle := newHandle(nil, readCtx)
	defer deleteHandle(readHandle)

	writeHandle := newHandle(nil, writeCtx)
	defer deleteHandle(writeHandle)

	rc := C.sqlite3rebaser_rebase_strm(
		rb.p,
		(*[0]byte)(C.sessionInputTrampoline),
		readHandle,
		(*[0]byte)(C.sessionOutputTrampoline),
		writeHandle,
	)

	if readCtx.err != nil {
		return readCtx.err
	}
	if writeCtx.err != nil {
		return writeCtx.err
	}
	if rc != C.SQLITE_OK {
		return Error{Code: ErrNo(rc)}
	}
	return nil
}

// Close releases rebaser resources.
// It is safe to call Close multiple times.
func (rb *Rebaser) Close() {
	if rb.p != nil {
		C.sqlite3rebaser_delete(rb.p)
		rb.p = nil
		runtime.SetFinalizer(rb, nil)
	}
}
