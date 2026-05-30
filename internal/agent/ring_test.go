package agent

import (
	"bytes"
	"testing"
)

func TestByteRingUnderCap(t *testing.T) {
	r := newByteRing(16)
	r.write([]byte("hello"))
	r.write([]byte("world"))
	got, wrapped := r.snapshot()
	if wrapped {
		t.Fatalf("should not be wrapped")
	}
	if !bytes.Equal(got, []byte("helloworld")) {
		t.Fatalf("got %q", got)
	}
}

func TestByteRingWrap(t *testing.T) {
	r := newByteRing(8)
	r.write([]byte("abcdef")) // 6
	r.write([]byte("ghij"))   // total 10, cap 8 → keep last 8: "cdefghij"
	got, wrapped := r.snapshot()
	if !wrapped {
		t.Fatalf("should be wrapped")
	}
	if !bytes.Equal(got, []byte("cdefghij")) {
		t.Fatalf("got %q want %q", got, "cdefghij")
	}
}

// A single write larger than the ring keeps only its tail.
func TestByteRingBigWrite(t *testing.T) {
	r := newByteRing(4)
	r.write([]byte("0123456789"))
	got, wrapped := r.snapshot()
	if !wrapped {
		t.Fatalf("should be wrapped")
	}
	if !bytes.Equal(got, []byte("6789")) {
		t.Fatalf("got %q want %q", got, "6789")
	}
}

// Snapshot must reflect order after multiple wraps.
func TestByteRingMultiWrap(t *testing.T) {
	r := newByteRing(4)
	for _, s := range []string{"a", "b", "c", "d", "e", "f"} {
		r.write([]byte(s))
	}
	got, _ := r.snapshot()
	if !bytes.Equal(got, []byte("cdef")) {
		t.Fatalf("got %q want %q", got, "cdef")
	}
}
