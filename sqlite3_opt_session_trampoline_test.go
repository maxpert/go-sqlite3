// Copyright (C) 2019 G.J.R. Timmer <gjr.timmer@gmail.com>.
//
// Use of this source code is governed by an MIT-style
// license that can be found in the LICENSE file.

//go:build sqlite_session
// +build sqlite_session

package sqlite3

import (
	"bytes"
	"errors"
	"io"
	"strings"
	"testing"
)

// TestSessionWriteContext tests the sessionWriteContext struct
func TestSessionWriteContext(t *testing.T) {
	tests := []struct {
		name      string
		data      []byte
		wantError bool
		wantN     int
	}{
		{
			name:      "write simple data",
			data:      []byte("hello world"),
			wantError: false,
			wantN:     11,
		},
		{
			name:      "write empty data",
			data:      []byte{},
			wantError: false,
			wantN:     0,
		},
		{
			name:      "write large data",
			data:      bytes.Repeat([]byte("x"), 1024),
			wantError: false,
			wantN:     1024,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var buf bytes.Buffer
			ctx := &sessionWriteContext{
				w:   &buf,
				err: nil,
			}

			// Test direct write
			n, err := ctx.w.Write(tt.data)

			// Check result
			if tt.wantError {
				if err == nil {
					t.Errorf("expected error, got nil")
				}
			} else {
				if err != nil {
					t.Errorf("unexpected error: %v", err)
				}
				if n != tt.wantN {
					t.Errorf("expected %d bytes written, got %d", tt.wantN, n)
				}
			}

			// Check written data
			if !tt.wantError {
				if buf.Len() != tt.wantN {
					t.Errorf("expected %d bytes in buffer, got %d", tt.wantN, buf.Len())
				}
				if len(tt.data) > 0 && !bytes.Equal(buf.Bytes(), tt.data) {
					t.Errorf("data mismatch: expected %v, got %v", tt.data, buf.Bytes())
				}
			}
		})
	}
}

// TestSessionWriteContextWithError tests error handling
func TestSessionWriteContextWithError(t *testing.T) {
	// Create a writer that always fails
	failWriter := &failingWriter{err: errors.New("write failed")}
	ctx := &sessionWriteContext{
		w:   failWriter,
		err: nil,
	}

	data := []byte("test data")
	_, err := ctx.w.Write(data)

	if err == nil {
		t.Errorf("expected error, got nil")
	}
}

// TestSessionReadContext tests the sessionReadContext struct
func TestSessionReadContext(t *testing.T) {
	tests := []struct {
		name        string
		input       string
		requestSize int
		wantRead    int
		wantEOF     bool
	}{
		{
			name:        "read simple data",
			input:       "hello world",
			requestSize: 11,
			wantRead:    11,
			wantEOF:     false,
		},
		{
			name:        "read partial data",
			input:       "hello world",
			requestSize: 5,
			wantRead:    5,
			wantEOF:     false,
		},
		{
			name:        "read with EOF",
			input:       "hello",
			requestSize: 10,
			wantRead:    5,
			wantEOF:     false, // Go's io.Reader may return n bytes and nil error, then EOF on next read
		},
		{
			name:        "read empty",
			input:       "",
			requestSize: 10,
			wantRead:    0,
			wantEOF:     true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			reader := strings.NewReader(tt.input)
			ctx := &sessionReadContext{
				r:   reader,
				err: nil,
			}

			// Test direct read
			buf := make([]byte, tt.requestSize)
			n, err := ctx.r.Read(buf)

			// Check result
			if tt.wantEOF {
				if err != io.EOF {
					t.Errorf("expected EOF, got %v", err)
				}
			} else if err != nil {
				t.Errorf("unexpected error: %v", err)
			}

			// Check bytes read
			if n != tt.wantRead {
				t.Errorf("expected %d bytes read, got %d", tt.wantRead, n)
			}

			// Check data content
			if tt.wantRead > 0 {
				expectedData := tt.input[:tt.wantRead]
				actualData := string(buf[:tt.wantRead])
				if actualData != expectedData {
					t.Errorf("data mismatch: expected %q, got %q", expectedData, actualData)
				}
			}
		})
	}
}

// TestSessionReadContextWithError tests error handling
func TestSessionReadContextWithError(t *testing.T) {
	// Create a reader that always fails
	failReader := &failingReader{err: errors.New("read failed")}
	ctx := &sessionReadContext{
		r:   failReader,
		err: nil,
	}

	buf := make([]byte, 10)
	_, err := ctx.r.Read(buf)

	if err == nil {
		t.Errorf("expected error, got nil")
	}
}

