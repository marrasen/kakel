package look

import "github.com/marrasen/gunim/icon"

// KindIcons are the images for the kinds of row and tab: a terminal, a
// command, a folder, a page, a log, a tunnel, a lock, a window, and one
// for each kind of file work. The guest of a window shared with a link
// is told a pane's by its name here.
var KindIcons = map[string]*icon.Icon{
	"files":       icon.Folder,
	"filemanager": icon.Folder,
	"reader":      icon.FileText,
	"log":         icon.ScrollText,
	"tunnel":      icon.Cable,
	"secrets":     icon.Lock,
	"jobs":        icon.Files,
	"copy":        icon.Copy,
	"move":        icon.FileInput,
	"delete":      icon.Trash2,
	"window":      icon.AppWindow,
	"served":      icon.ScreenShare,
	"guest":       icon.UserRound,
	"command":     icon.SquareChevronRight,
	"terminal":    icon.SquareTerminal,
	"machine":     icon.Settings,
	"split":       icon.Columns2,
	"servers":     icon.Server,
	"settings":    icon.Settings,
	"theme":       icon.Paintbrush,
	"layouts":     icon.LayoutPanelLeft,
}
