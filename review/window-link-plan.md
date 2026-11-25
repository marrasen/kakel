# Plan: share a window with a link

Asked for on 24 November 2026. The user clicks **Share Window with
Link…**, sends the URL to somebody, and that person works in the window
from a web browser, with no kakel of their own. The link works once. The
host sees everything and types too.

Each step below is one pull request: the work, then reviews by agents
that did not write it, then fixes, then CI green, then merge.

## What Marcus settled

1. **The relay is the user's own or `kakelrelay.marras.net`.** No public
   default relay.
2. **The host approves each guest.**
3. **A pane opened later in a shared window is shared too**, with a mark.
4. **One guest per link**, and as many links as wanted.
5. **The host sends the client to the relay**, so the client always
   matches the host's kakel.
6. **Terminals only** in the first version. Other panes say they are not
   shared.

## Words

| Is now | Becomes |
|---|---|
| Serve This Window… | Remote Access… |
| Connect to Window… | Connect to Another kakel… |
| (new) | Share Window with Link… |

"Serve" shared every window, so "this window" was wrong. A *window* is
from here on only a window on the screen. Another machine's kakel is
*another kakel*, or *a kakel on <machine>*. A *guest* is somebody who
opened a link; *the host* is who shared.

Command IDs, the names in `settings`, the wire names and Go identifiers
stay as they are: `serve.window`, `serve.attach`.

## How it works

```
 host kakel ──WebSocket──► relay ◄──WebSocket── guest's browser
     │                       │  GET /s/<id>/  (page, wasm) ▲
     │ PUT bundle (signed)   │─────────────────────────────┘
     └──── SSH from end to end, through the relay ─────────┘
```

**The link** is `https://<relay>/s/<id>/#<secret>`. `<id>` is 128 random
bits, and says nothing of the relay's own secret. `<secret>` is never sent
to the relay as part of the page: it is 16 random bytes the guest's key is
derived from, and 16 bytes of the hash of the share's host key.

**Opened once** means the first guest to click Join. Fetching the page,
as chat apps and mail scanners do, uses nothing up. Join binds the share
to a session key the guest's tab made; the link key is dead from then on,
whether the host allows the guest or not. The tab keeps its session key in
`sessionStorage`, so a reload or a lost network comes back as the same
guest. A link nobody joins within 15 minutes expires. A guest away for 30
minutes ends the share.

**The relay** gets three new doors beside `host`, `join` and `accept`:

- `GET /k/<secret>/about`: JSON saying what the relay can do. An older
  relay answers 404, and the host says "This relay needs a newer kakel to
  share windows."
- `HEAD` and `PUT /k/<secret>/bundle/<sha256>`: has the relay this client,
  and take it. Only with a signature by kakel's release key, or with
  `kakel -relay -allow-unsigned`.
- `/k/<secret>/share?id=…&bundle=…`: the host registers a share, as `host`
  registers a code: the same token, knocks and `accept` tickets.

And for guests, with no relay secret:

- `GET /s/<id>/` and the bundle's files under it;
- `/s/<id>/join`: the guest's WebSocket, joined to the host as `join` is.

**The bundle** is a zip of `index.html`, `wasm_exec.js` and the client's
wasm, gzipped, built by `make` before the desktop programs and embedded in
them under the build tag `webclient`. A plain `go build` has none, and
says link sharing is not in this build. The manifest that is signed names
its purpose, so a signature made for a release's `SHA256SUMS` is never one
for a bundle.

**The guest protocol** is package `guest`, over SSH from x/crypto, its own
small server per share, and no part of `serve`: a guest can ask for
nothing but what is in it. Both ends are the same build, so it has no
version of its own.

- A control channel: host to guest, the window's state (title, layout,
  panes, theme, whether the guest may type); guest to host, join, leave
  and which pane has the focus.
- A channel per pane, of frames: the size and the whole screen, then what
  the program writes, in one ordered stream, so a resize and the output
  round it can never swap. Guest to host: what is typed.
- **The host's size stays.** The guest scales its text to fit. A guest
  never resizes a program.
