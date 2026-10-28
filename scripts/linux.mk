# GNU Make selects this file automatically on Linux.
GO ?= go
NPM ?= npm
NODE ?= node
PKG_CONFIG ?= pkg-config
VITE_PORT ?= 9245
VERSION ?= $(shell $(NODE) -p "require('./frontend/package.json').version")
BUILD_NUMBER ?= 1
CHANNEL ?= dev
BUILD_EXECUTOR ?= local
SOURCE_COMMIT := $(shell git rev-parse HEAD 2>/dev/null || echo unknown)
SOURCE_DIRTY := $(shell git status --porcelain --untracked-files=normal 2>/dev/null | awk 'END { print (NR == 0 ? "false" : "true") }')
export SOURCE_COMMIT SOURCE_DIRTY
export BUILD_EXECUTOR
RELEASE_COMMAND ?= $(NODE) scripts/release.mjs
VERSION_BUMP ?= patch
RELEASE_FLAGS ?=
BIN_DIR ?= $(HOME)/.local/bin
XDG_DATA_HOME ?= $(HOME)/.local/share
DESKTOP_DIR ?= $(XDG_DATA_HOME)/applications
ICON_DIR ?= $(XDG_DATA_HOME)/icons/hicolor
GOARCH ?= $(shell $(GO) env GOARCH 2>/dev/null)
# Wails defaults to GTK4 and WebKitGTK 6.0; the gtk3 tag selects GTK3 and
# WebKit2GTK 4.1. Resolved lazily so release-only targets need neither.
GTK ?= $(shell ($(PKG_CONFIG) --exists gtk4 webkitgtk-6.0) >/dev/null 2>&1 && echo 4 || echo 3)
GO_TAGS = $(if $(filter 3,$(GTK)),gtk3,)

.PHONY: help doctor deps bootstrap install uninstall icons frontend frontend-dev dev check test test-install test-release build package dist release release-initial release-notes run

help:
	@echo "deps          Install Go and frontend dependencies from lockfiles (alias: bootstrap)"
	@echo "doctor        Check the Linux source-build requirements (GTK=3|4 to force a toolkit)"
	@echo "install       Build and install ~/.local/bin/racomi-dev with a desktop entry by default"
	@echo "uninstall     Remove the selected channel command, desktop entry and icon; preserve all user data"
	@echo "icons         Check the committed application icons used on Linux"
	@echo "frontend-dev  Start the Vite development server"
	@echo "dev           Start the desktop shell (requires frontend-dev)"
	@echo "check         Build frontend, check TypeScript, vet and test Go"
	@echo "build         Build the Linux executable in bin/CHANNEL/racomi"
	@echo "package       Alias of build; the Linux executable needs no bundle"
	@echo "dist          Create a versioned Linux tar.gz and SHA-256 checksum in bin/releases"
	@echo "release       Preview a SemVer upgrade (VERSION_BUMP=patch|minor|major|version; RELEASE_FLAGS=--apply to publish)"
	@echo "release-initial Preview publishing the current version (RELEASE_FLAGS=--apply to publish)"
	@echo "test-install  Check make install and the release installer using temporary directories"
	@echo "test-release  Check the release workflow using temporary Git repositories"
	@echo "run           Build and launch the Linux executable"

doctor:
	GTK="$(GTK)" PKG_CONFIG="$(PKG_CONFIG)" sh scripts/check-build-tools.sh "$(GO)" "$(NPM)"

deps bootstrap: doctor
	$(NPM) ci --prefix frontend
	$(GO) mod download

install: deps
	$(MAKE) build
	CHANNEL=$(CHANNEL) sh scripts/install-linux.sh install "$(CURDIR)/bin/$(CHANNEL)/racomi" "$(BIN_DIR)" "$(DESKTOP_DIR)" "$(ICON_DIR)"

uninstall:
	CHANNEL=$(CHANNEL) sh scripts/install-linux.sh uninstall "$(CURDIR)/bin/$(CHANNEL)/racomi" "$(BIN_DIR)" "$(DESKTOP_DIR)" "$(ICON_DIR)"

# Linux uses the committed PNG icons; regenerating them needs the macOS tools.
icons:
	@test -s assets/appicon.png && test -s assets/dev-icons/app-icon-dev.png && test -s frontend/public/appicon.png
	@echo "Icons are committed; regenerate them on macOS with make icons."

frontend:
	$(NPM) run build --prefix frontend

frontend-dev:
	$(NPM) run dev --prefix frontend -- --port $(VITE_PORT)

dev: frontend
	FRONTEND_DEVSERVER_URL=http://127.0.0.1:$(VITE_PORT) $(GO) run -tags "$(GO_TAGS)" -ldflags="-X racomi/internal/buildinfo.Version=$(VERSION) -X racomi/internal/buildinfo.Number=$(BUILD_NUMBER) -X racomi/internal/buildinfo.Commit=$(SOURCE_COMMIT)" .

check: frontend
	$(NPM) test --prefix frontend
	$(MAKE) test-install
	$(MAKE) test-release
	$(GO) vet -tags "$(GO_TAGS)" ./...
	$(GO) test -tags "$(GO_TAGS)" ./...

test:
	$(GO) test -tags "$(GO_TAGS)" ./internal/...

test-install:
	sh scripts/install-linux.test.sh
	sh scripts/install.test.sh

test-release:
	$(NODE) --test scripts/release.test.mjs scripts/updates.test.mjs scripts/macos-signing.test.mjs

build: frontend
	GO="$(GO)" sh scripts/build-linux.sh "$(CHANNEL)" "$(VERSION)" "$(BUILD_NUMBER)" "$(GOARCH)" "$(GTK)"

package: build

dist: build
	CHANNEL=$(CHANNEL) sh scripts/package-linux.sh "$(VERSION)" "$(GOARCH)"

release:
	$(RELEASE_COMMAND) $(VERSION_BUMP) $(RELEASE_FLAGS)

release-initial:
	$(RELEASE_COMMAND) initial $(RELEASE_FLAGS)

release-notes:
	@$(NODE) scripts/release-notes.mjs "$(RELEASE_REPO)" "$(RELEASE_TAG)"

run: build
	"./bin/$(CHANNEL)/racomi"
