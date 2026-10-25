ifeq ($(OS),Windows_NT)
include scripts/windows.mk
else ifeq ($(shell uname -s),Linux)
include scripts/linux.mk
else

GO ?= go
NPM ?= npm
NODE ?= node
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
APP_DIR ?= $(HOME)/Applications
BIN_DIR ?= $(HOME)/.local/bin
GOARCH ?= $(shell $(GO) env GOARCH 2>/dev/null)
MACOSX_DEPLOYMENT_TARGET ?= 13.0
export MACOSX_DEPLOYMENT_TARGET
CGO_CFLAGS ?= -O2 -g
export CGO_CFLAGS := $(CGO_CFLAGS) -mmacosx-version-min=$(MACOSX_DEPLOYMENT_TARGET)

NATIVE_ICON_OUTPUTS := build/macos/Assets.car build/appicon.png build/appicon-dark.png build/macos/racomi.icns
NATIVE_ICON_MISSING := $(filter-out $(wildcard $(NATIVE_ICON_OUTPUTS)),$(NATIVE_ICON_OUTPUTS))

.PHONY: help doctor deps bootstrap install uninstall icons frontend frontend-dev dev check test test-install test-release build package dist release release-initial release-notes run force-native-icons

help:
	@echo "deps          Install Go and frontend dependencies from lockfiles (alias: bootstrap)"
	@echo "doctor        Check the macOS source-build requirements"
	@echo "install       Build and install ~/Applications/racomi Dev.app by default and ~/.local/bin/racomi-dev"
	@echo "uninstall     Remove the selected channel app and launcher; preserve all user data"
	@echo "icons         Generate macOS icons, appearance catalog and web icon"
	@echo "frontend-dev  Start the Vite development server"
	@echo "dev           Start the desktop shell (requires frontend-dev)"
	@echo "check         Build frontend, check TypeScript, vet and test Go"
	@echo "build         Build the macOS executable in bin/CHANNEL/racomi"
	@echo "package       Create the macOS app in bin/CHANNEL/ (local ad-hoc signature)"
	@echo "dist          Create a versioned macOS ZIP and SHA-256 checksum in bin/releases"
	@echo "release       Preview a SemVer upgrade (VERSION_BUMP=patch|minor|major|version; RELEASE_FLAGS=--apply to publish)"
	@echo "release-initial Preview publishing the current version (RELEASE_FLAGS=--apply to publish)"
	@echo "test-install  Check install, replacement and uninstall using temporary directories"
	@echo "test-release  Check the release workflow using temporary Git repositories"
	@echo "run           Build and launch the macOS executable"

doctor:
	sh scripts/check-build-tools.sh "$(GO)" "$(NPM)"

deps bootstrap: doctor
	$(NPM) ci --prefix frontend
	$(GO) mod download

install: deps
	$(MAKE) package
	CHANNEL=$(CHANNEL) sh scripts/install-macos.sh install "$(CURDIR)/bin/$(CHANNEL)/$(if $(filter dev,$(CHANNEL)),racomi Dev,racomi).app" "$(APP_DIR)" "$(BIN_DIR)"

uninstall:
	CHANNEL=$(CHANNEL) sh scripts/install-macos.sh uninstall "$(CURDIR)/bin/$(CHANNEL)/$(if $(filter dev,$(CHANNEL)),racomi Dev,racomi).app" "$(APP_DIR)" "$(BIN_DIR)"

icons: $(NATIVE_ICON_OUTPUTS) frontend/public/appicon.png

# One producer owns all native outputs, including under make 3.81 with -j.
# Missing a secondary output must regenerate the catalog and its siblings once.
ifneq ($(strip $(NATIVE_ICON_MISSING)),)
build/macos/Assets.car: force-native-icons
endif

build/macos/Assets.car: build/macos/AppIcon.icon/icon.json build/macos/AppIcon.icon/Assets/racomi.png build/macos/generate-icons.sh
	MACOSX_DEPLOYMENT_TARGET=$(MACOSX_DEPLOYMENT_TARGET) sh build/macos/generate-icons.sh

build/appicon.png build/appicon-dark.png build/macos/racomi.icns: build/macos/Assets.car
	@test -s "$@"

force-native-icons:

frontend/public/appicon.png: build/appicon.png
	mkdir -p frontend/public
	sips -z 128 128 $< --out $@ >/dev/null

frontend: frontend/public/appicon.png
	$(NPM) run build --prefix frontend

frontend-dev: frontend/public/appicon.png
	$(NPM) run dev --prefix frontend -- --port $(VITE_PORT)

dev: frontend
	FRONTEND_DEVSERVER_URL=http://127.0.0.1:$(VITE_PORT) $(GO) run -ldflags="-extldflags=-mmacosx-version-min=$(MACOSX_DEPLOYMENT_TARGET) -X racomi/internal/buildinfo.Version=$(VERSION) -X racomi/internal/buildinfo.Number=$(BUILD_NUMBER) -X racomi/internal/buildinfo.Commit=$(shell git rev-parse HEAD 2>/dev/null || echo unknown)" .

check: frontend
	$(NPM) test --prefix frontend
	$(MAKE) test-install
	$(MAKE) test-release
	$(GO) vet ./...
	$(GO) test -ldflags="-extldflags=-mmacosx-version-min=$(MACOSX_DEPLOYMENT_TARGET)" ./...

test:
	$(GO) test -ldflags="-extldflags=-mmacosx-version-min=$(MACOSX_DEPLOYMENT_TARGET)" ./internal/...

test-install:
	sh scripts/install-macos.test.sh

test-release:
	$(NODE) --test scripts/release.test.mjs scripts/updates.test.mjs scripts/macos-signing.test.mjs

build: frontend
	GO="$(GO)" sh scripts/build-macos.sh "$(CHANNEL)" "$(VERSION)" "$(BUILD_NUMBER)" "$(GOARCH)" "$(MACOSX_DEPLOYMENT_TARGET)"

package: build $(NATIVE_ICON_OUTPUTS)
	@if [ "$(CHANNEL)" = stable ]; then sh scripts/prepare-sparkle.sh; fi
	CHANNEL=$(CHANNEL) sh scripts/package-macos.sh package "$(VERSION)" "$(BUILD_NUMBER)" "$(MACOSX_DEPLOYMENT_TARGET)" "$(GOARCH)"

dist: package
	CHANNEL=$(CHANNEL) sh scripts/package-macos.sh release "$(VERSION)" "$(BUILD_NUMBER)" "$(MACOSX_DEPLOYMENT_TARGET)" "$(GOARCH)"

release:
	$(RELEASE_COMMAND) $(VERSION_BUMP) $(RELEASE_FLAGS)

release-initial:
	$(RELEASE_COMMAND) initial $(RELEASE_FLAGS)

release-notes:
	@$(NODE) scripts/release-notes.mjs "$(RELEASE_REPO)" "$(RELEASE_TAG)"

run: build
	"./bin/$(CHANNEL)/racomi"

endif
