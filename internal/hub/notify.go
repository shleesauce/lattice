package hub

import (
	"bytes"
	"encoding/json"
	"log"
	"net/http"
	"os"
	"strings"
	"time"
)

// ntfyAction is one action button on a push (ntfy "actions" array). For an "http"
// action the ntfy mobile app fires the request CLIENT-SIDE — from the phone — so
// an approve link pointed at a tailnet-only hub URL resolves fine as long as the
// phone is on the tailnet (it is). A "view" action just opens a URL.
type ntfyAction struct {
	Action  string            `json:"action"` // "view" | "http"
	Label   string            `json:"label"`
	URL     string            `json:"url"`
	Method  string            `json:"method,omitempty"`  // http only
	Headers map[string]string `json:"headers,omitempty"` // http only
	Clear   bool              `json:"clear,omitempty"`   // dismiss the notification after tap
}

// ntfyMessage is the JSON publish shape (https://docs.ntfy.sh/publish/#publish-as-json).
type ntfyMessage struct {
	Topic    string       `json:"topic"`
	Title    string       `json:"title,omitempty"`
	Message  string       `json:"message"`
	Priority int          `json:"priority,omitempty"`
	Tags     []string     `json:"tags,omitempty"`
	Click    string       `json:"click,omitempty"`
	Actions  []ntfyAction `json:"actions,omitempty"`
}

// notify publishes one message to ntfy, best-effort and asynchronous. Topic comes
// from LATTICE_NTFY_TOPIC (empty ⇒ notifications disabled, so a stock hub stays
// silent); the server from LATTICE_NTFY_URL (default https://ntfy.sh). This is the
// exact env contract the fleet watchdog already uses, so the phone that's already
// subscribed to the fleet topic needs no new setup.
func (h *Hub) notify(msg ntfyMessage) {
	topic := strings.TrimSpace(os.Getenv("LATTICE_NTFY_TOPIC"))
	if topic == "" {
		return
	}
	base := strings.TrimRight(strings.TrimSpace(os.Getenv("LATTICE_NTFY_URL")), "/")
	if base == "" {
		base = "https://ntfy.sh"
	}
	msg.Topic = topic
	body, err := json.Marshal(msg)
	if err != nil {
		log.Printf("notify: marshal: %v", err)
		return
	}
	go func() {
		req, err := http.NewRequest(http.MethodPost, base, bytes.NewReader(body))
		if err != nil {
			log.Printf("notify: new request: %v", err)
			return
		}
		req.Header.Set("Content-Type", "application/json")
		resp, err := (&http.Client{Timeout: 10 * time.Second}).Do(req)
		if err != nil {
			log.Printf("notify: post: %v", err)
			return
		}
		resp.Body.Close()
	}()
}

// notifyEnabled reports whether a topic is configured — used to skip minting an
// approval nonce when nothing would be delivered anyway.
func notifyEnabled() bool {
	return strings.TrimSpace(os.Getenv("LATTICE_NTFY_TOPIC")) != ""
}

// prettyAgentName renders an agent id for a human-facing push: it strips the
// os-family suffix the id carries (e.g. "studio-darwin" → "studio").
func prettyAgentName(agentID string) string {
	for _, suf := range []string{"-darwin", "-windows", "-linux"} {
		if strings.HasSuffix(agentID, suf) {
			return strings.TrimSuffix(agentID, suf)
		}
	}
	return agentID
}
