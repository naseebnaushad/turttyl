//go:build darwin || linux

package control

import "syscall"

// umask sets the process umask and returns the old one, so the socket file is never
// briefly created with loose permissions.
func umask(m int) int { return syscall.Umask(m) }
