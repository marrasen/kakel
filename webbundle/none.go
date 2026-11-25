//go:build !webclient || js

package webbundle

// Bundle is the bundle, with the signature of its manifest as a .sig
// file has it, which is empty for one that is not signed. It reports
// whether this build has one at all, which it does not.
func Bundle() (zipped, sig []byte, ok bool) { return nil, nil, false }

// Built reports whether this build has a bundle, as Bundle does, without
// reading it: it has none.
func Built() bool { return false }
