// Copyright (C) 2019 G.J.R. Timmer <gjr.timmer@gmail.com>.
//
// Use of this source code is governed by an MIT-style
// license that can be found in the LICENSE file.

//go:build sqlite_session
// +build sqlite_session

package sqlite3

/*
#include <stdlib.h>
#include <string.h>

#ifndef USE_LIBSQLITE3
#include "sqlite3-binding.h"
#else
#include <sqlite3.h>
#endif
*/
import "C"
import (
	"io"
)

// sessionWriteContext holds the context for streaming changeset output.
// It wraps an io.Writer and tracks any error that occurs during writing.
type sessionWriteContext struct {
	w   io.Writer
	err error
}

// sessionReadContext holds the context for streaming changeset input.
// It wraps an io.Reader and tracks any error that occurs during reading.
type sessionReadContext struct {
	r   io.Reader
	err error
}

// sessionApplyContext holds the context for streaming changeset application.
// It combines a read context for the changeset data with callback handlers for
// table filtering and conflict resolution, plus a connection reference and
// an iterator for conflict handling.
type sessionApplyContext struct {
	readCtx  *sessionReadContext
	filter   TableFilter
	conflict ConflictHandler
	conn     *SQLiteConn
	iter     *ChangesetIterator
}
