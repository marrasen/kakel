# Plan: a shared window, second round

Asked for on 28 November 2026, after Marcus tried Share Window with Link
(v0.12.2-beta.19, plan in `window-link-plan.md`). Each step below is one
pull request: the work, reviews by agents that did not write it, fixes,
CI green on Linux and Windows, then merge.

## What Marcus found and settled

1. **A hidden guest page doesn't move the guest's name.** With the
   guest's browser tab hidden, the page followed the host's tabs, but the
   host's badge stayed on the first tab: a hidden page draws nothing, so
   it never takes the pane, and never says so.
2. **The guest's bar shows the host's title**, not that of the guest's
   own pane.
3. **Panes float on the guest's page.** Each pane scales its text on its
   own, with no edges. Settled: the host's window, whole, at one size
   (A), and, as a choice on the share, the guest's page setting the size
   (C). The choice changes at any time during a share, as Type / Only
   watch does.
4. **Follow.** A guest who picked a tab of its own can go back to
   following the host, with a button.
5. **Files.** The guest gets gunim's file manager, all of what a Files
   pane on a server has, and download and upload through the browser.
   The share has one folder, picked, that the guest reaches with what is
   under it and never above. Files: None / Look / Change, changeable at
   any time, starting at None.

## Steps

### 1. Following, and the title

- The guest says whether it follows the host, a new control frame,
  rather than only which pane it is in. While it follows, the host takes
  the guest to be in the host's own pane, whatever the page has drawn.
  The host's badge and chip stand where the guest is: following, or in a
  pane of its own.
- A **Follow <host>** button on the guest's bar, shown (animated) only
  while the guest does not follow. What ends following today still ends
  it.
- The bar's title is that of the pane the guest is in.
- Tests: following with the page drawing nothing (a hidden tab), the
  button, the title.

### 2. gunim: files in a browser

In gunim, its own pull request, by `WIDGETS.md` and its CLAUDE.md:

- files dropped on a page from the computer, with names, sizes and their
  contents to read as a stream, never whole in memory;
- a file picker for one or several files, the same;
- saving a stream as a download: to disk as it comes where the browser
  can (`showSaveFilePicker`), and otherwise collected outside the wasm
  heap;
- the same API on the desktop drivers where it makes sense, so a program
  is written once.

### 3. The host's window, whole, at one size (A)

- The host tells the guest the size of the window's panes area and of a
  cell, so the guest can lay the dock out at the host's size.
- The guest draws it as one picture scaled to fit the page: the same
  splits, one text size in every pane, every terminal filling its pane,
  a frame round the window. Panes have edges. The guest's pane has the
  guest's ring; the host's pane a thin line and the host's name.
- A phone, or a narrow page, keeps one pane at a time.
- Scaling a subtree may need a node in gunim: if so it is added there
  first, by the rules above.

### 4. The guest's page sets the size (C)

- A share has **Size for: Me / The guest**, in the share's card and the
  share dialog, changeable at any time, as Type / Only watch is. Watch
  only is enforced on the host; so is this.
- With The guest, the guest's panes fill its page at a readable text
  size, and the guest tells the host each shown terminal's columns and
  rows. The host resizes the program, as for a pane taken over by
  another kakel, and its own pane shows the program at the guest's size,
  the rest hatched, with "Sized for <guest>'s page". Larger than the
  host's pane, it is drawn smaller to fit.
- Back to Me, the guest leaving or the share ending gives the panes the
  host's sizes again.

### 5. The host's files

- The share has a folder, and **Files: None / Look / Change**, in the
  card and the dialog, changeable at any time. The folder starts as the
  one a Files pane in the window shows, and is changeable with Choose….
- A guest channel carries SFTP to a server of the share's own, rooted at
  the folder with `os.Root`, so no path, `..` or link leads above it.
  Look allows reading; Change also writing, renaming and removing. Every
  request is checked against the permission as it stands now; going down
  to Look or None closes what was open for more.
- Every upload, download, rename and removal goes into the Window Log,
  run together where there are many.
- Limits as for the other channels: open files, requests, bytes.

### 6. The guest's file manager

- A Files pane in the shared window is gunim's file manager on the
  guest's page, over that SFTP channel, starting in the host's pane's
  folder where that is inside the share's folder, and in the share's
  folder otherwise. Nothing above it is shown or reachable.
- Look is read only, in the file manager's own way. None says "Not
  shared" as now.
- Download a file, or a folder as a zip; upload with a button or by
  dropping files on the pane.
- What gunim's file manager lacks for this (a root, read only) is added
  to gunim.

### 7. All of it

A test with a relay, a host and headless Chrome: follow, both sizes,
files at each permission. A security review, a review of the whole.
`USAGE`, `FEATURES`, `GAPS`, `CHANGELOG`. `windows.yml` on the branch.

## Not in this

The host's Trash for the guest, Open with or anything that runs a program
on the host, several folders per share, a guest opening panes.
