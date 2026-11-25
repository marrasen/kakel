//go:build !js

package look

import (
	"image/color"

	"github.com/marrasen/gunim/filemanager"
	"github.com/marrasen/gunim/theme"
	"github.com/marrasen/gunim/widget"

	"github.com/marrasen/kakel/vt"
)

// filesOf is the file manager's colours in a pane, from the terminal's
// own: its ground and ink, and its colours for the marks of each kind of
// file, picked to read on the ground. strong is the accent on stage.
func filesOf(pal vt.Palette, strong color.NRGBA) []theme.Entry {
	bg, fg := nrgba(pal.BG), nrgba(pal.FG)
	ansi := func(dim, bright int) color.NRGBA { return standout(bg, nrgba(pal.ANSI[dim]), nrgba(pal.ANSI[bright])) }
	out := []theme.Entry{
		theme.Set(filemanager.SidebarFill, mix(bg, fg, 4)),
		theme.Set(filemanager.SidebarHot, alpha(fg, 0x14)),
		theme.Set(filemanager.SidebarOn, alpha(strong, 0x38)),
		theme.Set(filemanager.PaneFill, mix(bg, fg, 3)),
		theme.Set(filemanager.Faint, mix(fg, bg, 40)),
		theme.Set(filemanager.Caption, mix(fg, bg, 52)),
		theme.Set(filemanager.ErrorInk, ansi(1, 9)),
		theme.Set(filemanager.ErrorFill, mix(bg, nrgba(pal.ANSI[1]), 30)),
		theme.Set(filemanager.PlaceLit, ansi(2, 10)),
		theme.Set(filemanager.CloudLocalInk, ansi(2, 10)),
		theme.Set(filemanager.CloudPinnedInk, nrgba(pal.ANSI[2])),
		theme.Set(widget.MenubarFill, mix(bg, fg, 4)),
		// Disk usage's colours for what a cloud provider keeps online,
		// what it keeps here, and what is in no cloud, and its boxes.
		theme.Set(filemanager.UsageOnline, ansi(4, 12)),
		theme.Set(filemanager.UsageLocal, ansi(3, 11)),
		theme.Set(filemanager.UsagePlain, mix(fg, bg, 45)),
		theme.Set(filemanager.UsageTrack, alpha(fg, 0x14)),
		theme.Set(filemanager.UsageBox, mix(bg, fg, 8)),
		theme.Set(filemanager.UsageEdge, mix(bg, fg, 2)),
		theme.Set(filemanager.UsageHot, alpha(fg, 0x40)),
	}
	for tint, c := range map[filemanager.Tint]color.NRGBA{
		filemanager.TintOther:    mix(fg, bg, 40),
		filemanager.TintFolder:   ansi(3, 11),
		filemanager.TintImage:    ansi(5, 13),
		filemanager.TintVideo:    nrgba(pal.ANSI[9]),
		filemanager.TintAudio:    ansi(6, 14),
		filemanager.TintArchive:  mix(ansi(3, 11), ansi(1, 9), 50),
		filemanager.TintDocument: ansi(4, 12),
		filemanager.TintCode:     ansi(2, 10),
		filemanager.TintProgram:  ansi(1, 9),
	} {
		out = append(out, theme.Set(filemanager.TintToken(tint), c))
	}
	return out
}
