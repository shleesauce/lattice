package hub

import (
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/dylanstoryyy/lattice/internal/proto"
)

// Device is the unified fleet view: every machine the user knows about, merged
// from three sources — the lattice agent registry (full telemetry + can run
// sessions), the Tailscale tailnet (name/os/online/ip for every device incl.
// phones), and the SSH config (aliases + reachable hosts). Devices are deduped
// across sources by name/hostname/DNS tokens (union-find), so one physical
// machine shows once even if it appears in all three.
type Device struct {
	ID       string   `json:"id"`
	Name     string   `json:"name"`
	Host     string   `json:"host"`
	OS       string   `json:"os"`   // darwin | windows | android | ios | linux
	Kind     string   `json:"kind"` // monitor(laptop) | server(desktop) | smartphone
	Status   string   `json:"status"`
	Online   bool     `json:"online"`
	Local    bool     `json:"local"`
	Sources  []string `json:"sources"` // agent | tailscale | ssh

	// Agent-backed only — enables sessions + live telemetry.
	HasAgent     bool               `json:"hasAgent"`
	AgentID      string             `json:"agentId,omitempty"`
	Arch         string             `json:"arch,omitempty"`
	UptimeSec    uint64              `json:"uptimeSec,omitempty"`
	MemTotal     uint64              `json:"memTotal,omitempty"`
	MemUsedPct   float64             `json:"memUsedPct,omitempty"`
	DiskUsedPct  float64             `json:"diskUsedPct,omitempty"`
	LoadAvg1     float64             `json:"loadAvg1,omitempty"`
	CPUCount     int                 `json:"cpuCount,omitempty"`
	LastSeen     string              `json:"lastSeen,omitempty"`
	MACs         []string            `json:"macs,omitempty"`
	Capabilities *proto.Capabilities `json:"capabilities,omitempty"`

	// Reachability extras.
	TailscaleIP string `json:"tailscaleIP,omitempty"`
	SSHAlias    string `json:"sshAlias,omitempty"`
	SSHUser     string `json:"sshUser,omitempty"`
	SSHHost     string `json:"sshHost,omitempty"`
}

var nonAlnum = regexp.MustCompile(`[^a-z0-9]+`)

// idTokens reduces a raw name/hostname/DNS name into the match tokens used to
// dedupe a single machine across sources: its first DNS label and an
// alphanumeric-collapsed form (so "Dylan's Mac Studio" ↔ "Dylans-Mac-Studio").
func idTokens(raw string) []string {
	s := strings.ToLower(strings.TrimSpace(raw))
	if s == "" {
		return nil
	}
	s = strings.TrimSuffix(s, ".")
	if i := strings.Index(s, ".tail"); i > 0 { // strip MagicDNS suffix
		s = s[:i]
	}
	s = strings.TrimSuffix(s, ".local")
	label := s
	if i := strings.Index(label, "."); i > 0 {
		label = label[:i]
	}
	set := map[string]struct{}{}
	if label != "" {
		set[label] = struct{}{}
	}
	if c := nonAlnum.ReplaceAllString(label, ""); c != "" {
		set[c] = struct{}{}
	}
	out := make([]string, 0, len(set))
	for k := range set {
		out = append(out, k)
	}
	return out
}

// ── source fragments ────────────────────────────────────────────────────────

type fragment struct {
	tokens []string
	agent  *Agent
	ts     *tsPeer
	ssh    *sshHost
}

type tsPeer struct {
	host    string
	dnsName string
	os      string
	online  bool
	ip      string
	self    bool
}

type sshHost struct {
	alias    string
	hostName string
	user     string
}

