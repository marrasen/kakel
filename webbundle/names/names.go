// Package names is where the web client's bundle is kept in kakel's
// source, for what builds it and what embeds it. It embeds nothing
// itself, so the bundle can be built where there is none yet.
package names

// Dir is the folder the bundle is built into and embedded from, in
// kakel's source, with slashes. It is not checked in.
const Dir = "webbundle/client"

// The files in Dir. Sig is Manifest's signature, as gunimsign writes it,
// and missing for a bundle that is not signed.
const (
	Zip      = "client.zip"
	Manifest = "client.manifest"
	Sig      = Manifest + ".sig"
)
