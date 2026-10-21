# GNU Make selects this file automatically on Windows, including Git Bash.
SHELL := cmd.exe
.SHELLFLAGS := /d /c
GO ?= go
NPM ?= npm.cmd
NODE ?= node
CC = gcc
WINDRES ?= windres
GOARCH ?= amd64
VITE_PORT ?= 9245
APP_DIR ?= $(LOCALAPPDATA)/Programs/racomi
SHORTCUT_PATH ?= $(APPDATA)/Microsoft/Windows/Start Menu/Programs/racomi.lnk
VERSION ?=
BUILD_NUMBER ?= 1
VERSION_BUMP ?= patch
RELEASE_FLAGS ?=
RELEASE_COMMAND ?= $(NODE) scripts/release.mjs
WINDOWS_COMMAND = powershell.exe -NoProfile -ExecutionPolicy Bypass -File scripts/windows.ps1
WINDOWS_ARGS = -Go "$(GO)" -Npm "$(NPM)" -Node "$(NODE)" -CC "$(CC)" -Windres "$(WINDRES)" -Arch "$(GOARCH)" -AppDir "$(APP_DIR)" -ShortcutPath "$(SHORTCUT_PATH)" -Version "$(VERSION)" -BuildNumber "$(BUILD_NUMBER)" -Port $(VITE_PORT)

.PHONY: help doctor deps bootstrap install uninstall icons frontend frontend-dev dev check test test-install test-release build package dist release release-initial run
help doctor deps bootstrap install uninstall icons frontend frontend-dev dev check test test-install build package dist run:
	$(WINDOWS_COMMAND) -Action $@ $(WINDOWS_ARGS)

test-release:
	$(NODE) --test scripts/release.test.mjs

release:
	$(RELEASE_COMMAND) $(VERSION_BUMP) $(RELEASE_FLAGS)

release-initial:
	$(RELEASE_COMMAND) initial $(RELEASE_FLAGS)
