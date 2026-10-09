package view

import (
	"testing"

	"github.com/marrasen/kakel/app"
	"github.com/marrasen/kakel/machines"
)

// Files on a server are called by the server and the folder; files on
// this computer, and other panes, by their title alone.
func TestFilesOnAServerAreTitledByTheServer(t *testing.T) {
	list := []machines.Info{{ID: "s1", Name: "web"}}
	for _, c := range []struct {
		p    app.Pane
		want string
	}{
		{app.Pane{Kind: app.KindFileManager, Machine: "s1", Title: "log"}, "web: log"},
		{app.Pane{Kind: app.KindFileManager, Title: `D:\`}, `D:\`},
		{app.Pane{Kind: app.KindTerminal, Machine: "s1", Title: "me@web: ~"}, "me@web: ~"},
	} {
		if got := titleIn(list, c.p); got != c.want {
			t.Errorf("titleIn(%+v) = %q, want %q", c.p, got, c.want)
		}
	}
}
