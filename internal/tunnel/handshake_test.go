package tunnel

import (
	"bufio"
	"bytes"
	"testing"
)

// TestEditorHeaderWireFormat pins the editor handshake to its original D27 wire
// (bare "sessionId\n") so a hub/agent version skew never breaks the editor path.
func TestEditorHeaderWireFormat(t *testing.T) {
	var b bytes.Buffer
	if err := WriteStreamHeader(&b, "sess-abc123"); err != nil {
		t.Fatal(err)
	}
	if got := b.String(); got != "sess-abc123\n" {
		t.Fatalf("editor wire = %q, want %q", got, "sess-abc123\n")
	}
	tgt, err := ReadStreamHeader(bufio.NewReader(&b))
	if err != nil {
		t.Fatal(err)
	}
	if tgt.Kind != KindEditor || tgt.SessionID != "sess-abc123" {
		t.Fatalf("got %+v, want editor sess-abc123", tgt)
	}
}

func TestPreviewHeaderRoundTrip(t *testing.T) {
	var b bytes.Buffer
	if err := WritePreviewHeader(&b, 5173); err != nil {
		t.Fatal(err)
	}
	tgt, err := ReadStreamHeader(bufio.NewReader(&b))
	if err != nil {
		t.Fatal(err)
	}
	if tgt.Kind != KindPreview || tgt.Port != 5173 {
		t.Fatalf("got %+v, want preview 5173", tgt)
	}
}

func TestPreviewHeaderRejectsBadPort(t *testing.T) {
	for _, line := range []string{"#preview 0\n", "#preview 70000\n", "#preview abc\n"} {
		if _, err := ReadStreamHeader(bufio.NewReader(bytes.NewBufferString(line))); err == nil {
			t.Fatalf("expected error for %q", line)
		}
	}
}
