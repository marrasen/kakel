# Local build helpers, and what a release is made of.
#
# Every build is pure Go. No C toolchain, no development headers, and no
# platform can only be built on itself: cgo is off everywhere, so both
# releases cross-compile from either machine.

export CGO_ENABLED = 0

GO_WIN = GOOS=windows GOARCH=amd64 go

# VERSION is what the build calls itself. A tag when there is one, the
# commit when there is not, so a binary handed to somebody can always be
# got back to. CI passes the tag it is building.
VERSION ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo dev)
STAMP    = -X github.com/marrasen/kakel/internal/build.version=$(VERSION)

# -trimpath keeps the building machine's directory layout out of the
# binary, so two people building the same tag get the same bytes.
BUILD = go build -trimpath -tags webclient -ldflags "$(STAMP)"
DIST  = dist

# gunimsign signs, and is the only one given the private key: what builds
# and checks is not. The key is in its environment, where make prints
# nothing of it. make gives a target's own variables to what it depends
# on as well, so what is made ahead of a signature drops it itself.
unexport GUNIM_SIGN_KEY
KEYLESS = unset GUNIM_SIGN_KEY;
sign-client sign-sums: export GUNIM_SIGN_KEY := $(value GUNIM_SIGN_KEY)
GUNIMSIGN = go run github.com/marrasen/gunim/tools/gunimsign
CLIENT    = webbundle/client

.PHONY: all test vet fmt icon client build-client sign-client windows linux release sign check-release sign-sums clean
all: test windows

# The whole suite runs without a display: the window's tests draw into
# gunim's offscreen window.
test:
	go test ./...

vet:
	GOOS=windows GOARCH=amd64 CGO_ENABLED=0 go vet ./...
	# session has Unix files the Windows build never sees.
	GOOS=linux GOARCH=amd64 CGO_ENABLED=0 go vet ./session/...

fmt:
	gofmt -l .

# -H=windowsgui links the Windows build as a windowed program: started
# from a shortcut, Windows gives it no console window, which would flash
# up before the program let it go.
windows:
	$(GO_WIN) build -ldflags "-H=windowsgui" -o kakel.exe .

linux:
	GOOS=linux GOARCH=amd64 go build -o kakel-linux .

# The executable's own icon, for Explorer and a pinned shortcut. Only
# needed when the drawing changes, and a test fails when it has changed
# and this has not been run. See "The icon" in the README.
icon:
	go run github.com/akavel/rsrc@v0.10.2 -ico "$$(go run ./internal/mkico)" -arch amd64 -o rsrc_windows_amd64.syso

# The web client a host gives a relay for the guests of a shared window,
# into webbundle/client, where a build with the tag webclient embeds it
# from. It is signed when GUNIM_SIGN_KEY is set, and make warns when it is
# not. See RELEASING.md.
client: sign-client
	go run ./internal/mkclient -signed

build-client:
	$(KEYLESS) go run ./internal/mkclient

# The signature of the client's manifest, when there is a key.
sign-client: | build-client
	@if [ -n "$$GUNIM_SIGN_KEY" ]; then echo "$(GUNIMSIGN) $(CLIENT)/client.manifest"; $(GUNIMSIGN) $(CLIENT)/client.manifest; fi

# Everything a release ships, into dist/. Run by CI on a tag, and by
# hand to see what a release would contain.
#
# Both cross-compile, so this makes a whole release wherever it is run.
# The web client is built once, and the same one embedded in both.
release: clean client
	mkdir -p $(DIST)
	$(GO_WIN) build -trimpath -tags webclient -ldflags "$(STAMP) -H=windowsgui" -o $(DIST)/kakel.exe .
	cd $(DIST) && zip -q kakel_$(VERSION)_windows_amd64.zip kakel.exe
	rm $(DIST)/kakel.exe
	GOOS=linux GOARCH=amd64 $(BUILD) -o $(DIST)/kakel .
	cd $(DIST) && tar czf kakel_$(VERSION)_linux_amd64.tar.gz kakel
	rm $(DIST)/kakel
	cd $(DIST) && sha256sum * > SHA256SUMS
	cat $(DIST)/SHA256SUMS
	@test -s $(CLIENT)/client.manifest.sig || echo "WARNING: the web client in this release is NOT SIGNED. No relay takes it without -allow-unsigned." >&2

# The signature of a release's SHA256SUMS, which installed copies check
# before they run an update. It takes the private key from
# GUNIM_SIGN_KEY, and fails without it. It fails too when the programs in
# dist/ hold a web client that is not signed with the release key, as no
# relay would take it. See RELEASING.md.
sign: check-release sign-sums

check-release:
	$(KEYLESS) go run ./internal/mkclient -check $(DIST)/kakel_*

sign-sums: | check-release
	$(GUNIMSIGN) $(DIST)/SHA256SUMS

clean:
	rm -rf $(DIST)
