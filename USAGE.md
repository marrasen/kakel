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
pane opened through another kakel.
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
  tunnels, and panes on a server or through another kakel removed
  from the list, and saving names them. Settings, the Jobs, the
  Layouts and the other tool panes are left out too, unsaid, as their
  own commands open them.
- **Start lines.** A terminal can hold start lines: lines kakel types
  into its shell as the layout opens it, as the keyboard would, each
  followed by Enter. One terminal might type `vite dev`, another
  `tail -f log/app.log`, one on a server `tmux attach`. Spaces at the
  end of a line are dropped, and a tab in one is typed as a space. A
  space at the start stays, so a shell set to ignore such lines keeps
  that line out of its history. The others go into it: Ctrl+C, Up and
  Enter runs one again.
  With Shell Setup on, the shell says when it is at its prompt. The
  first line is typed at the first prompt after Shell Setup's own
  lines, and each line after it at the next prompt, so a line that
  keeps running, as a dev server does, holds back the lines after it.
  Only Shell Setup's own marks count, so a prompt that marks itself
  as well is counted once. A shell that has said nothing five seconds
  after it first printed gets each line once it has been quiet for half
  a second. On a server whose login shell is not bash or zsh, such as
  fish, turn Shell Setup off for it: in the server's settings pane,
  pick Login Shell and clear "Let kakel follow the shell's folder and
  commands". Such a shell takes Shell Setup's lines as its own input.
  With it off, the start lines are typed as for a shell without Shell
  Setup. A shell that shows fewer prompts while it is set up than kakel
  expects gets the next line once it stands quiet at a prompt, with
  text before the cursor and no command started, for ten seconds, or
  three once Shell Setup's prompts are past; then it waits for the next
  prompt as before.
  Without Shell Setup, each line is typed once the shell has printed
  something and been quiet for half a second. A line still running
  then does not hold back the next, so keep to one line, or turn Shell
  Setup on.
  Nothing is typed into a question for a password, a passphrase, a
  one-time code or a host key, and what is typed to answer it cancels
  nothing. A prompt Shell Setup marked is never taken for such a
  question, whatever it reads. Ctrl+C or Ctrl+D cancels the lines left, and so does
  anything else typed or pasted into the pane before the last line is
  typed, by you, by someone watching it, or by an agent, and the shell
  ending. A server that fails at first gets its lines when Retry opens
  its shell. A terminal through another kakel, and a shell
  started again in its pane, is typed nothing into. Saving the window
  again keeps each terminal's start lines. They are saved in the
  settings file as plain text, so keep passwords out of them.
- **Giving a terminal start lines.** Press Start Lines… on the layout's
  card in File › Layouts › Manage Layouts. The card counts the start
  lines the layout's terminals type, as "4 terminals, 3 start lines";
  lines a terminal through another kakel holds are not counted.
  The dialog
  draws the window across its width: each pane is a box where it
  stands, its name, its machine when that is not this computer, and as
  many of its lines as fit. With more lines than fit, the last row
  says how many more. A terminal is filled
  like the editor; a pane that takes no lines is quieter, its icon
  dimmed. A dot on a pane's icon says it holds lines. The pointer over a
  box brightens its border. A group of tabs
  is one box with its tabs along its top. The tab in front shows its
  name, cut where the box is crowded. Each tab behind shows its name
  whole, shortest names first, as far as the room goes; the rest show
  their icons, and name themselves in a tooltip. In a box too narrow
  for every icon, the tabs behind are all dots and the tab in front
  keeps its icon and a pill of its own, so the one coloured dot reads
  as a tab holding lines. A box turns between icons and dots over a
  moment as the window is resized, never resting half way. Click a pane or a tab, or move with the
  arrow keys: Left and Right go along a box's tabs first, then on to
  the nearest box beside. Of two boxes as near, as the two panes over
  one across a T, an arrow goes back to the one it came from.
  Home and End pick the first pane and the last. A click, Enter or
  Space puts the keyboard in the pane's lines. Under the map are the
  pane's name and where it starts, as its shell, its machine and its
  folder, so no `cd` is needed, on a second line where the window is
  narrow, and an editor for its lines, one per line. An empty editor
  reads `npm run dev`. The
  editor is coloured for that shell, and marks a line written
  for another: a PowerShell command in a bash terminal is marked
  "PowerShell command. This terminal starts Bash.", and in a server's
  login shell "PowerShell command. This terminal starts the login
  shell." A pane that takes no lines shows a quiet well in place of the
  editor, saying what it is, such as "Start lines are for terminals.
  This pane runs a command." Tab moves from the
  map to the editor and on to the buttons, and a tab typed in the
  editor is a space. Save keeps the lines of each terminal whose lines
  you changed, with spaces at the end and blank lines dropped, and
  leaves the others as they are; Cancel and Escape keep none of what
  was typed. A command pane, a file manager and a terminal through
  another kakel have no start lines yet; lines such a terminal
  already holds stay as they are. Where the layout was removed
  meanwhile, saved over with other splits, tabs or panes, or the same
  terminal given other lines in another window, Save says so and the
  dialog stays open. Where it was saved over with nothing changed, Save
  saves, and where a window opened before that save is still open, a
  notice says its terminals keep the lines they have. A window
  opened from the layout, or saved as it, keeps the lines given here
  when it is saved over the layout again. Lines saved while that
  window's terminal is still starting are the ones typed into it.
  The lines reach only the windows opened from the layout as it is
  now, or saved as it. When the same layout is open in two windows and
  one is saved over it, the other window's terminals no longer follow
  the dialog: they keep the lines they have, and saving that window
  over the layout writes those. The same goes for a layout saved over
  by another kakel, and for a layout removed and a new one saved under
  its name. Undo of a removal puts the layout back as it was, and the
  dialog reaches its windows again.
