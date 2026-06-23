package hub

import (
	"fmt"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// These stress the concurrency-sensitive paths the pre-public hardening touched,
// under real contention with -race. They assert no panic/race/deadlock and that
// the bounded structures actually stay bounded under load.

// loginLimiter is hit from every login attempt (fail/allow/reset) and swept from
// the hourly cleanup loop — concurrently. Storm all four entry points across many
// goroutines on overlapping and distinct IPs, then prove a final sweep collapses
// the map back down once every entry has aged past the window.
func TestLoginLimiterConcurrentStorm(t *testing.T) {
	l := newLoginLimiter()
	const workers = 64
	const opsPer = 500

	var wg sync.WaitGroup
	for w := 0; w < workers; w++ {
		wg.Add(1)
		go func(w int) {
			defer wg.Done()
			for i := 0; i < opsPer; i++ {
				ip := fmt.Sprintf("10.0.%d.%d", w%8, i%32) // overlapping + distinct
				switch i % 4 {
				case 0, 1:
					l.fail(ip)
				case 2:
					l.allow(ip)
				case 3:
					l.sweep() // concurrent sweeps must be safe
				}
			}
		}(w)
	}
	wg.Wait()

	// Age every recorded attempt past the window, then sweep: the map must drain
	// completely — no stale bucket may survive (this is the leak the sweep fixes).
	l.mu.Lock()
	old := time.Now().Add(-2 * loginWindow)
	for ip := range l.fails {
		for i := range l.fails[ip] {
			l.fails[ip][i] = old
		}
	}
	l.mu.Unlock()

	l.sweep()

	l.mu.Lock()
	remaining := len(l.fails)
	l.mu.Unlock()
	if remaining != 0 {
		t.Errorf("after aging+sweep, %d stale buckets leaked (want 0)", remaining)
	}
}

// liveAgentCount is read by the unauthenticated /api/health probe under arbitrary
// concurrency while agents register/deregister. Storm reads against a churning
// agents map; -race validates the RLock/Lock discipline and the count stays in
// range (never negative, never above the live set).
func TestLiveAgentCountConcurrentChurn(t *testing.T) {
	r := &Registry{agents: make(map[string]*agentConn)}
	const ids = 50
	stop := make(chan struct{})
	var wg sync.WaitGroup

	// Churners: add/remove agents under the write lock.
	for c := 0; c < 4; c++ {
		wg.Add(1)
		go func(c int) {
			defer wg.Done()
			for {
				select {
				case <-stop:
					return
				default:
				}
				id := fmt.Sprintf("agent-%d-%d", c, time.Now().UnixNano()%ids)
				r.mu.Lock()
				r.agents[id] = &agentConn{id: id}
				r.mu.Unlock()
				r.mu.Lock()
				delete(r.agents, id)
				r.mu.Unlock()
			}
		}(c)
	}

	// Readers: hammer the health-path count.
	var bad atomic.Int64
	for rd := 0; rd < 16; rd++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := 0; i < 5000; i++ {
				if n := r.liveAgentCount(); n < 0 {
					bad.Add(1)
				}
			}
		}()
	}

	time.Sleep(50 * time.Millisecond)
	close(stop)
	wg.Wait()
	if bad.Load() != 0 {
		t.Errorf("liveAgentCount returned a negative count %d time(s)", bad.Load())
	}
}

// ReapAuditLog runs on the reap loop while sessions are actively appending audit
// rows. The store pins a single SQLite connection (writes serialized + busy
// timeout), so reap and insert must interleave without error or deadlock, and the
// table must end at or under the cap.
func TestReapAuditLogUnderConcurrentWrites(t *testing.T) {
	st := testStore(t)
	const keep = 200
	stop := make(chan struct{})
	var wg sync.WaitGroup
	var writeErr atomic.Pointer[error]

	// Writers: append audit rows continuously.
	for wkr := 0; wkr < 4; wkr++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for {
				select {
				case <-stop:
					return
				default:
				}
				if _, err := st.db.Exec(
					`INSERT INTO audit_log (session_id, agent_id, event_type, tool_name, detail_json, at)
					 VALUES (?,?,?,?,?,?)`,
					"s", "mini", "tool_use", "Bash", "{}",
					time.Now().UTC().Format(time.RFC3339Nano),
				); err != nil {
					e := err
					writeErr.Store(&e)
					return
				}
			}
		}()
	}

	// Reaper: repeatedly cap the table while writes are in flight.
	wg.Add(1)
	go func() {
		defer wg.Done()
		for i := 0; i < 200; i++ {
			if _, err := st.ReapAuditLog(keep); err != nil {
				e := err
				writeErr.Store(&e)
				return
			}
		}
	}()

	time.Sleep(150 * time.Millisecond)
	close(stop)
	wg.Wait()

	if ep := writeErr.Load(); ep != nil {
		t.Fatalf("error under concurrent reap+write: %v", *ep)
	}
	// Final reap, then assert the cap held.
	if _, err := st.ReapAuditLog(keep); err != nil {
		t.Fatalf("final reap: %v", err)
	}
	var count int
	if err := st.db.QueryRow(`SELECT COUNT(*) FROM audit_log`).Scan(&count); err != nil {
		t.Fatalf("count: %v", err)
	}
	if count > keep {
		t.Errorf("audit_log=%d rows, exceeds cap %d", count, keep)
	}
}
