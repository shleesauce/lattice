package agent

import (
	"context"
	"encoding/hex"
	"fmt"
	"log"
	"net"
	"strings"
	"syscall"

	"github.com/dylanstoryyy/lattice/internal/proto"
)

// wolPorts are the conventional Wake-on-LAN UDP ports. 9 (discard) is the de
// facto standard; 7 (echo) is a common alternate, so we broadcast to both.
var wolPorts = []int{9, 7}

// wake builds a WoL magic packet for the target MAC and broadcasts it on the
// agent's LAN, then pushes a wake_result correlated by ReqID.
func wake(ctx context.Context, p proto.WakePayload, outbound chan<- []byte) {
	result := proto.WakeResultPayload{ReqID: p.ReqID}

	mac, err := parseMAC(p.MAC)
	if err != nil {
		result.Error = err.Error()
		sendWakeResult(ctx, outbound, result)
		return
	}

	packet := magicPacket(mac)
	if err := broadcastMagic(ctx, packet); err != nil {
		result.Error = err.Error()
		sendWakeResult(ctx, outbound, result)
		return
	}

	result.OK = true
	sendWakeResult(ctx, outbound, result)
}

// parseMAC accepts aa:bb:cc:dd:ee:ff, aa-bb-..., or bare hex and returns the 6
// hardware-address bytes.
func parseMAC(s string) ([]byte, error) {
	clean := strings.NewReplacer(":", "", "-", "", ".", "", " ", "").Replace(strings.TrimSpace(s))
	if len(clean) != 12 {
		return nil, fmt.Errorf("invalid MAC %q", s)
	}
	b, err := hex.DecodeString(clean)
	if err != nil {
		return nil, fmt.Errorf("invalid MAC %q: %w", s, err)
	}
	return b, nil
}

// magicPacket builds the standard WoL payload: 6×0xFF then 16× the MAC.
func magicPacket(mac []byte) []byte {
	packet := make([]byte, 0, 6+16*6)
	for i := 0; i < 6; i++ {
		packet = append(packet, 0xFF)
	}
	for i := 0; i < 16; i++ {
		packet = append(packet, mac...)
	}
	return packet
}

// broadcastMagic sends the packet to the limited broadcast (255.255.255.255)
// AND every interface's directed broadcast, on each WoL port. An UNCONNECTED
// socket + WriteTo is used because connecting a UDP socket to 255.255.255.255
// fails on macOS ("broken pipe"). SO_BROADCAST is set via the Control hook or
// the send is refused with EACCES on Linux/macOS — the platforms the sender
// actually runs on.
func broadcastMagic(ctx context.Context, packet []byte) error {
	lc := net.ListenConfig{
		Control: func(network, address string, c syscall.RawConn) error {
			var serr error
			if err := c.Control(func(fd uintptr) { serr = setSocketBroadcast(fd) }); err != nil {
				return err
			}
			return serr
		},
	}
	pc, err := lc.ListenPacket(ctx, "udp4", "0.0.0.0:0")
	if err != nil {
		return fmt.Errorf("open broadcast socket: %w", err)
	}
	defer pc.Close()

	// Targets: the limited broadcast plus each interface's directed broadcast,
	// so the packet reaches the target's segment regardless of the sender's
	// default route.
	targets := append([]net.IP{net.IPv4bcast}, directedBroadcasts()...)

	var lastErr error
	sent := false
	for _, port := range wolPorts {
		for _, ip := range targets {
			if _, err := pc.WriteTo(packet, &net.UDPAddr{IP: ip, Port: port}); err != nil {
				lastErr = err
				continue
			}
			sent = true
		}
	}
	if !sent {
		if lastErr != nil {
			return lastErr
		}
		return fmt.Errorf("failed to send WoL packet")
	}
	return nil
}

// directedBroadcasts returns the directed-broadcast address of every up, non-
// loopback IPv4 interface (e.g. 192.168.0.255 for 192.168.0.46/24).
func directedBroadcasts() []net.IP {
	var out []net.IP
	ifaces, err := net.Interfaces()
	if err != nil {
		return out
	}
	for _, ifi := range ifaces {
		if ifi.Flags&net.FlagUp == 0 || ifi.Flags&net.FlagLoopback != 0 {
			continue
		}
		addrs, err := ifi.Addrs()
		if err != nil {
			continue
		}
		for _, a := range addrs {
			ipnet, ok := a.(*net.IPNet)
			if !ok || ipnet.IP.To4() == nil {
				continue
			}
			ip := ipnet.IP.To4()
			mask := ipnet.Mask
			bc := make(net.IP, 4)
			for i := 0; i < 4; i++ {
				bc[i] = ip[i] | ^mask[i]
			}
			out = append(out, bc)
		}
	}
	return out
}

// sendWakeResult encodes and pushes a wake_result frame.
func sendWakeResult(ctx context.Context, outbound chan<- []byte, result proto.WakeResultPayload) {
	frame, err := proto.Encode(proto.TypeWakeResult, result)
	if err != nil {
		log.Printf("agent: encode wake_result: %v", err)
		return
	}
	select {
	case outbound <- frame:
	case <-ctx.Done():
	}
}