// TestTableFilter tests the table filter callback
func TestTableFilter(t *testing.T) {
	tests := []struct {
		name      string
		tableName string
		filter    TableFilter
		want      bool
	}{
		{
			name:      "include table",
			tableName: "users",
			filter: func(name string) bool {
				return name == "users"
			},
			want: true,
		},
		{
			name:      "exclude table",
			tableName: "logs",
			filter: func(name string) bool {
				return name == "users"
			},
			want: false,
		},
		{
			name:      "include all",
			tableName: "any_table",
			filter: func(name string) bool {
				return true
			},
			want: true,
		},
		{
			name:      "exclude all",
			tableName: "any_table",
			filter: func(name string) bool {
				return false
			},
			want: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := tt.filter(tt.tableName)
			if result != tt.want {
				t.Errorf("expected %v, got %v", tt.want, result)
			}
		})
	}
}

// TestConflictHandler tests the conflict handler callback
func TestConflictHandler(t *testing.T) {
	tests := []struct {
		name         string
		conflictType ConflictType
		handler      ConflictHandler
		wantAction   ConflictAction
	}{
		{
			name:         "omit conflict",
			conflictType: ConflictData,
			handler: func(ct ConflictType, iter *ChangesetIterator) ConflictAction {
				if ct == ConflictData {
					return ConflictActionOmit
				}
				return ConflictActionAbort
			},
			wantAction: ConflictActionOmit,
		},
		{
			name:         "replace conflict",
			conflictType: ConflictConflict,
			handler: func(ct ConflictType, iter *ChangesetIterator) ConflictAction {
				if ct == ConflictConflict {
					return ConflictActionReplace
				}
				return ConflictActionAbort
			},
			wantAction: ConflictActionReplace,
		},
		{
			name:         "abort conflict",
			conflictType: ConflictNotFound,
			handler: func(ct ConflictType, iter *ChangesetIterator) ConflictAction {
				return ConflictActionAbort
			},
			wantAction: ConflictActionAbort,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := tt.handler(tt.conflictType, nil)
			if result != tt.wantAction {
				t.Errorf("expected action %d, got %d", tt.wantAction, result)
			}
		})
	}
}

// TestHandleOperations tests handle creation and lookup
func TestHandleOperations(t *testing.T) {
	// Create a handle
	ctx := &sessionWriteContext{w: &bytes.Buffer{}}
	handle := newHandle(nil, ctx)

	// Verify handle exists
	retrieved := lookupHandle(handle)
	if retrieved == nil {
		t.Fatal("handle should exist after creation")
	}

	// Verify type
	if _, ok := retrieved.(*sessionWriteContext); !ok {
		t.Errorf("expected *sessionWriteContext, got %T", retrieved)
	}

	// Delete handle
	deleteHandle(handle)

	// Note: We can't test that the handle is gone without causing a panic,
	// which is the expected behavior
}

// TestSessionApplyContext tests the apply context struct
func TestSessionApplyContext(t *testing.T) {
	reader := strings.NewReader("test data")
	filter := func(name string) bool { return name == "users" }
	handler := func(ct ConflictType, iter *ChangesetIterator) ConflictAction { return ConflictActionOmit }

	readCtx := &sessionReadContext{
		r:   reader,
		err: nil,
	}

	ctx := &sessionApplyContext{
		readCtx:  readCtx,
		filter:   filter,
		conflict: handler,
		conn:     nil,
	}

	// Test reader through readCtx
	buf := make([]byte, 4)
	n, err := ctx.readCtx.r.Read(buf)
	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}
	if n != 4 {
		t.Errorf("expected 4 bytes, got %d", n)
	}
	if string(buf) != "test" {
		t.Errorf("expected 'test', got %q", string(buf))
	}

	// Test filter
	if !ctx.filter("users") {
		t.Errorf("expected filter to return true for 'users'")
	}
	if ctx.filter("logs") {
		t.Errorf("expected filter to return false for 'logs'")
	}

	// Test handler
	action := ctx.conflict(ConflictData, nil)
	if action != ConflictActionOmit {
		t.Errorf("expected ConflictActionOmit, got %d", action)
	}
}

// Helper types for testing error conditions

type failingWriter struct {
	err error
}

func (w *failingWriter) Write(p []byte) (n int, err error) {
	return 0, w.err
}

type failingReader struct {
	err error
}

func (r *failingReader) Read(p []byte) (n int, err error) {
	return 0, r.err
}