// devices builds the merged device list from all three sources.
func (h *Hub) devices() []Device {
	frags := []fragment{}

	// Snapshot each source exactly once (tailscale/fleet rebuild on every call
	// and map iteration order is random — never index them inside a loop).
	// Sort the map-derived peers by host so fragment order — and thus the merge
	// — is deterministic across calls.
	agents := h.fleet()
	peers := tailscalePeers()
	hosts := sshHosts()
	sort.Slice(peers, func(i, j int) bool {
		if peers[i].dnsName != peers[j].dnsName {
			return peers[i].dnsName < peers[j].dnsName
		}
		return peers[i].host < peers[j].host
	})

	// 1) lattice agents (authoritative telemetry).
	for i := range agents {
		a := agents[i]
		frags = append(frags, fragment{
			tokens: dedup(append(idTokens(a.Name), idTokens(a.Hostname)...)),
			agent:  &a,
		})
	}
	// 2) tailscale peers.
	for i := range peers {
		p := peers[i]
		frags = append(frags, fragment{
			tokens: dedup(append(idTokens(p.dnsName), idTokens(p.host)...)),
			ts:     &p,
		})
	}
	// 3) ssh config hosts. Join on BOTH the alias and the resolved HostName: the
	// alias is the bridge between a short agent hostname ("mbp", "studio", "pc")
	// and the machine's long Tailscale/DNS name ("dylans-macbook-pro", …), so an
	// agent and its tailnet entry fold into one device. (The fleet uses real
	// hostnames/DNS names, not generic single-letter aliases, so this doesn't
	// over-merge.)
	for i := range hosts {
		s := hosts[i]
		frags = append(frags, fragment{
			tokens: dedup(append(idTokens(s.alias), idTokens(s.hostName)...)),
			ssh:    &s,
		})
	}

	groups := unionByToken(frags)

	out := make([]Device, 0, len(groups))
	for _, g := range groups {
		out = append(out, foldGroup(g))
	}
	// Online first, then agent-backed, then name.
	sort.Slice(out, func(i, j int) bool {
		if out[i].Online != out[j].Online {
			return out[i].Online
		}
		if out[i].HasAgent != out[j].HasAgent {
			return out[i].HasAgent
		}
		return strings.ToLower(out[i].Name) < strings.ToLower(out[j].Name)
	})
	return out
}

// unionByToken groups fragments that share any identity token (union-find).
func unionByToken(frags []fragment) [][]fragment {
	parent := make([]int, len(frags))
	for i := range parent {
		parent[i] = i
	}
	var find func(int) int
	find = func(x int) int {
		for parent[x] != x {
			parent[x] = parent[parent[x]]
			x = parent[x]
		}
		return x
	}
	union := func(a, b int) { parent[find(a)] = find(b) }

	tokenOwner := map[string]int{}
	for i, f := range frags {
		for _, t := range f.tokens {
			if j, ok := tokenOwner[t]; ok {
				union(i, j)
			} else {
				tokenOwner[t] = i
			}
		}
	}
	byRoot := map[int][]fragment{}
	for i, f := range frags {
		r := find(i)
		byRoot[r] = append(byRoot[r], f)
	}
	out := make([][]fragment, 0, len(byRoot))
	for _, g := range byRoot {
		out = append(out, g)
	}
	return out
}

