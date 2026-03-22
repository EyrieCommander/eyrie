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

// reservedPorts are used by known framework defaults and Eyrie itself.
var reservedPorts = map[int]bool{
	42617: true, // ZeroClaw default
	18789: true, // OpenClaw default
	7200:  true, // Eyrie dashboard
}

// AllocatePort finds the first available port in the instance range
// that is not used by any existing instance or reserved.
func AllocatePort(existing []Instance) (int, error) {
	used := make(map[int]bool, len(existing)+len(reservedPorts))
	for k, v := range reservedPorts {
		used[k] = v
	}
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
