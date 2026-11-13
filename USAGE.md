# Using kakel

Keys, the shortcuts file, sharing panes with an agent, and where things
are kept. [FEATURES.md](FEATURES.md) says what each of these is for.

## Keys

Kakel comes with:

| Key | |
|---|---|
| `Shift+PageUp` / `Shift+PageDown` | scroll the scrollback |
| mouse wheel | scroll, or arrow keys on the alternate screen |
| drag | select; `Alt+drag` selects a rectangle |
| `Shift+drag` | select even while a program owns the mouse |
| `Ctrl+Shift+C` / `Ctrl+Shift+V` | copy and paste |
| middle click | paste |
| `Ctrl+=` / `Ctrl+-` / `Ctrl+0` | font size, from 8 to 96 pixels |
| `Ctrl+Shift+B` | open or close the Machines pane |
| `Ctrl+Shift+L` | go to the Machines pane |
| `Ctrl+Shift+N` | connect to a server |
| `Shift+Win+K` on Windows, `Ctrl+Alt+K` on Linux | the launcher, from any program |
| `Ctrl+Shift+A` | show every pane at once |
| `Ctrl+PageDown` / `Ctrl+PageUp` | the next / previous tab of the pane |
| `Ctrl+Shift+PageDown` / `Ctrl+Shift+PageUp` | move the tab right / left |
| `Ctrl+Shift+M` | fill the window with the pane, or put the others back |
| `F11` | fill the screen with the panes |

A paste of more than one line, or over 5 KB, opens in an editor first.
The editor colours the code the way the pane's shell reads it: bash
(and other POSIX shells), PowerShell or the Command Prompt. The text
stays plain while a program the shell started is running, and in a
pane opened through another kakel window.
`Enter` pastes it, `Shift+Enter` starts a new line, and `Escape`
cancels. Settings › Terminal › Show before pasting turns this off.

A file manager pane showing a folder of a git repository on this
computer says the branch, how far it is ahead of or behind its
upstream, and how many files have changed, on its line along the
bottom. Where the repository's own settings name a program for git to
run, as a filter, only the branch is shown. Folders on servers show
nothing.

These are the ones you need most. "Edit Shortcuts" on the Help
menu, or `Ctrl+Shift+H`, lists every command and the keys that run it,
and changes them. The keys of the reader follow.

## Changing a shortcut

Open the Shortcuts tab in Settings, or press `Ctrl+Shift+H` or choose
Help › Edit Shortcuts to go straight to it. The groups are down the left,
with Changed among them.
Every command is listed with the keys that run it.

- Hover a row, or walk to it with the arrow keys, then press `+` or Enter
  and press the keys. Esc cancels.
- Press `×` on a shortcut to remove it, or Delete with the keyboard on it.
- `↺` puts the command back to what kakel comes with. Reset All does it
  for every command.
- Type anywhere in the pane to search by name, by keys, or by the name the
  shortcuts file uses. The tooltip on `+` shows that name.

A change applies at once, and a toast offers Undo. A key another command
has asks first, with Move Here. So does a key a program may need, such as
`Ctrl+R`, or a `Ctrl+Alt` key, which AltGr can need. A chord you would
type, such as a plain letter, is refused. Keys the system keeps, such as
`Alt+Tab`, never reach kakel. The launcher's key is on the first row, and
works from any program.

The editor keeps your changes in `keys.json`, which you can also edit by
hand. To start from every shortcut you have now, choose Settings › Files
› Shortcuts › Start the File. Edit it, then choose Read Again beside it.
If the file can't be read, the editor says why and changes nothing until
it can.

The file says what to change, not what the whole window does:

- Add a line to put a command on another chord. To move it, set the old
  chord to `"nothing"` as well, or the command runs on both.
- Delete a line and that chord goes back to what kakel comes with.
- Shortcuts added to a later kakel arrive on their own. A built-in
  chord that a later kakel moves does not, because your file still
  names the old one.

Every chord in the file runs before the pane sees it, so a chord a
program in the pane needs stops reaching it.

The file browser's own keys are not in the file.

## Every pane at once

