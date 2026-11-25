//go:build webclient && !js

package webbundle

import (
	"embed"
	"io/fs"

	"github.com/marrasen/kakel/webbundle/names"
)

// A build that fails here has no bundle yet: go run ./internal/mkclient
// builds it. The pattern takes the signature when there is one.
//
//go:embed client/client.*
var files embed.FS

// Bundle is the bundle, with the signature of its manifest as a .sig
// file has it, which is empty for one that is not signed. It reports
// whether this build has one at all.
func Bundle() (zipped, sig []byte, ok bool) {
	zipped, err := files.ReadFile("client/" + names.Zip)
	if err != nil {
		return nil, nil, false
	}
	sig, _ = files.ReadFile("client/" + names.Sig)
	return zipped, sig, true
}

// Built reports whether this build has a bundle, as Bundle does, without
// reading it.
func Built() bool {
	_, err := fs.Stat(files, "client/"+names.Zip)
	return err == nil
}
