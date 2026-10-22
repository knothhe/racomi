ifeq ($(OS),Windows_NT)
include scripts/windows.mk
else

GO ?= go
NPM ?= npm
NODE ?= node
VITE_PORT ?= 9245
VERSION ?= $(shell $(NODE) -p "require('./frontend/package.json').version")
BUILD_NUMBER ?= 1
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

.PHONY: help doctor deps bootstrap install uninstall icons frontend frontend-dev dev check test test-install test-release build package dist release release-initial run force-native-icons

help:
	@echo "deps          Install Go and frontend dependencies from lockfiles (alias: bootstrap)"
	@echo "doctor        Check the macOS source-build requirements"
	@echo "install       Build and install ~/Applications/racomi.app and ~/.local/bin/racomi"
	@echo "uninstall     Remove the local app and launcher; preserve all user data"
	@echo "icons         Generate macOS icons, appearance catalog and web icon"
	@echo "frontend-dev  Start the Vite development server"
	@echo "dev           Start the desktop shell (requires frontend-dev)"
	@echo "check         Build frontend, check TypeScript, vet and test Go"
	@echo "build         Build the macOS executable in bin/racomi"
	@echo "package       Create the macOS app in bin/racomi.app (local ad-hoc signature)"
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
	sh scripts/install-macos.sh install "$(CURDIR)/bin/racomi.app" "$(APP_DIR)" "$(BIN_DIR)"

uninstall:
	sh scripts/install-macos.sh uninstall "$(CURDIR)/bin/racomi.app" "$(APP_DIR)" "$(BIN_DIR)"

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
	FRONTEND_DEVSERVER_URL=http://127.0.0.1:$(VITE_PORT) $(GO) run -ldflags="-extldflags=-mmacosx-version-min=$(MACOSX_DEPLOYMENT_TARGET)" .

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
	$(NODE) --test scripts/release.test.mjs

build: frontend
	mkdir -p bin
	CGO_ENABLED=1 GOARCH=$(GOARCH) $(GO) build -trimpath -tags production -ldflags="-s -w -extldflags=-mmacosx-version-min=$(MACOSX_DEPLOYMENT_TARGET)" -o bin/racomi .

package: build $(NATIVE_ICON_OUTPUTS)
	sh scripts/package-macos.sh package "$(VERSION)" "$(BUILD_NUMBER)" "$(MACOSX_DEPLOYMENT_TARGET)" "$(GOARCH)"

dist: package
	sh scripts/package-macos.sh release "$(VERSION)" "$(BUILD_NUMBER)" "$(MACOSX_DEPLOYMENT_TARGET)" "$(GOARCH)"

release:
	$(RELEASE_COMMAND) $(VERSION_BUMP) $(RELEASE_FLAGS)

release-initial:
	$(RELEASE_COMMAND) initial $(RELEASE_FLAGS)

run: build
	./bin/racomi

endif
