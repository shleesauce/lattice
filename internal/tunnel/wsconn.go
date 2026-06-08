// Package tunnel carries the IDE-milestone editor traffic (D27). The agent dials
// a SECOND persistent WebSocket out to the hub (preserving D2: zero inbound on
// leaves) and both ends run a yamux session over it, multiplexing one stream per
// browser↔code-server connection. yamux speaks net.Conn, so this package adapts
// a gorilla *websocket.Conn into one, and pins the shared yamux config so the two
// ends agree on framing and timeouts.
package tunnel

import (
	"io"
	"net"
	"sync"
	"time"

	"github.com/gorilla/websocket"
	"github.com/hashicorp/yamux"
)

// WSConn adapts a gorilla WebSocket into a net.Conn so yamux can multiplex over
// it. Each yamux frame is sent as one binary WS message; Read pulls bytes out of
// the current inbound message, advancing to the next on EOF. gorilla permits one
// concurrent reader + one concurrent writer, which is exactly yamux's access
// pattern (a single recv loop + a single send loop); the write mutex is cheap
// insurance against any stray concurrent writer (e.g. a close).
type WSConn struct {
	ws *websocket.Conn

	readMu sync.Mutex
	cur    io.Reader // current inbound message reader (nil ⇒ fetch next)

	writeMu sync.Mutex
}

// NewWSConn wraps a gorilla connection. The connection must already be upgraded
// (server) or dialed (client); no further WS handshake happens here.
func NewWSConn(ws *websocket.Conn) *WSConn {
	return &WSConn{ws: ws}
}

// Read returns bytes from the inbound WS message stream, transparently spanning
// message boundaries so yamux sees one continuous byte stream.
func (c *WSConn) Read(p []byte) (int, error) {
	c.readMu.Lock()
	defer c.readMu.Unlock()
	for {
		if c.cur == nil {
			mt, r, err := c.ws.NextReader()
			if err != nil {
				return 0, err
			}
			// Control frames are handled internally by gorilla; only data frames
			// reach here. Treat text and binary identically (we only send binary).
			if mt != websocket.BinaryMessage && mt != websocket.TextMessage {
				continue
			}
			c.cur = r
		}
		n, err := c.cur.Read(p)
		if err == io.EOF {
			c.cur = nil
			if n > 0 {
				return n, nil // hand back what we got; next Read fetches the next msg
			}
			continue
		}
		return n, err
	}
}

// Write sends p as a single binary WS message.
func (c *WSConn) Write(p []byte) (int, error) {
	c.writeMu.Lock()
	defer c.writeMu.Unlock()
	if err := c.ws.WriteMessage(websocket.BinaryMessage, p); err != nil {
		return 0, err
	}
	return len(p), nil
}

// Close closes the underlying WebSocket.
func (c *WSConn) Close() error { return c.ws.Close() }

// LocalAddr / RemoteAddr proxy the underlying socket addresses.
func (c *WSConn) LocalAddr() net.Addr  { return c.ws.LocalAddr() }
func (c *WSConn) RemoteAddr() net.Addr { return c.ws.RemoteAddr() }

// SetDeadline sets both read and write deadlines.
func (c *WSConn) SetDeadline(t time.Time) error {
	if err := c.ws.SetReadDeadline(t); err != nil {
		return err
	}
	return c.ws.SetWriteDeadline(t)
}

// SetReadDeadline bounds the next Read.
func (c *WSConn) SetReadDeadline(t time.Time) error { return c.ws.SetReadDeadline(t) }

// SetWriteDeadline bounds the next Write (yamux uses this for ConnectionWriteTimeout).
func (c *WSConn) SetWriteDeadline(t time.Time) error { return c.ws.SetWriteDeadline(t) }

// Config returns the shared yamux configuration both ends use. Keepalive lets a
// half-open tunnel (sleeping laptop, network partition) be detected and torn
// down so the agent's reconnect loop can re-establish it. Logs are silenced —
// the hub/agent already log tunnel lifecycle at a higher level.
func Config() *yamux.Config {
	cfg := yamux.DefaultConfig()
	cfg.EnableKeepAlive = true
	cfg.KeepAliveInterval = 30 * time.Second
	cfg.ConnectionWriteTimeout = 10 * time.Second
	cfg.LogOutput = io.Discard
	return cfg
}
