package agent

import "sync"

// byteRing is a fixed-capacity ring buffer of raw bytes. It is the per-terminal
// scrollback store: pump output ALWAYS lands here so a (re)attaching browser can
// be replayed the recent screen state even when no connection is live. Once the
// ring wraps, snapshot reports truncated=true.
type byteRing struct {
	mu        sync.Mutex
	buf       []byte
	size      int  // bytes currently held (≤ cap)
	start     int  // index of the oldest byte
	wrapped   bool // true once total writes exceeded cap
	totalSeen int
}

// newByteRing builds a ring holding at most capBytes.
func newByteRing(capBytes int) *byteRing {
	if capBytes <= 0 {
		capBytes = 1
	}
	return &byteRing{buf: make([]byte, capBytes)}
}

// write appends p, overwriting the oldest bytes once full.
func (r *byteRing) write(p []byte) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.totalSeen += len(p)
	capn := len(r.buf)

	// Only the last capn bytes of the whole stream can survive — if p alone is
	// bigger than the ring, keep just its tail.
	if len(p) >= capn {
		copy(r.buf, p[len(p)-capn:])
		r.start = 0
		r.size = capn
		r.wrapped = true
		return
	}

	for _, b := range p {
		idx := (r.start + r.size) % capn
		if r.size < capn {
			r.buf[idx] = b
			r.size++
		} else {
			// Full: overwrite oldest, advance start.
			r.buf[r.start] = b
			r.start = (r.start + 1) % capn
			r.wrapped = true
		}
	}
}

// snapshot returns the buffered bytes in order plus whether older bytes were
// dropped (the stream exceeded the ring at some point).
func (r *byteRing) snapshot() ([]byte, bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	out := make([]byte, r.size)
	capn := len(r.buf)
	for i := 0; i < r.size; i++ {
		out[i] = r.buf[(r.start+i)%capn]
	}
	return out, r.wrapped
}
