BIN  := status
DIST := dist

# VERSION identifies the build and names the artifact. A tagged commit gives the
# tag, anything else the short hash, plus -dirty when the tree has uncommitted
# changes — so an artifact can always be traced back to a tree.
VERSION ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo dev)

GOOS   ?= $(shell go env GOOS)
GOARCH ?= $(shell go env GOARCH)

# Release builds are stripped and path-trimmed: -s -w drops the symbol and DWARF
# tables, which is about a third of the binary, and -trimpath keeps local
# filesystem paths out of it.
RELEASE_FLAGS := -trimpath -ldflags "-s -w -X main.version=$(VERSION)"

PKG     := $(BIN)_$(VERSION)_$(GOOS)_$(GOARCH)
TARBALL := $(DIST)/$(PKG).tar.gz

# Platforms dist-all packages. Cross-compiled builds have cgo disabled, since
# there is no cross toolchain here; the only consequence is that the DNS check's
# fallback to the platform resolver is Go's own resolver on those binaries.
PLATFORMS := darwin/arm64 darwin/amd64 linux/amd64 linux/arm64

# sha256sum on Linux, shasum on macOS.
SHA256 := $(shell command -v sha256sum >/dev/null 2>&1 && echo sha256sum || echo 'shasum -a 256')

.PHONY: build run test race vet lint dist dist-all clean

build:
	go build -o $(BIN) .

run: build
	./$(BIN)

test:
	go test ./...

race:
	go test -race ./...

vet:
	go vet ./...

# Everything CI should gate on.
lint: vet
	gofmt -l . | tee /dev/stderr | (! read)

# dist builds a release binary for one platform and packages it, with the README
# and a checksum, as dist/$(BIN)_<version>_<os>_<arch>.tar.gz. Override GOOS and
# GOARCH to target another platform:
#
#	make dist GOOS=linux GOARCH=amd64
#
# The tarball unpacks into its own directory rather than scattering files into
# the current one.
dist:
	@rm -rf $(DIST)/$(PKG)
	@mkdir -p $(DIST)/$(PKG)
	GOOS=$(GOOS) GOARCH=$(GOARCH) go build $(RELEASE_FLAGS) -o $(DIST)/$(PKG)/$(BIN) .
	@cp README.md $(DIST)/$(PKG)/
	@tar -czf $(TARBALL) -C $(DIST) $(PKG)
	@rm -rf $(DIST)/$(PKG)
	@cd $(DIST) && $(SHA256) $(PKG).tar.gz > $(PKG).tar.gz.sha256
	@echo "$(TARBALL) ($$(du -h $(TARBALL) | cut -f1))"

# dist-all packages every platform in PLATFORMS. VERSION is passed down so all
# the artifacts of one run carry the same identity.
dist-all:
	@for p in $(PLATFORMS); do \
		$(MAKE) --no-print-directory dist VERSION=$(VERSION) GOOS=$${p%/*} GOARCH=$${p#*/} || exit 1; \
	done

clean:
	rm -f $(BIN)
	rm -rf $(DIST)
