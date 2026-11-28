//go:build unix

package guest

import "syscall"

// openNow opens a name without waiting, as a pipe with nobody at its other
// end would have it wait.
const openNow = syscall.O_NONBLOCK
