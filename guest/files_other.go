//go:build !unix

package guest

// openNow opens a name without waiting: nothing to say on a system
// whose opens do not wait.
const openNow = 0