- **Panes through another kakel.** These are kept by that kakel, and by
  the machine beyond it when the pane ran on one it reaches. Opening the
  layout connects to the other kakel once for all of them. A pane the
  layout opened there last time, and that is still open there, is
  watched again, with its program still running and its screen as it is.
  One closed there since, or any pane after that kakel was started
  again, opens new, as New Terminal there does. Closing a pane here, or
  the layout's window, or quitting, leaves the pane open in the other
  kakel: close it there to end it. If the layout is open twice at once,
  each copy has panes of its own there, and each comes back to its own
  the next time, for up to four copies. A pane the other kakel's user
  opened, that you worked in from here, is saved as a terminal there:
  the layout opens a new one, and leaves theirs alone.
- **Opening a new pane there.** A terminal runs the shell new
  terminals start on that machine, as that kakel picks it. On the
  kakel's own machine it starts in the folder it was in; on a machine
  beyond it, it starts where that machine starts it. A pane whose
  program ended there is started again there. A machine renamed in that
  kakel is still found. An older kakel there opens a new pane each time,
  a terminal in its default folder, and says so. A machine beyond the
  kakel that it is not connected to fails its pane, saying why: connect
  to it from that kakel, then press Retry.
- **From a window of tools.** Save Layout in a window that holds only
  tool panes saves the window you last worked in.
- **Open.** Choose it in File › Layouts, which lists every layout, a key
  given to it beside its name. Or type its name in the palette
  (`Ctrl+Shift+K`) or the launcher, or press Open on its card in File ›
  Layouts › Manage Layouts. It always opens in a new window, so nothing
  running is closed. Each pane starts afresh.
- **When a server fails.** Each server, and each other kakel, is
  connected to once for all its panes. One that cannot be reached
  fails only its own panes, which say why and offer Retry. One removed
  from the list since fails its panes the same way.
- **Open at start.** Turn on Open at start on a layout's card, and it
  opens in its own window each time kakel starts.
- **Manage.** File › Layouts › Manage Layouts lists the layouts as
  cards: Open, Rename, Start Lines…, Remove (with Undo on the notice)
  and Open at start.
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

## Sharing a window with a link

Somebody with a web browser, and no kakel, can work in one of your
windows with you. They see its terminals as you do, and type in them if
you let them.

1. Choose Share → Share Window with Link….
2. Give the name the guest is told you have, and choose whether the guest
   can type or only watch. Paste the URL of a relay (see Running a
   relay). What you choose is offered again the next time.
3. Press Create. kakel gives the relay the page the guest opens, if it
   does not have it, makes the link and copies it. The dialog shows the
   link and how it stands.
4. Send the link to one person. It works once, and expires after 15
   minutes if nobody joins with it. Opening it uses nothing up: pressing
   Join does.
