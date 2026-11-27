package ptyshell

import "golang.org/x/sys/unix"

// PasswordPrompt reports whether the program in the terminal is reading a password:
// echo is off while line mode (ICANON) is on. Full-screen programs such as vim turn
// both off, so they are not mistaken for a password prompt.
func (p *process) PasswordPrompt() bool {
	raw, err := p.f.SyscallConn() // not Fd(): that would switch the PTY to blocking mode
	if err != nil {
		return false
	}
	var lflag uint64
	ok := false
	_ = raw.Control(func(fd uintptr) {
		t, err := unix.IoctlGetTermios(int(fd), ioctlGetTermios)
		if err == nil {
			lflag, ok = uint64(t.Lflag), true
		}
	})
	return ok && lflag&unix.ECHO == 0 && lflag&unix.ICANON != 0
}
