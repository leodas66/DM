//go:build unix

package uihandoff

import "net"

func netListenUnixForTest(path string) (*net.UnixListener, error) {
	listener, err := net.ListenUnix("unix", &net.UnixAddr{Name: path, Net: "unix"})
	if err == nil {
		listener.SetUnlinkOnClose(false)
	}
	return listener, err
}
