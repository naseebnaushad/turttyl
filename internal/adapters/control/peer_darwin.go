package control

import (
	"fmt"
	"net"
	"os"

	"golang.org/x/sys/unix"
)

// checkPeer rejects connections from processes owned by another user.
func checkPeer(c *net.UnixConn) error {
	raw, err := c.SyscallConn()
	if err != nil {
		return err
	}
	var uid uint32
	var cerr error
	if err := raw.Control(func(fd uintptr) {
		var x *unix.Xucred
		x, cerr = unix.GetsockoptXucred(int(fd), unix.SOL_LOCAL, unix.LOCAL_PEERCRED)
		if cerr == nil {
			uid = x.Uid
		}
	}); err != nil {
		return err
	}
	if cerr != nil {
		return cerr
	}
	if int(uid) != os.Getuid() {
		return fmt.Errorf("peer uid %d is not this user", uid)
	}
	return nil
}