Pane → Go To → All Panes…, or `Ctrl+Shift+A`, shows every pane of
every kakel window at once, each one live and shrunk, over the whole of
the screen the window is on. Each window shrinks from where it stands
into a card, and each of its tabs sits in the card the shape of the
window, its splits as they stand. Windows on other screens fly in from
their side. The arrows walk the panes, `Tab` goes through them in turn,
`Enter` goes to the one marked and `Escape` puts every window back as
it was. A click goes straight there: the pane grows back into its
window, which is in front, on that tab, once All Panes has gone.
`Ctrl+Shift+A` again closes it. The launcher (`Shift+Win+K`, or `Ctrl+Alt+K` on Linux) offers it
too: type "all panes" and press `Enter`. Where kakel cannot say where its
windows are, All Panes shows over the window it was asked in instead. The images are shrunk by the GPU rather than cell by
cell, and a window with nothing happening in it still skips the frames
it would have skipped anyway.

## More than one window

In All Panes, drag a pane to move it:

- onto a pane, and it joins it in a split, on the side of it you let
  go nearest; the half it would take lights up.
- onto another window's card, and it moves there as a tab. Onto its own
  window's card, it leaves its split for the tabs beside it.
- onto room no card covers, and it opens a window of its own where you
  let it go. A window's only pane stays where it is.
- over another kakel window, and it moves there; that window lights up
  while the pane is over it.

Each pane has a bar of tabs: those along the window's top stand in the
title bar, and one lower down over its pane. The tab with the keyboard
is underlined where the window shows more than one pane. A click on a
tab gives its pane the keyboard. Drag a tab to move it, without All
Panes:

- along its bar, to put it elsewhere among the tabs.
- onto another bar, in this window or another, to join those tabs.
- onto a pane: near an edge it splits the pane on that side, with that
  half lit, and in the middle it joins its tabs.
- outside every kakel window, and it opens a window of its own there.

Double-click a tab, or press `Ctrl+Shift+M`, to have its pane fill the
window; do it again, or click the button at the end of its bar, to put
the others back.

Each window has its own panes and its own pane in front. The Machines
pane lists the panes of every window, and clicking one in another
window brings that window to the front.
The pin in the title bar, before minimize, keeps a window above other
programs' windows; View › Always on Top does the same. Questions and
notices show in the window you last worked in. A
window whose last pane moves away closes. Its close button asks before
closing the panes still in it, and the last window's asks as Exit does.

## Layouts

A layout is one window, saved to open again as it is.

- **Save.** Arrange the window, then choose File › Layouts › Save
  Layout… and give it a name. A window opened from a layout offers that
  layout's name, to save over it. A name another layout has replaces
  that layout, and the dialog says so before you press Save.
- **What is saved.** The splits and tabs, the window's place and size,
  and for each terminal, command and file manager the machine, the
  shell or profile, the folder and the command. Left out are readers,
  tunnels, and panes on a server or through a kakel window removed
  from the list, and saving names them. Settings, the Jobs, the
  Layouts and the other tool panes are left out too, unsaid, as their
  own commands open them.
- **Start lines.** A terminal can hold start lines: lines kakel types
  into its shell as the layout opens it, as the keyboard would, each
  followed by Enter. One terminal might type `vite dev`, another
  `tail -f log/app.log`, one on a server `tmux attach`. Each line is
  trimmed, and a tab in one is typed as a space. They go through the
  shell, so they are in its history: Ctrl+C, Up and Enter runs one
  again.
  With Shell Setup on, the shell says when it is at its prompt. The
  first line is typed at the first prompt after Shell Setup's own
  lines, and each line after it at the next prompt, so a line that
  keeps running, as a dev server does, holds back the lines after it. A
  shell that has said nothing five seconds after it first printed, and
  one that stands quiet for three seconds at a prompt with a line not
  typed, is typed into as a shell without Shell Setup is.
  Without Shell Setup, each line is typed once the shell has printed
  something and been quiet for half a second. A line still running
  then does not hold back the next, so keep to one line, or turn Shell
  Setup on.
  Nothing is typed into a question for a password, a passphrase, a
  one-time code or a host key, and what is typed to answer it cancels
  nothing. Ctrl+C or Ctrl+D cancels the lines left, and so does
  anything else typed or pasted into the pane before the last line is
  typed, by you, by someone watching it, or by an agent, and the shell
  ending. A server that fails at first gets its lines when Retry opens
  its shell. A terminal through another kakel window, and a shell
  started again in its pane, is typed nothing into. Saving the window
  again keeps each terminal's start lines. They are saved in the
  settings file as plain text, so keep passwords out of them.
