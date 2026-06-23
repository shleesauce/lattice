package tunnel

import (
	"bufio"
	"fmt"
	"io"
	"strconv"
	"strings"
)

// Stream handshake (D27 / D32). Every yamux stream the hub opens carries a
// one-line header naming where the agent should splice it:
//
//   - EDITOR streams target a code-server the agent hosts per editor session, so
//     the header is just the bare sessionId — kept byte-identical to the original
//     D27 wire so a hub/agent version skew never breaks the working editor path.
//   - PREVIEW streams target an arbitrary dev server on the agent's loopback, so
//     the header is "#preview <port>". '#' can't begin a sessionId (hex/uuid), so
//     the two forms are unambiguous and an un-updated agent simply fails the
//     preview cleanly (logs "no editor for session #preview …") without crashing.
//
// After the header the stream is a transparent byte pipe carrying the browser's
// raw HTTP/WebSocket bytes; the hub owns all proxy logic, the agent is a dumb
// per-target connector.

// Kind selects what an accepted tunnel stream targets.
type Kind int

const (
	KindEditor Kind = iota
	KindPreview
)

// StreamTarget is the parsed handshake: an editor session, or a loopback port.
type StreamTarget struct {
	Kind      Kind
	SessionID string // KindEditor
	Port      int    // KindPreview
}

// previewPrefix marks a preview stream header.
const previewPrefix = "#preview "

// WriteStreamHeader writes the editor sessionId line into a freshly opened stream.
// It MUST be the first thing written, before any HTTP bytes. Wire format is the
// original D27 one (sessionId + "\n") and must stay that way.
func WriteStreamHeader(w io.Writer, sessionID string) error {
	_, err := io.WriteString(w, sessionID+"\n")
	return err
}

// WritePreviewHeader writes the preview port line ("#preview <port>\n") into a
// freshly opened stream. Like the editor header it MUST precede any HTTP bytes.
func WritePreviewHeader(w io.Writer, port int) error {
	_, err := io.WriteString(w, fmt.Sprintf("%s%d\n", previewPrefix, port))
	return err
}

// ReadStreamHeader reads and parses the target line from an accepted stream. The
// caller must keep reading the REMAINING bytes from the same *bufio.Reader (not
// the raw stream) so any bytes bufio buffered past the newline aren't lost.
func ReadStreamHeader(r *bufio.Reader) (StreamTarget, error) {
	line, err := r.ReadString('\n')
	if err != nil {
		return StreamTarget{}, err
	}
	line = strings.TrimRight(line, "\r\n")

	if rest, ok := strings.CutPrefix(line, previewPrefix); ok {
		port, err := strconv.Atoi(strings.TrimSpace(rest))
		if err != nil || port < 1 || port > 65535 {
			return StreamTarget{}, fmt.Errorf("invalid preview port %q", rest)
		}
		return StreamTarget{Kind: KindPreview, Port: port}, nil
	}
	return StreamTarget{Kind: KindEditor, SessionID: line}, nil
}
