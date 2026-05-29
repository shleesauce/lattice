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
	if err := broadcastMagic(packet); err != nil {
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

// broadcastMagic UDP-broadcasts the packet to 255.255.255.255 on the WoL ports.
// SO_BROADCAST must be set on the socket or the send fails with EACCES/
// EADDRNOTAVAIL on Linux/macOS — exactly the platforms the sender runs on.
func broadcastMagic(packet []byte) error {
	dialer := net.Dialer{
		Control: func(network, address string, c syscall.RawConn) error {
			var serr error
			if err := c.Control(func(fd uintptr) { serr = setSocketBroadcast(fd) }); err != nil {
				return err
			}
			return serr
		},
	}

	var lastErr error
	sent := false
	for _, port := range wolPorts {
		conn, err := dialer.Dial("udp4", fmt.Sprintf("255.255.255.255:%d", port))
		if err != nil {
			lastErr = err
			continue
		}
		_, err = conn.Write(packet)
		conn.Close()
		if err != nil {
			lastErr = err
			continue
		}
		sent = true
	}
	if !sent {
		if lastErr != nil {
			return lastErr
		}
		return fmt.Errorf("failed to send WoL packet")
	}
	return nil
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
