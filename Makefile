.PHONY: build run test lint clean dist dist-all checksums version

# VERSION is what the binaries report and what the archive is named after.
#
# From the git tags, so a local `make dist` produces the same name the
# release workflow would for the same commit — "v0.4.2" on the tag itself,
# "v0.4.2-3-gabc1234" three commits later, with "-dirty" when the tree has
# uncommitted work in it. Override it (make dist VERSION=v1.0.0-rc1) when
# you are staging something the tags do not know about yet.
VERSION ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo dev)
COMMIT  ?= $(shell git rev-parse HEAD 2>/dev/null)

BINARY_NAME = tele-tui
BUILD_DIR   = bin
DIST_DIR    = dist

MODULE  = github.com/imtaqin/telegram-cli
LDFLAGS = -s -w \
	-X $(MODULE)/internal/version.Version=$(VERSION) \
	-X $(MODULE)/internal/version.Commit=$(COMMIT)

# What ships beside the binaries.
#
# config.example.toml is in here because it is the reference for every
# setting — the file the README points at, and the one a new user needs most
# after the binary itself. An archive carrying the program but not the
# document describing how to configure it sends the reader off to find the
# repository, which is what downloading a release was meant to avoid.
#
# docs/ for the same reason, since the README was split: the keymap, the
# settings and the troubleshooting notes moved out of it, and an archive
# carrying only the front door would be a smaller manual than the one the
# previous release shipped.
#
# The WHOLE directory, design record and fixtures included, rather than the
# six user-facing files. Those files link to the design record and the
# goldens, so a subset would ship with dead links — and all of it is 340K
# against a 19M binary, which is not a trade worth making.
DIST_DOCS = README.md LICENSE config.example.toml
DIST_DIRS = docs

GO_BUILD = CGO_ENABLED=0 go build -trimpath -ldflags="$(LDFLAGS)"

build:
	@mkdir -p $(BUILD_DIR)
	$(GO_BUILD) -o $(BUILD_DIR)/tele-tui ./cmd/teletui
	$(GO_BUILD) -o $(BUILD_DIR)/telegram-mcp ./cmd/telegram-mcp
	$(GO_BUILD) -o $(BUILD_DIR)/telegram-api ./cmd/telegram-api
	@echo "Built $(VERSION): $(BUILD_DIR)/tele-tui $(BUILD_DIR)/telegram-mcp $(BUILD_DIR)/telegram-api"

run: build
	./$(BUILD_DIR)/$(BINARY_NAME)

test:
	go test ./...

lint:
	golangci-lint run ./...

# version is what the release would call this commit. Printed rather than
# guessed at: the workflow and a person staging a build should be able to
# ask the same question and get the same answer.
version:
	@echo $(VERSION)

clean:
	rm -rf $(BUILD_DIR) $(DIST_DIR)

# dist packages one platform's three binaries and the docs into the archive
# the release publishes.
#
#   make dist                            the machine you are on
#   make dist GOOS=linux GOARCH=arm64    somewhere else
#
# The release workflow calls this rather than repeating it. A second
# packaging path is a second thing to keep in step, and the one nobody runs
# by hand is the one that rots — which is how config.example.toml came to be
# missing from every archive ever published.
GOOS   ?= $(shell go env GOOS)
GOARCH ?= $(shell go env GOARCH)

dist:
	@set -e; \
	ext=""; archive="tar.gz"; \
	if [ "$(GOOS)" = "windows" ]; then ext=".exe"; archive="zip"; fi; \
	pkg="telegram-cli_$(VERSION)_$(GOOS)_$(GOARCH)"; \
	stage="$(DIST_DIR)/stage/$$pkg"; \
	rm -rf "$$stage"; mkdir -p "$$stage"; \
	for cmd in teletui telegram-mcp telegram-api; do \
		out="$$cmd"; [ "$$cmd" = "teletui" ] && out="tele-tui"; \
		GOOS=$(GOOS) GOARCH=$(GOARCH) $(GO_BUILD) -o "$$stage/$$out$$ext" ./cmd/$$cmd; \
	done; \
	cp $(DIST_DOCS) "$$stage/"; \
	for d in $(DIST_DIRS); do cp -R "$$d" "$$stage/"; done; \
	mkdir -p $(DIST_DIR); \
	if [ "$$archive" = "zip" ]; then \
		( cd $(DIST_DIR)/stage && zip -qr "../$$pkg.zip" "$$pkg" ); \
	else \
		tar -czf "$(DIST_DIR)/$$pkg.tar.gz" -C $(DIST_DIR)/stage "$$pkg"; \
	fi; \
	rm -rf "$(DIST_DIR)/stage"; \
	echo "Packaged $(DIST_DIR)/$$pkg.$$archive"

# dist-all builds every platform the release publishes, so a packaging
# change can be checked against all of them before it is pushed rather than
# after a tag has already gone out.
DIST_PLATFORMS = \
	linux/amd64 linux/arm64 \
	darwin/amd64 darwin/arm64 \
	windows/amd64 \
	android/arm64

dist-all:
	@for platform in $(DIST_PLATFORMS); do \
		$(MAKE) --no-print-directory dist \
			GOOS=$${platform%/*} GOARCH=$${platform#*/} VERSION=$(VERSION); \
	done

# checksums is what a downloader verifies against. Written last, over
# whatever is in dist/, so it cannot describe an archive that is not there.
#
# sha256sum on Linux, shasum on macOS: the release runs on one and a person
# staging a build is usually on the other, and a target that only works on
# the CI runner is a target nobody checks before pushing.
checksums:
	@cd $(DIST_DIR) && \
		{ command -v sha256sum >/dev/null && sha256sum *.tar.gz *.zip 2>/dev/null \
		  || shasum -a 256 *.tar.gz *.zip 2>/dev/null; } > checksums.txt && \
		echo "Wrote $(DIST_DIR)/checksums.txt"
