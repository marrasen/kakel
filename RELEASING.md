# Cutting a release

Releases are driven by a tag. Everything else is done by CI.

## The steps

1. **Write the changelog.** Move what is under `## Unreleased` in
   [CHANGELOG.md](CHANGELOG.md) into a new `## vX.Y.Z` heading. The
   release notes on GitHub are taken from that section by the workflow,
   and a tag whose version has no section fails the build rather than
   publishing an empty release.
2. **Commit it**, on `main`, and run the suite on Windows first, as
   Checks runs it on Linux alone:
   `gh workflow run windows.yml --ref main`, and wait for it to pass.
3. **Tag and push:**

   ```
   git tag v0.1.0
   git push origin v0.1.0
   ```

4. CI runs the suite on Windows and on Linux. Only if both pass does it
   build the binaries, and put them on a GitHub release with the notes
   from the changelog.

## Betas

A beta is a tag with `-beta.N` after the version, as `v0.12.0-beta.1`,
and its own `## v0.12.0-beta.1` section in the changelog. The same
steps cut it, from any branch. CI publishes it as a pre-release, so
GitHub's latest release stays the last one that is not a beta.

Only kakels that take betas update to one: a beta itself, until told
otherwise, and any kakel with Settings › General › Beta releases on.
`install.ps1` and `install.sh` take the newest beta with `KAKEL_BETA=1`.
A beta updates to the release it led to, as that is newer.
`-alpha.N` and `-rc.N` work the same way; nothing else after a dash
counts as a release.

## Numbering

[Semantic versioning](https://semver.org/spec/v2.0.0.html). While the
major version is 0 the shape is still moving, and a minor bump may
change how something behaves. `v1.0.0` is for when that stops being
true.

## What a release ships

- `kakel_vX.Y.Z_windows_amd64.zip`
- `kakel_vX.Y.Z_linux_amd64.tar.gz`
- `SHA256SUMS`
- `SHA256SUMS.sig`, the signature of `SHA256SUMS`

Keep those names. `install.ps1`, `install.sh` and kakel's own updates
find the archive by them, and check it against `SHA256SUMS`. An
installed kakel runs an update only when `SHA256SUMS.sig` matches the
public key in `internal/build/build.go`. An
installed kakel updates only from a release: a build that calls itself
`dev-…` or `v…-N-g…` never looks.

The version is stamped into the binary at link time, so a build can
always say which one it is: it is on the about dialog, it goes to a
program in a pane as `TERM_PROGRAM_VERSION`, and it goes to an agent
over MCP. A build from a working tree calls itself `dev-<commit>`, and
`dev-<commit>-dirty` when the tree had changes that are in no commit.

`make release` does the same thing by hand -- see
[BUILDING.md](BUILDING.md).

## The signing key

CI signs `SHA256SUMS` with `make sign`, which takes the private key
from the repository's `GUNIM_SIGN_KEY` secret. Its public key is
`ReleaseKey` in `internal/build/build.go`. To sign by hand, after `make release`:

```
GUNIM_SIGN_KEY=$(cat kakel.key) make sign
```

Never commit the private key. Lose it, and installed copies can take no
more updates: their users must download the next release by hand. To
change it, make a new pair with
`go run github.com/marrasen/gunim/tools/gunimsign -keygen new.key`,
put the new public key in `internal/build/build.go`, and sign that one release
with the old key; every release after it is signed with the new one.
Then replace the secret with `gh secret set GUNIM_SIGN_KEY < new.key`.

### The web client's signature

From the first release that shares windows with a link, both programs
hold the web client a window's guests open. A relay takes it only with a
signature by the release key. CI makes that signature as it builds, from
the same secret. By hand, give both commands the key:

```
GUNIM_SIGN_KEY=$(cat kakel.key) make release VERSION=v0.1.0
GUNIM_SIGN_KEY=$(cat kakel.key) make sign
```

`make sign` looks inside the finished programs. It fails, and nothing is
published, when one holds a web client that is not signed with
`ReleaseKey`, or they hold different ones. For a program that holds none
it warns, until kakel shares windows.

gunimsign makes the signature, and is the only program given the key.
What it signs is `client.manifest`: the list of the client's files under
a line that says what it is, so the signature of a `SHA256SUMS` is never
one of a web client, nor the other way round.

The key a relay looks for is the `ReleaseKey` it was built with. A relay
takes the web client of any release signed with that key, older or newer
than itself. Changing the key splits them:

- **A relay from before the change** turns away the web clients of
  releases after it. Update the relay.
- **A relay from after the change** turns away the web clients of
  releases before it. Update the kakel that shares.

The release that changes the key is signed with both. Its `SHA256SUMS`
is signed with the old key, which installed copies check. Its web client
is signed with the new one, which its own relays check. Before its tag:

```
gh secret set GUNIM_CLIENT_KEY < new.key
```

and after it, once `GUNIM_SIGN_KEY` is the new key:

```
gh secret delete GUNIM_CLIENT_KEY
```

`GUNIM_CLIENT_KEY` is used in place of `GUNIM_SIGN_KEY` for the web
client alone, whenever it is set. Tell whoever runs a relay to update it.

## What is not automated

- **macOS.** There is no build and no runner.
- **arm64.** Both releases are amd64.
- **Code signing.** Neither binary carries an Authenticode signature,
  so Windows will warn about an unknown publisher. The signature above
  is only for kakel's own updates.