- **Panes through another kakel window.** These are kept by the
  window, and by the machine beyond it when the pane ran on one the
  window reaches. Opening the layout connects to the window once for
  all of them. A pane the layout opened there last time, and that is
  still open there, is watched again, with its program still running
  and its screen as it is. One closed there since, or any pane after
  that window's kakel was started again, opens new, as New Terminal
  there does. Closing a pane here, or the layout's window, or quitting,
  leaves the pane open on the other window: close it there to end it.
  If the layout is open twice at once, each copy has panes of its own
  there, and each comes back to its own the next time, for up to four
  copies. A pane the other window's user opened, that you worked in
  from here, is saved as a terminal there: the layout opens a new one,
  and leaves theirs alone.
- **Opening a new pane there.** A terminal runs the shell new
  terminals start on that machine, as the window picks it. On the
  window's own machine it starts in the folder it was in; on a machine
  beyond the window it starts where that machine starts it. A pane
  whose program ended there is started again there. A machine renamed
  on that window is still found. A window running an older kakel opens
  a new pane each time, a terminal in its default folder, and says so.
  A machine beyond the window that the window is not connected to
  fails its pane, saying why: connect to it from that window, then
  press Retry.
- **From a window of tools.** Save Layout in a window that holds only
  tool panes saves the window you last worked in.
- **Open.** Choose it in File › Layouts, which lists every layout, a key
  given to it beside its name. Or type its name in the palette
  (`Ctrl+Shift+K`) or the launcher, or press Open on its card in File ›
  Layouts › Manage Layouts. It always opens in a new window, so nothing
  running is closed. Each pane starts afresh.
- **When a server fails.** Each server, and each kakel window, is
  connected to once for all its panes. One that cannot be reached
  fails only its own panes, which say why and offer Retry. One removed
  from the list since fails its panes the same way.
- **Open at start.** Turn on Open at start on a layout's card, and it
  opens in its own window each time kakel starts.
- **Manage.** File › Layouts › Manage Layouts lists the layouts as
  cards: Open, Rename, Remove (with Undo on the notice) and Open at
  start.
- **A key for a layout.** Settings › Shortcuts lists each layout under
  Layouts, as Open and its name; give it a key there. Its command is
  `layout.open.<id>`, which a rename leaves as it was. A layout
  removed keeps its key, listed as Open Removed Layout, so Undo brings
  both back.

## Sharing a pane with an agent

Click into a terminal and choose Share → Share Panes…. The first time,
that starts a share with that pane. After that it opens the Agent Share
dialog: the one code, a tick box for each terminal pane that puts it in
the share or takes it out, and which agent the prompt is for -- Claude
Code, Codex, Cursor, or another host that takes a JSON MCP config. The
agent picked is remembered for next time. The "Agent Share" chip on the
menu bar opens the same dialog.

The dialog's buttons:

- **Copy Prompt** puts a prompt on the clipboard and does nothing else.
  Paste the whole of it to the agent. It carries the code and says how
  that host adds kakel's `kakel -mcp` server. What the tools do and
  what the rules are come from the server's own instructions once the
  agent connects, so the prompt does not repeat them.
- **Copy Code** copies the code alone.
- **Setup…** shows the command line, or the JSON for a host set up by a
  file, that adds the server, with a button to copy it.
- **Write Skill** saves a `SKILL.md` where that host reads skills from,
  and says where it went. `kakel -mcp-skill` prints the same file.
- **Stop Sharing** ends the share, and the code stops working. Taking
  the last pane out does the same.

Share → Permissions… shows four tick boxes for the pane you are on,
saying what the agent may do there beyond reading the pane and typing
into it: read only, read cleared scrollback, open more panes, and
restart the connection. Every box starts off, and a change takes effect
at once. An agent that hits a password prompt can ask you to type it
into the pane: a line appears saying what it wants, what you type goes
to the program, and the agent is told you typed something and never
what.

