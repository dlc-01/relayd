package portcheck

import (
	"bufio"
	"fmt"
	"net"
	"os"
	"runtime"
	"strings"
)

func IsOccupied(port int) (bool, error) {
	if runtime.GOOS == "linux" {
		return isOccupiedLinux(port)
	}
	return isOccupiedNet(port)
}

func isOccupiedLinux(port int) (bool, error) {
	for _, path := range []string{"/proc/net/tcp", "/proc/net/tcp6"} {
		occupied, err := checkProcNet(path, port)
		if err != nil {
			continue
		}
		if occupied {
			return true, nil
		}
	}
	return false, nil
}

func isOccupiedNet(port int) (bool, error) {
	ln, err := net.Listen("tcp", fmt.Sprintf("0.0.0.0:%d", port))
	if err != nil {
		return true, nil
	}
	ln.Close()
	return false, nil
}

func checkProcNet(path string, port int) (bool, error) {
	f, err := os.Open(path)
	if err != nil {
		return false, err
	}
	defer f.Close()

	hexPort := fmt.Sprintf("%04X", port)
	scanner := bufio.NewScanner(f)
	scanner.Scan()

	for scanner.Scan() {
		fields := strings.Fields(scanner.Text())
		if len(fields) < 4 {
			continue
		}
		localAddr := fields[1]
		state := fields[3]
		parts := strings.Split(localAddr, ":")
		if len(parts) != 2 {
			continue
		}
		if parts[1] == hexPort && state == "0A" {
			return true, nil
		}
	}
	return false, scanner.Err()
}

func IsForbidden(port int) bool {
	forbidden := map[int]bool{
		22: true, 25: true, 53: true,
		80: true, 443: true,
	}
	return port < 1024 || forbidden[port]
}

func IsInRange(port, min, max int) bool {
	return port >= min && port <= max
}

func Check(port, minPort, maxPort int) error {
	if IsForbidden(port) {
		return fmt.Errorf("port %d is forbidden", port)
	}
	if !IsInRange(port, minPort, maxPort) {
		return fmt.Errorf("port %d out of allowed range [%d, %d]", port, minPort, maxPort)
	}
	occupied, err := IsOccupied(port)
	if err != nil {
		return fmt.Errorf("port check failed: %w", err)
	}
	if occupied {
		return fmt.Errorf("port %d is occupied", port)
	}
	return nil
}