// foldGroup collapses one machine's fragments into a single Device.
func foldGroup(g []fragment) Device {
	d := Device{}
	srcs := map[string]struct{}{}

	// Pick the best agent fragment: online wins, then most recent LastSeen.
	var best *Agent
	for _, f := range g {
		if f.agent == nil {
			continue
		}
		srcs["agent"] = struct{}{}
		if best == nil || betterAgent(f.agent, best) {
			best = f.agent
		}
	}
	if best != nil {
		d.HasAgent = true
		d.AgentID = best.ID
		d.Name = best.Name
		d.Host = best.Hostname
		d.OS = best.OS
		d.Arch = best.Arch
		d.Online = best.Online
		d.Local = best.Local
		d.UptimeSec = best.UptimeSec
		d.MemTotal = best.MemTotal
		d.MemUsedPct = best.MemUsedPct
		d.DiskUsedPct = best.DiskUsedPct
		d.LoadAvg1 = best.LoadAvg1
		d.CPUCount = best.CPUCount
		d.LastSeen = best.LastSeen
		d.MACs = best.MACs
		caps := best.Capabilities
		d.Capabilities = &caps
	}

	for _, f := range g {
		if f.ts != nil {
			srcs["tailscale"] = struct{}{}
			if !d.Online && f.ts.online {
				d.Online = true
			}
			if d.OS == "" {
				d.OS = normalizeOS(f.ts.os)
			}
			if d.Name == "" {
				d.Name = f.ts.host
			}
			if d.Host == "" {
				d.Host = f.ts.dnsName
			}
			if d.TailscaleIP == "" {
				d.TailscaleIP = f.ts.ip
			}
			if f.ts.self {
				d.Local = true
			}
		}
		if f.ssh != nil {
			srcs["ssh"] = struct{}{}
			if d.SSHAlias == "" {
				d.SSHAlias = f.ssh.alias
			}
			d.SSHUser = firstNonEmpty(d.SSHUser, f.ssh.user)
			d.SSHHost = firstNonEmpty(d.SSHHost, f.ssh.hostName)
			if d.Name == "" {
				d.Name = f.ssh.alias
			}
		}
	}

	d.OS = normalizeOS(d.OS)
	d.Kind = kindFor(d.Name, d.Host, d.OS)
	d.Sources = sortedKeys(srcs)
	d.ID = deviceID(d)
	d.Status = deviceStatus(d)
	return d
}

func betterAgent(a, b *Agent) bool {
	if a.Online != b.Online {
		return a.Online
	}
	return a.LastSeen > b.LastSeen // RFC3339 sorts lexicographically
}

// deviceStatus: live/idle/detached are layered on by the dashboard from
// sessions for agent machines; here we report the base reachability so
// non-agent devices still get a color.
//   online + agent  → "online"   (dashboard upgrades to live/idle/detached)
//   online, no agent → "reachable"
//   offline          → "exited"
func deviceStatus(d Device) string {
	if !d.Online {
		return "exited"
	}
	if d.HasAgent {
		return "online"
	}
	return "reachable"
}

func deviceID(d Device) string {
	if d.AgentID != "" {
		return d.AgentID
	}
	if d.Host != "" {
		return "host:" + strings.ToLower(d.Host)
	}
	return "dev:" + strings.ToLower(d.Name)
}

func normalizeOS(os string) string {
	s := strings.ToLower(strings.TrimSpace(os))
	switch {
	case s == "macos" || s == "darwin":
		return "darwin"
	case strings.HasPrefix(s, "win"):
		return "windows"
	case s == "android":
		return "android"
	case s == "ios" || s == "ipados":
		return "ios"
	case s == "linux":
		return "linux"
	default:
		return s
	}
}

// kindFor maps a device to an icon family: phones, laptops (monitor), desktops
// (server). Uses OS + name heuristics.
func kindFor(name, host, os string) string {
	if os == "android" || os == "ios" {
		return "smartphone"
	}
	n := strings.ToLower(name + " " + host)
	switch {
	case strings.Contains(n, "iphone") || strings.Contains(n, "ipad") ||
		strings.Contains(n, "phone") || strings.Contains(n, "galaxy") ||
		strings.Contains(n, "pixel") || strings.Contains(n, "s26"):
		return "smartphone"
	case strings.Contains(n, "macbook") || strings.Contains(n, "mbp") ||
		strings.Contains(n, "air") || strings.Contains(n, "laptop") ||
		strings.Contains(n, "book"):
		return "monitor"
	default:
		return "server"
	}
}

// ── tailscale source ────────────────────────────────────────────────────────

func tailscaleBin() string {
	for _, c := range []string{
		"tailscale",
		"/usr/local/bin/tailscale",
		"/opt/homebrew/bin/tailscale",
		"/Applications/Tailscale.app/Contents/MacOS/Tailscale",
	} {
		if p, err := exec.LookPath(c); err == nil {
			return p
		}
		if _, err := os.Stat(c); err == nil {
			return c
		}
	}
	return ""
}

