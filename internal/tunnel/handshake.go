package tunnel

import (
	"bufio"
	"io"
	"strings"
)

// Stream handshake (D27). Every yamux stream the hub opens carries a one-line
// header naming the editor session it targets, so the agent — which hosts one
// code-server per editor session on its own loopback port — can route the stream
// to the right backend. After the header the stream is a transparent byte pipe
// carrying the browser's raw HTTP/WebSocket bytes; the hub owns all code-server
// proxy logic, the agent is a dumb per-session connector.

// WriteStreamHeader writes the target sessionId line into a freshly opened stream.
// It MUST be the first thing written, before any HTTP bytes.
func WriteStreamHeader(w io.Writer, sessionID string) error {
	_, err := io.WriteString(w, sessionID+"\n")
	return err
}

// ReadStreamHeader reads the sessionId line from an accepted stream. The caller
// must keep reading the REMAINING bytes from the same *bufio.Reader (not the raw
// stream) so any bytes bufio buffered past the newline aren't lost.
func ReadStreamHeader(r *bufio.Reader) (string, error) {
	line, err := r.ReadString('\n')
	if err != nil {
		return "", err
	}
	return strings.TrimRight(line, "\r\n"), nil
}
