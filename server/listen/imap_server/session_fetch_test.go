package imap_server

import (
	"errors"
	"io"
	"testing"
)

type testBodyWriteCloser struct {
	written  int
	writeErr error
	closeErr error
}

func (w *testBodyWriteCloser) Write(content []byte) (int, error) {
	if w.written >= 0 {
		return w.written, w.writeErr
	}
	return len(content), w.writeErr
}

func (w *testBodyWriteCloser) Close() error {
	return w.closeErr
}

func TestWriteBodySectionContentPropagatesWriteAndCloseErrors(t *testing.T) {
	writeErr := errors.New("write failed")
	closeErr := errors.New("close failed")
	writer := &testBodyWriteCloser{written: 0, writeErr: writeErr, closeErr: closeErr}

	err := writeBodySectionContent(writer, []byte("content"))
	if !errors.Is(err, writeErr) || !errors.Is(err, closeErr) {
		t.Fatalf("error = %v, want write and close errors", err)
	}
}

func TestWriteBodySectionContentRejectsShortWrite(t *testing.T) {
	writer := &testBodyWriteCloser{written: 2}
	if err := writeBodySectionContent(writer, []byte("data")); !errors.Is(err, io.ErrShortWrite) {
		t.Fatalf("error = %v, want io.ErrShortWrite", err)
	}
}

func TestWriteBodySectionContentPropagatesCloseError(t *testing.T) {
	closeErr := errors.New("close failed")
	writer := &testBodyWriteCloser{written: -1, closeErr: closeErr}
	if err := writeBodySectionContent(writer, nil); !errors.Is(err, closeErr) {
		t.Fatalf("error = %v, want close error", err)
	}
}
