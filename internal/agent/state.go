package agent

import (
	"context"
	"sync/atomic"
)

// agentState is the PROCESS-GLOBAL session registry (D18). It is created once in
// Run() and survives every WebSocket reconnect, so terminal + claude processes
// outlive the browser and the hub link. Each connection (session()) hands its
// outbound channel to the state via setSink; with no live connection the sink is
// nil and pumps write to their ring/event buffers only.
type agentState struct {
	terms   *terminals
	claudes *claudeSessions
}

// newAgentState builds the empty process-global registries, rooting every
// long-lived session process at baseCtx (the process-lifetime context from Run)
// so sessions outlive WebSocket reconnects (D18).
func newAgentState(baseCtx context.Context) *agentState {
	return &agentState{
		terms:   newTerminals(baseCtx),
		claudes: newClaudeSessions(baseCtx),
	}
}

// setSink points every pump at the current connection's outbound channel (or
// nil on disconnect). Pumps load it atomically on every write so a reconnect
// swaps the destination without restarting any process or racing a stale chan.
func (s *agentState) setSink(outbound chan []byte) {
	s.terms.sink.set(outbound)
	s.claudes.sink.set(outbound)
}

// sink is the swappable destination shared by all pumps of one kind. A nil boxed
// channel means "no live connection — buffer only."
type sink struct {
	v atomic.Pointer[chan []byte]
}

// set atomically swaps the destination channel (nil clears it).
func (s *sink) set(ch chan []byte) {
	s.v.Store(&ch)
}

// send pushes a frame to the current connection if one is live. With no live
// connection it drops the frame (the ring/event buffer retains the state for
// replay on the next attach). Non-blocking on the channel so a slow/dead browser
// link can never wedge a pump.
func (s *sink) send(frame []byte) {
	p := s.v.Load()
	if p == nil || *p == nil {
		return
	}
	ch := *p
	select {
	case ch <- frame:
	default:
		// Outbound buffer full (slow link): drop rather than block the pump.
	}
}
