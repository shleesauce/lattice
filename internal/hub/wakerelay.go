package hub

import (
	"net"
	"strings"
)

// wakeTarget is everything the relay selector needs to know about the machine we
// want to wake: its WoL MACs and its last-known LAN subnets. Derived from a
// persisted/offline Agent (or, when the caller only has a Device, from that).
type wakeTarget struct {
	agentID string
	macs    []string
	subnets []*net.IPNet // parsed from the target's last-known LANIPs
}

// relayChoice is the outcome of selecting a relay agent for a wake.
type relayChoice struct {
	RelayID  string   // chosen live agent to emit the magic packet ("" if none)
	MAC      string   // the target MAC to wake
	Subnet   string   // the matched subnet (for logging / UI), "" if matched by fallback
	OnSubnet bool     // true when RelayID actually shares a subnet with the target
	Reason   string   // why no relay (when RelayID == "")
	Tried    []string // relay agent ids considered (for diagnostics)
}

// parseCIDRs turns a slice of "ip/mask" CIDR strings into *net.IPNet networks,
// silently dropping any that don't parse. The returned networks are masked to the
// network address so net.IPNet.Contains works as a same-subnet test.
func parseCIDRs(cidrs []string) []*net.IPNet {
	out := make([]*net.IPNet, 0, len(cidrs))
	for _, c := range cidrs {
		_, n, err := net.ParseCIDR(strings.TrimSpace(c))
		if err != nil || n == nil {
			continue
		}
		out = append(out, n)
	}
	return out
}

// cidrIP extracts the host IP from a "ip/mask" CIDR string.
func cidrIP(cidr string) net.IP {
	ip, _, err := net.ParseCIDR(strings.TrimSpace(cidr))
	if err != nil {
		return nil
	}
	return ip
}

// agentSharesSubnet reports whether a live agent's own LAN CIDRs place it on any
// of the target's subnets — i.e. a WoL broadcast from this agent would reach the
// target's broadcast domain. The test is symmetric: the agent's host IP must fall
// inside one of the target's networks (or vice-versa) for either /mask.
func agentSharesSubnet(agent Agent, targetSubnets []*net.IPNet) (string, bool) {
	for _, ac := range agent.LANIPs {
		aip := cidrIP(ac)
		if aip == nil {
			continue
		}
		for _, tn := range targetSubnets {
			if tn.Contains(aip) {
				return tn.String(), true
			}
		}
	}
	// Reverse direction: the agent's network containing the target's host IP.
	agentNets := parseCIDRs(agent.LANIPs)
	for _, an := range agentNets {
		for _, tn := range targetSubnets {
			if an.Contains(tn.IP) {
				return an.String(), true
			}
		}
	}
	return "", false
}

// selectWakeRelay picks the best LIVE agent to emit a WoL magic packet for the
// target, given the full fleet. Selection order:
//
//  1. A live agent that shares a subnet with the target's last-known LAN — the
//     only relay whose broadcast actually reaches the sleeper. The local (hub-host)
//     agent is preferred among equals (snappier, always-on).
//  2. If the target has no known subnet (older agent that never reported LANIPs),
//     fall back to ANY live agent (legacy behavior) — best-effort, OnSubnet=false.
//  3. If the target HAS a known subnet but no live agent shares it, return no relay
//     with Reason "no relay reachable on that subnet" so the UI stops failing
//     silently.
//
// The target itself is never chosen as its own relay (it's the thing we're waking).
func selectWakeRelay(t wakeTarget, fleet []Agent) relayChoice {
	choice := relayChoice{}
	if len(t.macs) > 0 {
		choice.MAC = t.macs[0]
	}

	var live []Agent
	for _, a := range fleet {
		if a.Online && a.ID != t.agentID {
			live = append(live, a)
		}
	}
	if len(live) == 0 {
		choice.Reason = "no live agent available to send the wake packet"
		return choice
	}

	// 1) same-subnet relay (prefer the local hub-host agent among matches).
	var matched *Agent
	var matchedSubnet string
	for i := range live {
		choice.Tried = append(choice.Tried, live[i].ID)
		sub, ok := agentSharesSubnet(live[i], t.subnets)
		if !ok {
			continue
		}
		if matched == nil || (live[i].Local && !matched.Local) {
			matched = &live[i]
			matchedSubnet = sub
		}
	}
	if matched != nil {
		choice.RelayID = matched.ID
		choice.Subnet = matchedSubnet
		choice.OnSubnet = true
		return choice
	}

	// 2) target subnet unknown ⇒ legacy any-live fallback (can't do better).
	if len(t.subnets) == 0 {
		// Prefer the local agent if present, else first live.
		pick := &live[0]
		for i := range live {
			if live[i].Local {
				pick = &live[i]
				break
			}
		}
		choice.RelayID = pick.ID
		return choice
	}

	// 3) target subnet known but no live peer shares it.
	choice.Reason = "no relay reachable on that subnet"
	return choice
}

// wakeTargetForAgent builds a wakeTarget from a fleet agent record (the offline
// machine we want to wake).
func wakeTargetForAgent(a Agent) wakeTarget {
	return wakeTarget{
		agentID: a.ID,
		macs:    a.MACs,
		subnets: parseCIDRs(a.LANIPs),
	}
}