5. When the guest joins, kakel asks you. The question shows the name the
   guest wrote, the address they came from and their browser. Anybody can
   write any name, so check with them if you are not sure. Choose Type or
   Only watch, and press Allow or Refuse. Nothing in the question works
   until no key and no mouse button has been pressed for a second, so it
   cannot be answered by accident. After that Enter and Escape refuse,
   and from the keyboard Allow works once it has had it for a second.
   You have ten minutes to answer.

The guest sees the terminals of that window and nothing else: no other
window, no files, no settings and no dialogs. Of a pane that is no
terminal they see what kind it is. A terminal you open in the window
later, or move into it, is shared too, and kakel says so when you did not
move it there yourself. A terminal you asked for in another window opens
in that window, also when its server answers after you went to the shared
one. One that you move out of the shared window, or close, is gone from
the guest's page at once. A terminal running a command is called Command
there, as a command line may hold a password.

A guest who can type can do anything you can in those terminals.
Whoever runs the relay could see everything: the page the guest opens
comes from it.

While the window is shared:

- A chip on its menu bar says Shared, or who is in it. A click on it
  lists the links, with Type or Only watch, and Stop, for each. Add makes
  another, for another guest.
- Each terminal a guest sees has a gold ring, bright round the one the
  guest is in. The guest's name is on that terminal's tab.
- The Machines pane lists each guest under This computer. The × on the
  row ends that share.
- The size of the terminals stays yours. The guest's text is scaled to
  fit.

A guest who loses the connection, or reloads the page, comes back by
itself for 30 minutes, also when the relay was restarted meanwhile.
Sharing ends when you choose Stop Sharing Window, when the guest leaves,
when the window closes, and when kakel exits. The Window Log keeps when a
window was shared, who asked, what you answered and how it ended, and
never the link.

### As the guest

Open the link in a current Chrome, Edge, Firefox or Safari, on a computer
or a phone. Write your name and press Join. The window opens once the
host allows you, with the host's panes, tabs and theme. Your tabs follow
the host's until you pick one, click in a terminal or type. From then you
have your own.

- The bar says who shares the window, and whether you can type or only
  watch. Typing while you only watch shakes it, and lights the pane's
  edge.
- Copy and paste as in kakel: Ctrl+Shift+C and Ctrl+Shift+V, Ctrl+Insert
  and Shift+Insert, Cmd+C and Cmd+V on a Mac, or a right click. A paste
  of several lines is asked about first.
- Shift+Page Up and Page Down, and the wheel, scroll back through what
  your page has been shown.
- Full screen gives the terminal more of the browser's own keys: see the
  button on the bar.
- On a narrow page one pane shows at a time, with a tab for each. With a
  finger, so does a phone on its side, and there is a row of keys a
  phone's keyboard has not: Esc, Tab, Ctrl and the arrows, with Alt,
  Home, End, PgUp and PgDn under …. A tap is a click. A finger held
  selects the word under it, or keeps the selection it is in, takes in
  more as it moves, and opens the menu as it lifts.
- What you type in a pane that is still opening is typed once it has. If
  the connection goes first, none of it is, and the page says so.
- A lost connection comes back by itself, and so does a reload. Opened
  in two tabs, the window is in the one opened last, and the other has
  Use Here.
- Leave ends your part for good. To come back you need a new link.

Settings › Sharing › Sharing with a link has the name, the choice and the
relay that the next link starts with.

## Remote access, and connecting to another kakel

Remote access lets a kakel on another machine, with an allowed key, use
this whole kakel as you: the panes of every window, shells, files and
tunnels. It goes on with no window open, until you turn it off.

Remote Access… and Connect to Another kakel… are on the Share menu
rather than on a key. "Remote Access…" asks for the port and where to
listen, and Turn On then shows the address and the host key's
fingerprint to check. "Connect to Another kakel…" asks for the address
and the key file to offer. Connecting opens nothing over there: what
that kakel has open lands in the Machines pane under its name, and the
plus on that heading opens a pane on it. The servers that kakel is
connected to get headings of their own, and their plus opens things on
them through it. Nothing listens until you ask it to, and the keys
allowed in are the ones you list in an `authorized_keys` file in kakel's
own directory, not the one in `~/.ssh`. The Remote Access dialog lists
them: Add Key… takes a public key pasted, or one of this machine's, and
Remove Key… takes one off and hangs up on the kakel it let in. If you
made that file a link to `~/.ssh/authorized_keys`, the keys added here
may log in over ssh too.