Kakel writes down what an agent types, and Share → Typing History shows
it for the pane you are on. You hand the pane over, you give the access
and you hold the secrets, so what the agent does in there is yours to
read. It is what the agent sent, not what the shell ran: a line it
edited before pressing Enter is there as it was typed. A secret you type
at the agent's asking is not in it, because you typed that yourself.
The record lives as long as the pane: it is capped, it says how many of
the oldest entries it has dropped, and it goes when the pane closes.

## Serving this window, and working in another

Serving this window and connecting to another are on the Share menu
rather than on a key. "Serve This Window…" asks for the port and where
to listen, and then shows the address and the host key's fingerprint to
check. "Connect to Window…" asks for the address and the key file to
offer. Connecting opens nothing over there: what
that window has open lands in the Machines pane under its name, and the plus
on that heading opens a pane on it. The servers that window is
connected to get headings of their own, and their plus opens things on
them through it. Nothing listens until you ask it to,
and the keys allowed in are the ones you list in an `authorized_keys`
file in kakel's own directory, not the one in `~/.ssh`. The Serve dialog
lists them: Add Key… takes a public key pasted, or one of this
machine's, and Remove Key… takes one off and hangs up on the window it
let in. If you made that file a link to `~/.ssh/authorized_keys`, the
keys added here may log in over ssh too.

### Through a relay

When the other machine cannot reach this one, serve through a relay
(see below). Tick "Through a relay" in the Serve dialog and paste the
relay's URL. Kakel makes a code, such as `7KQ2-M9XD`, and keeps it in
`relay_code` beside the serving key, so it is the same every time this
window is served and each machine has its own. The other end types
that code. Copy Link puts the relay's URL and the code on the
clipboard, for pasting at the other end in one go. New Code makes
another; the old one lets nobody else in. Untick "On a port of this
machine" to serve through the relay alone. The keys that may connect
are the same either way.

At the other end, "Connect to Window…" with "Through a relay" takes the
relay's URL and the code; pasting the copied link into either field
fills in both. The relay used last is offered again. A window saved in
the server list as a kakel window takes the relay's URL as its address
and the code in Code. The host key is checked as for any window, under
the code and the relay's name: a new code is a new window to trust.

Both dialogs have a Through choice: a saved server to reach the relay,
or the window itself, through, as a server saved behind another one is
reached. The stream goes out from that server over the SSH connection
to it, which kakel makes first when it is not connected. A saved kakel
window takes a Through as well.

Kakel reaches a relay through the HTTP proxy that `HTTPS_PROXY` names
or, on Windows, the one set for the system. Settings › Sharing ›
Relays › Proxy overrides both: a URL such as
`http://user:password@proxy:8080`, or `none` to go straight there. A
proxy found only through a PAC script, or one that signs in with
Windows (NTLM or Kerberos), is not supported yet.

### Tunnels with plain ssh

A served window also takes ssh's own tunnels, with the same keys and
the same host key. There is no shell, so give `-N`:

    ssh -p 2222 -N -L 8080:intranet:80 kakel@office-pc

That makes port 8080 here reach `intranet` port 80 as the served
machine finds it. `-J` and `-W` work the same way. The user name says
where the tunnel goes out from: `kakel` for the served machine itself,
or a machine saved there, by its name as the commands spell it
(`db-server` for "DB Server"):

    ssh -p 2222 -N -L 5432:localhost:5432 db-server@office-pc

At first a tunnel reaches what a kakel window connecting reaches: the
servers connected over there already. Turn on Settings › Sharing ›
Tunnels may connect, on the served machine, and a saved server not
connected is connected to on the way, when that needs nothing asked: a
key, or a secret while the secrets are open. A question would wait on
a screen nobody may be at, so the tunnel is refused instead, saying to
connect it there first. With it on, a kakel window connected over there
can be gone through too.

Through a relay, ssh runs kakel to reach it:

    ssh -o ProxyCommand="kakel -relay-pipe https://relay.example.com/k/…#7KQ2-M9XD" -N -L 8080:intranet:80 kakel@office-pc

`-relay-pipe` takes `-proxy` for an HTTP proxy, or uses the system's.

"Tunnels with ssh…", on the Share menu, in the served dialog and in
Settings › Sharing, explains all this with the commands filled in for
this window: its port, its name, its relay and the machines saved here,
each with a Copy beside it.

