.PHONY: bump-patch bump-minor bump-major test venv clean-venv \
        build build-all parity clean-build

VENV := .venv
PYTHON := $(VENV)/bin/python
STAMP := $(VENV)/.requirements-installed

# --- Go toolchain -----------------------------------------------------------
#
# Binaries land in build/<goos>-<goarch>/sdd, never in the repo root. The tuple
# is in the path rather than the filename so a cross-compiled set can be built
# side by side without collisions, and so `sdd` is the name that ends up on a
# user's PATH regardless of platform.
#
# The tuple uses Go's own GOOS-GOARCH spelling — the values `go env` reports and
# `go build` accepts — so no translation table is needed between what the
# Makefile writes and what the toolchain understands.

GOOS ?= $(shell go env GOOS)
GOARCH ?= $(shell go env GOARCH)

BUILD_DIR := build
HOST_TUPLE := $(GOOS)-$(GOARCH)
SDD := $(BUILD_DIR)/$(HOST_TUPLE)/sdd

# Release targets. Windows is listed with its .exe suffix handled below; the
# rest share the bare `sdd` name.
PLATFORMS := \
	linux-amd64 \
	linux-arm64 \
	darwin-amd64 \
	darwin-arm64 \
	windows-amd64

# binary-name returns the output filename for a tuple: Windows needs .exe, and
# `go build` will not infer it when -o names an explicit path.
binary-name = $(if $(filter windows-%,$(1)),sdd.exe,sdd)

# build compiles for the host platform. This is the target the parity harness
# and day-to-day work use.
build:
	@mkdir -p $(BUILD_DIR)/$(HOST_TUPLE)
	@go build -o $(SDD) ./cmd/sdd
	@echo "built $(SDD)"

# build-all cross-compiles every release platform. Pure-Go with no cgo, so
# these build from any host without a cross toolchain.
build-all:
	@for tuple in $(PLATFORMS); do \
		goos=$${tuple%-*}; goarch=$${tuple#*-}; \
		out=$(BUILD_DIR)/$$tuple/sdd; \
		case $$tuple in windows-*) out=$$out.exe;; esac; \
		mkdir -p $(BUILD_DIR)/$$tuple; \
		CGO_ENABLED=0 GOOS=$$goos GOARCH=$$goarch go build -o $$out ./cmd/sdd \
			|| exit 1; \
		echo "built $$out"; \
	done

# parity runs the differential oracle against the host binary, building it
# first so the comparison never runs against a stale artifact. Override the
# roots with `make parity ROOTS="path1 path2"`. Exits non-zero on a mismatch,
# which is what makes it usable as a CI gate.
PARITY_ROOTS ?= .plans
parity: build $(STAMP)
	@$(PYTHON) tools/parity/parity.py $(PARITY_ROOTS) --binary $(SDD)

clean-build:
	@rm -rf $(BUILD_DIR)
	@echo "Removed $(BUILD_DIR)"

# Create the virtualenv and install requirements. The stamp file is keyed to
# requirements.txt, so edits to the deps trigger a reinstall on the next run.
$(STAMP): requirements.txt
	@test -d $(VENV) || { echo "Creating virtualenv in $(VENV)..."; python3 -m venv $(VENV); }
	@$(PYTHON) -m pip install --quiet --upgrade pip
	@$(PYTHON) -m pip install --quiet -r requirements.txt
	@touch $(STAMP)
	@echo "Dependencies installed in $(VENV)"

venv: $(STAMP)

test: $(STAMP)
	@$(PYTHON) -m unittest discover -s tests -v

clean-venv:
	@rm -rf $(VENV)
	@echo "Removed $(VENV)"

# Version bumps are gated on the test suite. `test` runs as a prerequisite, so
# a failing suite aborts before bump-version.py or any git write happens.
bump-patch: test
	$(eval VERSION := $(shell python3 bump-version.py patch))
	@test -n "$(VERSION)" || { echo "ERROR: bump-version.py produced no version — aborting"; exit 1; }
	@git add .claude-plugin/plugin.json && git commit -m "v$(VERSION)" && git tag -a "v$(VERSION)" -m "v$(VERSION)"
	@echo "Bumped to v$(VERSION)"

bump-minor: test
	$(eval VERSION := $(shell python3 bump-version.py minor))
	@test -n "$(VERSION)" || { echo "ERROR: bump-version.py produced no version — aborting"; exit 1; }
	@git add .claude-plugin/plugin.json && git commit -m "v$(VERSION)" && git tag -a "v$(VERSION)" -m "v$(VERSION)"
	@echo "Bumped to v$(VERSION)"

bump-major: test
	$(eval VERSION := $(shell python3 bump-version.py major))
	@test -n "$(VERSION)" || { echo "ERROR: bump-version.py produced no version — aborting"; exit 1; }
	@git add .claude-plugin/plugin.json && git commit -m "v$(VERSION)" && git tag -a "v$(VERSION)" -m "v$(VERSION)"
	@echo "Bumped to v$(VERSION)"