### Through a relay

When the other machine cannot reach this one, use a relay (see below).
Tick "Through a relay" in the Remote Access dialog and paste the relay's
URL. kakel makes a code, such as `7KQ2-M9XD`, and keeps it in
`relay_code` beside the remote access key, so it is the same every time
remote access is turned on and each machine has its own. The other end
types that code. Copy Link puts the relay's URL and the code on the
clipboard, for pasting at the other end in one go. New Code makes
another; the old one lets nobody else in. Untick "On a port of this
machine" to use the relay alone. The keys that may connect are the same
either way.

At the other end, "Connect to Another kakel…" with "Through a relay"
takes the relay's URL and the code; pasting the copied link into either
field fills in both. The relay used last is offered again. A machine
saved in the server list with the type kakel takes the relay's URL as
its address and the code in Code. The host key is checked as for any
kakel, under the code and the relay's name: a new code is a new kakel to
trust.

Both dialogs have a Through choice: a saved server to reach the relay,
or the other kakel itself, through, as a server saved behind another one
is reached. The stream goes out from that server over the SSH connection
to it, which kakel makes first when it is not connected. A saved kakel
takes a Through as well.

Kakel reaches a relay through the HTTP proxy that `HTTPS_PROXY` names
or, on Windows, the one set for the system. Settings › Sharing ›
Relays › Proxy overrides both: a URL such as
`http://user:password@proxy:8080`, or `none` to go straight there. A
proxy found only through a PAC script, or one that signs in with
Windows (NTLM or Kerberos), is not supported yet.

### Tunnels with plain ssh

A kakel with remote access on also takes ssh's own tunnels, with the
same keys and the same host key. There is no shell, so give `-N`:

    ssh -p 2222 -N -L 8080:intranet:80 kakel@office-pc

That makes port 8080 here reach `intranet` port 80 as the machine
connected to finds it. `-J` and `-W` work the same way. The user name
says where the tunnel goes out from: `kakel` for that machine itself,
or a machine saved there, by its name as the commands spell it
(`db-server` for "DB Server"):

    ssh -p 2222 -N -L 5432:localhost:5432 db-server@office-pc

At first a tunnel reaches what another kakel connecting reaches: the
servers connected over there already. Turn on Settings › Sharing ›
Tunnels may connect, on the machine connected to, and a saved server not
connected is connected to on the way, when that needs nothing asked: a
key, or a secret while the secrets are open. A question would wait on
a screen nobody may be at, so the tunnel is refused instead, saying to
connect it there first. With it on, another kakel connected over there
can be gone through too.

Through a relay, ssh runs kakel to reach it:

    ssh -o ProxyCommand="kakel -relay-pipe https://relay.example.com/k/…#7KQ2-M9XD" -N -L 8080:intranet:80 kakel@office-pc

`-relay-pipe` takes `-proxy` for an HTTP proxy, or uses the system's.

"Tunnels with ssh…", on the Share menu, in the Remote Access dialog and
in Settings › Sharing, explains all this with the commands filled in for
this kakel: its port, its name, its relay and the machines saved here,
each with a Copy beside it.

## Running a relay

A relay joins two kakels that cannot reach each other: both
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

The relay only carries bytes. The two kakels speak SSH to each other
through it, so it cannot read what passes, and the one connected to
still lets in only the keys it lists.

A relay also serves windows shared with a link, under `/s/`. It has to
be the kakel of the release that shares them or a newer one: to update
it, replace the program and start it again with the same options. Its
secret and URL stay, remote access comes back by itself, and so do the
windows shared and their guests. The links do not hold the secret. A
proxy in front of the relay has to:

- pass on everything under the relay's address, `/s/` and what follows it
  too, and not `/k/` alone;
- pass on the Host header and WebSocket upgrades;
- add the caller's address as `X-Forwarded-For`;
- run on the same machine as the relay. The relay believes
  `X-Forwarded-For` from this machine alone. From any other, every guest
  has the proxy's own address, and they share what the relay allows one
  address.

A kakel that shares a window gives the relay the page its guests open.
The relay takes only one signed with kakel's release key. `-allow-unsigned`
takes any, for trying a kakel you built, and lets anyone with the
relay's URL put a page of their own on its domain. To try one on this
machine alone:

    kakel -relay -allow-unsigned

Its URL is then `http://127.0.0.1:8080/k/` and the secret it prints.

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
