package api

import (
	"bytes"
	"testing"
)

func TestStreamFileChunks(t *testing.T) {
	payload := bytes.Repeat([]byte("a"), 600*1024+17)
	type call struct {
		size   int
		append bool
	}
	var calls []call
	written, err := streamFileChunks(bytes.NewReader(payload), make([]byte, 256*1024), func(data []byte, appendChunk bool) error {
		calls = append(calls, call{size: len(data), append: appendChunk})
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if written != int64(len(payload)) {
		t.Fatalf("written = %d, want %d", written, len(payload))
	}
	if len(calls) != 3 {
		t.Fatalf("calls = %d, want 3", len(calls))
	}
	if calls[0].size != 256*1024 || calls[0].append {
		t.Fatalf("first call = %+v, want 256KiB overwrite", calls[0])
	}
	if calls[1].size != 256*1024 || !calls[1].append {
		t.Fatalf("second call = %+v, want 256KiB append", calls[1])
	}
	if calls[2].size != len(payload)-512*1024 || !calls[2].append {
		t.Fatalf("last call = %+v", calls[2])
	}
}

func TestStreamFileChunksCreatesEmptyFile(t *testing.T) {
	calls := 0
	written, err := streamFileChunks(bytes.NewReader(nil), make([]byte, 1024), func(data []byte, appendChunk bool) error {
		calls++
		if len(data) != 0 || appendChunk {
			t.Fatalf("empty file call data=%d append=%v", len(data), appendChunk)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if written != 0 || calls != 1 {
		t.Fatalf("written=%d calls=%d, want 0/1", written, calls)
	}
}
