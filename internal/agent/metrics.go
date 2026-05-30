package agent

import (
	"context"
	"log"
	"net"
	"os"
	"runtime"
	"strings"

	"github.com/shirou/gopsutil/v4/disk"
	"github.com/shirou/gopsutil/v4/host"
	"github.com/shirou/gopsutil/v4/load"
	"github.com/shirou/gopsutil/v4/mem"

	"github.com/dylanstoryyy/lattice/internal/proto"
)

// gatherMetrics samples the host metrics for one heartbeat. Every probe is
// tolerated independently: a failing probe logs and leaves its fields zero.
func gatherMetrics(ctx context.Context) proto.HeartbeatPayload {
	hb := proto.HeartbeatPayload{CPUCount: runtime.NumCPU()}

	if up, err := host.UptimeWithContext(ctx); err != nil {
		log.Printf("agent: uptime metric: %v", err)
	} else {
		hb.UptimeSec = up
	}

	if usage, err := disk.UsageWithContext(ctx, rootPath()); err != nil {
		log.Printf("agent: disk metric: %v", err)
	} else if usage != nil {
		hb.DiskTotal = usage.Total
		hb.DiskFree = usage.Free
		hb.DiskUsedPct = usage.UsedPercent
	}

	if vm, err := mem.VirtualMemoryWithContext(ctx); err != nil {
		log.Printf("agent: mem metric: %v", err)
	} else if vm != nil {
		hb.MemTotal = vm.Total
		hb.MemUsedPct = vm.UsedPercent
	}

	// load.Avg returns an error on platforms without loadavg (e.g. Windows);
	// per the contract we ignore the error and leave LoadAvg1 at 0.
	if avg, err := load.AvgWithContext(ctx); err == nil && avg != nil {
		hb.LoadAvg1 = avg.Load1
	}

	hb.MACs = hardwareMACs()

	// Refresh capabilities so placement always scores fresh can-run state (D19)
	// without waiting for a reconnect.
	hb.Capabilities = detectCapabilities(ctx)

	return hb
}

// hardwareMACs returns the deduped physical-interface MAC addresses, skipping
// loopback, down, and zero-address interfaces. The hub keeps the last-known set
// so an offline machine can still be woken by a LAN peer.
func hardwareMACs() []string {
	ifaces, err := net.Interfaces()
	if err != nil {
		return nil
	}
	seen := make(map[string]struct{})
	var out []string
	for _, ifc := range ifaces {
		if ifc.Flags&net.FlagLoopback != 0 || ifc.Flags&net.FlagUp == 0 {
			continue
		}
		mac := ifc.HardwareAddr.String()
		if mac == "" || mac == "00:00:00:00:00:00" {
			continue
		}
		mac = strings.ToLower(mac)
		if _, dup := seen[mac]; dup {
			continue
		}
		seen[mac] = struct{}{}
		out = append(out, mac)
	}
	return out
}

// rootPath is the volume to report disk usage for: "/" on unix, the system
// drive on Windows.
func rootPath() string {
	if runtime.GOOS == "windows" {
		if sys := os.Getenv("SystemDrive"); sys != "" {
			return sys + "\\"
		}
		return "C:\\"
	}
	return "/"
}
