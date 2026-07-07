SHELL := /bin/sh

EXT_DIR := editors/vscode
EXT_NAME := bcl-vscode
EXT_PUBLISHER := oarkflow
EXT_VERSION := 0.1.0
EXT_ID := $(EXT_PUBLISHER).$(EXT_NAME)-$(EXT_VERSION)
GOOS := $(shell go env GOOS)
GOARCH := $(shell go env GOARCH)
LSP_BIN_DIR := $(EXT_DIR)/bin/$(GOOS)-$(GOARCH)
LSP_BIN := $(LSP_BIN_DIR)/bcl-lsp
ifeq ($(GOOS),windows)
LSP_BIN := $(LSP_BIN_DIR)/bcl-lsp.exe
endif

VSCODE_EXTENSIONS_DIR ?= $(HOME)/.vscode/extensions
VSCODE_EXTENSION_LINK := $(VSCODE_EXTENSIONS_DIR)/$(EXT_ID)

.PHONY: vscode-extension vscode-extension-deps vscode-extension-lsp vscode-extension-install vscode-extension-reload install-extension reload-vscode bench clean-extension-bin

vscode-extension: vscode-extension-deps vscode-extension-lsp
	cd $(EXT_DIR) && npm run compile

vscode-extension-deps:
	cd $(EXT_DIR) && npm install

vscode-extension-lsp:
	mkdir -p $(LSP_BIN_DIR)
	go build -o $(LSP_BIN) ./cmd/bcl-lsp

vscode-extension-install: vscode-extension
	mkdir -p $(VSCODE_EXTENSIONS_DIR)
	rm -rf $(VSCODE_EXTENSION_LINK)
	ln -s $(CURDIR)/$(EXT_DIR) $(VSCODE_EXTENSION_LINK)
	@echo "Installed $(EXT_ID) -> $(VSCODE_EXTENSION_LINK)"

vscode-extension-reload:
	code --reuse-window $(CURDIR)
	sleep 1
ifeq ($(GOOS),darwin)
	@osascript -e 'tell application "Visual Studio Code" to activate' >/dev/null 2>&1 || true
	@osascript -e 'tell application "System Events" to tell process "Code" to keystroke "r" using {command down}' >/dev/null 2>&1 || echo "VS Code reload could not be automated. Run 'Developer: Reload Window' from the Command Palette."
else
	@code --command workbench.action.reloadWindow >/dev/null 2>&1 || echo "VS Code reload could not be automated. Run 'Developer: Reload Window' from the Command Palette."
endif

install-extension: vscode-extension-install vscode-extension-reload

reload-vscode: vscode-extension-reload

bench:
	GOCACHE=/private/tmp/bcl-gocache GOMODCACHE=/private/tmp/bcl-gomodcache go test -run '^$$' -bench . -benchmem -count=1

clean-extension-bin:
	rm -rf $(EXT_DIR)/bin
