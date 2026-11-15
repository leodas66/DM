//go:build unix

package uihandoff

import "net"

func netListenUnixForTest(path string) (*net.UnixListener, error) {
	return net.ListenUnix("unix", &net.UnixAddr{Name: path, Net: "unix"})
}
