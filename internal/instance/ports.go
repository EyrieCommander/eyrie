package instance

import (
	"fmt"
	"net"
	"strconv"
)

const (
	PortRangeStart = 43000
	PortRangeEnd   = 43999
)

// AllocatePort finds the first available port in the instance range
// (43000-43999) that is not used by any existing instance.
// Known framework ports (42617, 18789, 7200) are outside this range
// and don't need explicit reservation.
func AllocatePort(existing []Instance) (int, error) {
	used := make(map[int]bool, len(existing))
	for _, inst := range existing {
		used[inst.Port] = true
	}

	for port := PortRangeStart; port <= PortRangeEnd; port++ {
		if used[port] {
			continue
		}
		// Also check if something else is actually listening on it.
		if portAvailable(port) {
			return port, nil
		}
	}
	return 0, fmt.Errorf("no available ports in range %d-%d", PortRangeStart, PortRangeEnd)
}

func portAvailable(port int) bool {
	ln, err := net.Listen("tcp", "127.0.0.1:"+strconv.Itoa(port))
	if err != nil {
		return false
	}
	ln.Close()
	return true
}