## Running a relay

A relay joins two kakel windows that cannot reach each other: both
reach the relay instead, over WebSockets on HTTPS, which gets through
HTTP proxies. It runs on a server with no screen:

    kakel -relay -domain relay.example.com

That gets a certificate from Let's Encrypt, so ports 443 and 80 must be
reachable. Behind nginx or Caddy doing HTTPS, run it on plain HTTP
instead and point the proxy at it, passing WebSocket upgrades:

    kakel -relay -listen 127.0.0.1:8080

It prints its URL, ending in `/k/` and a secret. Without the secret the
relay answers nothing, so keep the URL to the people who should use it.
The secret is kept in `relay_secret` in kakel's private directory (or
`-dir`); `-new-secret` makes a new one and the old URL stops working.

The relay only carries bytes. The windows speak SSH to each other
through it, so it cannot read what passes, and the served window still
lets in only the keys it lists.

## Where kakel keeps its files

Kakel keeps its files where the operating system puts a program's.
Make a directory called `kakel-files` beside `kakel.exe` and it
keeps them there instead, so one machine can hold several copies with
files of their own. Settings › Files names every file and says how to
move them.

Kakel used to be called gridterm. On its first start it renames
gridterm's old directories: `gridterm` becomes `kakel`, and
`gridterm-files` becomes `kakel-files`.

## The file manager

The file manager is a pane, like a terminal: split it, put it in a tab,
or drag it to another window. Files on a server's card, or the Files
command, opens one beside the file manager in front; Files in a New
Window opens one in a window of its own. Files from the launcher or the
tray opens a new tab in the window you last worked in; Settings ›
General › Files open in can make it split beside the file manager
there, or open a window of its own. Opening a folder from Windows,
once kakel opens folders, opens a window with one. A window of file
managers alone opens where the last one was, and as big, as it closed.

kakel's menus are its menus. While a file manager is in front, its
lines join kakel's File, Edit and View menus, a Go menu comes after
View, and the lines for a terminal alone leave. Edit's Cut, Copy, Paste
and Select All act on the files, File › New File Manager Window opens
another in a window of its own, and Close Pane closes it. `Enter` opens,
`Backspace` or `Alt+Left` goes back, with the folder you left selected,
`Alt+Up` goes up, and `Ctrl+L` types a path. `Ctrl+C`, `Ctrl+X` and `Ctrl+V` copy, cut and paste, also
between machines, `F2` renames and `Delete` moves to the trash. `F5`
or `Ctrl+R` lists the folder again, even with the keyboard elsewhere
in the window.
`Ctrl+F` filters the folder, `Ctrl+P` finds a file, `Ctrl+1` and
`Ctrl+2` switch between details and icons, and `Space` views a file.
The button beside the filter, or Go › Filter subfolders too, has the
filter look in every folder under this one; a Folder column then says
where each item sits, and Go to its folder on an item's menu goes
there.
`Ctrl+W` closes the pane. Keys with `Ctrl+Shift` stay kakel's, as
`Ctrl+Shift+D` to split and `Ctrl+Shift+C` to copy, except
`Ctrl+Shift+N`, a new folder. Copy path is on a file's menu.

Create zip… and Paste as zip… on the menus make a zip, and Extract…
unpacks a zip or a tar into a new folder beside it. Tick Protect with a
password to protect a new zip: what it holds is encrypted with AES-256,
which 7-Zip and WinZip open too, while the names of the files in it
stay readable. Extracting a zip a password protects, made by kakel,
7-Zip, WinZip or `zip -P`, asks for the password before it makes the
folder. The question offers your saved secrets, and can save the
password typed there once it has opened the zip, under the zip's name.
A secret with the zip's name opens it without asking.

The `+` at the end of the tabs opens a tab like the one in front: a
file manager at the same folder, or a terminal. Right-click it to pick
one.

`Space`, or View on a file's menu, shows the file over the window: a
picture large, code in its language's colours, Markdown rendered, and
anything else as its bytes in hex. `Space` or `Escape` closes it. The
preview beside the files colours code and renders Markdown too.
`Ctrl`+click a file's path in a terminal to view it the same way, at
the line a compiler named.
