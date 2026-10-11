GO ?= go
NPM ?= npm
VITE_PORT ?= 9245
MACOSX_DEPLOYMENT_TARGET ?= 13.0
export MACOSX_DEPLOYMENT_TARGET
CGO_CFLAGS ?= -O2 -g
export CGO_CFLAGS := $(CGO_CFLAGS) -mmacosx-version-min=$(MACOSX_DEPLOYMENT_TARGET)

NATIVE_ICON_OUTPUTS := build/macos/Assets.car build/appicon.png build/appicon-dark.png build/macos/racomi.icns
NATIVE_ICON_MISSING := $(filter-out $(wildcard $(NATIVE_ICON_OUTPUTS)),$(NATIVE_ICON_OUTPUTS))

.PHONY: help install icons frontend frontend-dev dev check test build package run force-native-icons

help:
	@echo "install       Install dependencies from lockfiles"
	@echo "icons         Generate macOS icons, appearance catalog and web icon"
	@echo "frontend-dev  Start the Vite development server"
	@echo "dev           Start the desktop shell (requires frontend-dev)"
	@echo "check         Build frontend, check TypeScript, vet and test Go"
	@echo "build         Build the macOS executable in bin/racomi"
	@echo "package       Create the unsigned macOS app in bin/racomi.app"
	@echo "run           Build and launch the macOS executable"

install:
	$(NPM) ci --prefix frontend
	$(GO) mod download

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
	$(GO) vet ./...
	$(GO) test -ldflags="-extldflags=-mmacosx-version-min=$(MACOSX_DEPLOYMENT_TARGET)" ./...

test:
	$(GO) test -ldflags="-extldflags=-mmacosx-version-min=$(MACOSX_DEPLOYMENT_TARGET)" ./internal/...

build: frontend
	mkdir -p bin
	CGO_ENABLED=1 $(GO) build -trimpath -tags production -ldflags="-s -w -extldflags=-mmacosx-version-min=$(MACOSX_DEPLOYMENT_TARGET)" -o bin/racomi .

package: build $(NATIVE_ICON_OUTPUTS)
	mkdir -p bin/racomi.app/Contents/MacOS bin/racomi.app/Contents/Resources
	cp bin/racomi bin/racomi.app/Contents/MacOS/racomi
	cp build/macos/Info.plist bin/racomi.app/Contents/Info.plist
	cp build/macos/racomi.icns bin/racomi.app/Contents/Resources/racomi.icns
	cp build/macos/Assets.car bin/racomi.app/Contents/Resources/Assets.car
	cp build/appicon.png bin/racomi.app/Contents/Resources/NSApplicationIcon.png
	touch bin/racomi.app

run: build
	./bin/racomi