- **Watch only is enforced on the host.** Typed frames are dropped there.
- A guest's terminal never answers a program's questions, never sets a
  clipboard on either side by itself, and is sent only panes standing in
  the shared window, checked again on every open.

**The guest's program** is `webclient`, built for `js/wasm`: the join page,
waiting, the window, and the end. It reuses kakel's terminal view, which
moves out of `view` into a package both build. All of it animates, as
kakel does.

**The host's window** shows that it is shared, who is in it, and which
pane they type in, and has Stop Sharing. A guest asking to join is a
dialog with the name they gave and the address they came from.

## Threats, and what answers them

| Threat | Answer |
|---|---|
| A link preview or scanner fetches the URL | Join spends it, not fetching |
| The link reaches the wrong person | the host's approval; the link dies on the first Join |
| The relay's log or a proxy's | the secret is in the fragment; SSH end to end |
| Whoever runs the relay swaps the client | not answered; said in the share dialog |
| A host serves a page of its own on the relay's domain | signed bundles only |
| A guest asks for panes elsewhere, files, tunnels, shells | no such request exists |
| A guest floods | frame, rate and channel limits; waiting guests limited |
| Guessing ids | 128 bits, and the relay's miss limit |
| A page framing or scripting the client | CSP, `frame-ancestors 'none'`, no third party |
| The guest reads what is typed as a password | never sent: only what the program draws is |

## Steps

Steps 1 to 7 are done. Of step 8 the test and the documents are done; the
reviews and `windows.yml` on the branch come last, before the merge.

### 1. The names

Every word the user sees: menus, commands, dialogs, settings, chips,
toasts, errors, the relay's own messages, `README`, `USAGE`, `FEATURES`,
`GAPS`, `WORDING`. Old words go into `commandAlso`. IDs stay.

### 2. The terminal view on its own, and a first page

- `view/term.go` and what it needs move to a package that imports nothing
  of `app`; `view` wraps it. No change anyone sees.
- `relay`'s WebSocket dialling splits by build, so a browser can join.
- `webclient` with one terminal fed by a test stream, and a test that runs
  it in headless Chrome and looks at it: drawing, keys, paste, speed.
- CI builds `GOOS=js GOARCH=wasm` so it stays building.

### 3. gunim: a program's keys in a browser

In gunim, its own pull request, following its `WIDGETS.md`:

- a program can ask for the keys a page is able to take (Ctrl+R, Ctrl+L,
  Ctrl+Shift+C, F-keys, Alt+arrows and the like), and, in full screen with
  the Keyboard Lock API, the tab keys too (Ctrl+W, Ctrl+T, Ctrl+N);
- asking before the page is closed, and full screen;
- a dock that is only looked at: no drags, no closes, no plus;
- whatever step 2 finds.

### 4. The guest protocol and the relay

Package `guest`, both ends. The relay's share, bundle and guest doors. The
host's half of them. Tests end to end in Go through a real relay, over the
race detector: link spent once, refused, expired, reconnect, watch only, a
pane that leaves the window, sizes, a slow guest, floods.

### 5. The bundle

The zip, its manifest and signature, the Makefile, the release workflow,
the embedding, the relay's checks, limits and eviction, the headers.

### 6. The host

`app`: intents, state, the share's life (link, asking, joined, away,
ended), pane membership from `app`'s layout as it changes, all errors in
the Window Log. `view`: the dialog, the approval, the chip, the marks, the
guest's name, Stop Sharing, the Settings section, palette and menu.

### 7. The guest's program

Join page, waiting, the window with its live layout and tabs, scaled text,
the bar with who and what, Leave, ended and failed pages, reconnecting,
full screen, copy and paste, touch.

### 8. All of it

One test that runs a relay, a host and headless Chrome together. Reviews
of security and of the whole. `USAGE`, `FEATURES`, `GAPS`, `RELEASING`,
`CHANGELOG`. `windows.yml` on the branch.

## Not in this

File panes, the host's own fonts (the browser draws what the bundled ones
have not, as Chinese, Japanese and Korean), a public relay, a guest
opening panes or moving the layout, two guests on one link, recording.