// tailscalePeers runs `tailscale status --json` and returns self + every peer.
// Best-effort: returns nil if tailscale isn't installed or errors.
func tailscalePeers() []tsPeer {
	bin := tailscaleBin()
	if bin == "" {
		return nil
	}
	ctx, cancel := context.WithTimeout(context.Background(), 4*time.Second)
	defer cancel()
	out, err := exec.CommandContext(ctx, bin, "status", "--json").Output()
	if err != nil {
		return nil
	}
	// Decode Self + Peer generically.
	var raw struct {
		Self struct {
			HostName     string   `json:"HostName"`
			DNSName      string   `json:"DNSName"`
			OS           string   `json:"OS"`
			Online       bool     `json:"Online"`
			TailscaleIPs []string `json:"TailscaleIPs"`
		} `json:"Self"`
		Peer map[string]struct {
			HostName     string   `json:"HostName"`
			DNSName      string   `json:"DNSName"`
			OS           string   `json:"OS"`
			Online       bool     `json:"Online"`
			TailscaleIPs []string `json:"TailscaleIPs"`
		} `json:"Peer"`
	}
	if err := json.Unmarshal(out, &raw); err != nil {
		return nil
	}
	first := func(s []string) string {
		if len(s) > 0 {
			return s[0]
		}
		return ""
	}
	peers := make([]tsPeer, 0, len(raw.Peer)+1)
	if raw.Self.HostName != "" {
		peers = append(peers, tsPeer{
			host: raw.Self.HostName, dnsName: raw.Self.DNSName, os: raw.Self.OS,
			online: true, ip: first(raw.Self.TailscaleIPs), self: true,
		})
	}
	for _, p := range raw.Peer {
		peers = append(peers, tsPeer{
			host: p.HostName, dnsName: p.DNSName, os: p.OS,
			online: p.Online, ip: first(p.TailscaleIPs),
		})
	}
	return peers
}

// ── ssh config source ───────────────────────────────────────────────────────

// sshHosts parses ~/.ssh/config into concrete host aliases (skipping wildcard
// "Host *" blocks). Best-effort: returns nil if no config.
func sshHosts() []sshHost {
	home, err := os.UserHomeDir()
	if err != nil {
		return nil
	}
	data, err := os.ReadFile(filepath.Join(home, ".ssh", "config"))
	if err != nil {
		return nil
	}
	var hosts []sshHost
	var cur *sshHost
	flush := func() {
		if cur != nil && cur.alias != "" && cur.alias != "*" {
			hosts = append(hosts, *cur)
		}
		cur = nil
	}
	for _, line := range strings.Split(string(data), "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		key, val, _ := strings.Cut(line, " ")
		key = strings.ToLower(strings.TrimSpace(key))
		val = strings.TrimSpace(val)
		switch key {
		case "host":
			flush()
			// Use the first alias of a multi-alias Host line; skip pure wildcards.
			alias := strings.Fields(val)
			if len(alias) == 0 {
				continue
			}
			name := alias[0]
			if name == "*" && len(alias) > 1 {
				name = alias[1]
			}
			cur = &sshHost{alias: name}
		case "hostname":
			if cur != nil {
				cur.hostName = val
			}
		case "user":
			if cur != nil {
				cur.user = val
			}
		}
	}
	flush()
	return hosts
}

// ── small helpers ───────────────────────────────────────────────────────────

func dedup(in []string) []string {
	seen := map[string]struct{}{}
	out := in[:0]
	for _, s := range in {
		if s == "" {
			continue
		}
		if _, ok := seen[s]; ok {
			continue
		}
		seen[s] = struct{}{}
		out = append(out, s)
	}
	return out
}

func sortedKeys(m map[string]struct{}) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

func firstNonEmpty(a, b string) string {
	if a != "" {
		return a
	}
	return b
}
