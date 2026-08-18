//go:build !windows

package cmd

import (
	"os"
	"syscall"
)

// reraise ends the process by the signal it is given, so a parent sees
// WIFSIGNALED rather than a normal exit imitating one — which is what a
// shell uses to decide that Ctrl-C should also break the loop slio was
// running in. It does not return.
//
// The blocking wait is required rather than decorative. raise(3) is
// synchronous for the calling thread, but Go's syscall.Kill is not: the
// runtime may deliver the signal on another thread while this one carries
// on, and the fallback below sits immediately downstream.
func reraise(sig syscall.Signal) {
	if err := syscall.Kill(syscall.Getpid(), sig); err != nil {
		os.Exit(128 + int(sig))
	}
	select {} // the signal is already unblocked and default-dispositioned
}
