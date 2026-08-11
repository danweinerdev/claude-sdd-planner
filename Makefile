.PHONY: bump-patch bump-minor bump-major test venv clean-venv \
        build build-release build-all parity gen-fixtures check-fixtures clean-build

VENV := .venv
PYTHON := $(VENV)/bin/python
STAMP := $(VENV)/.requirements-installed

# --- Go toolchain -----------------------------------------------------------
#
# Binaries land in build/<goos>-<goarch>-<variant>/sdd, never in the repo root.
# The tuple is in the path rather than the filename so a cross-compiled set can
# be built side by side without collisions, and so `sdd` is the name that ends
# up on a user's PATH regardless of platform.
#
# The tuple uses Go's own GOOS-GOARCH spelling — the values `go env` reports and
# `go build` accepts — so no translation table is needed between what the
# Makefile writes and what the toolchain understands.
#
# Two variants, because they are built for different readers:
#
#   debug    Full DWARF and symbol table, untrimmed paths. What you attach a
#            debugger to and what a stack trace names a real file in.
#   release  -trimpath -ldflags="-s -w". This is what CI publishes.
#
# The release flags are not only about size (~32% smaller). Without -trimpath a
# Go binary embeds the absolute path of the tree it was compiled in, so an
# unstripped release would ship a builder's home directory to every user who
# downloads it. Stripping is therefore a correctness property of a published
# artifact, not an optimization, which is why release is the CI default and
# debug is never published.

GOOS ?= $(shell go env GOOS)
GOARCH ?= $(shell go env GOARCH)

BUILD_DIR := build
HOST_TUPLE := $(GOOS)-$(GOARCH)

# Release strip flags: -s drops the symbol table, -w the DWARF sections, and
# -trimpath rewrites embedded source paths to module-relative form.
RELEASE_LDFLAGS := -s -w
RELEASE_FLAGS := -trimpath -ldflags="$(RELEASE_LDFLAGS)"

# The host binaries, one per variant. SDD names the debug build: it is the one
# day-to-day work and the parity harness use, since a failing comparison is
# something you then go and debug.
SDD := $(BUILD_DIR)/$(HOST_TUPLE)-debug/sdd
SDD_RELEASE := $(BUILD_DIR)/$(HOST_TUPLE)-release/sdd

PLATFORMS := \
	linux-amd64 \
	linux-arm64 \
	darwin-amd64 \
	darwin-arm64 \
	windows-amd64

# build compiles the host platform, debug variant.
build:
	@mkdir -p $(dir $(SDD))
	@go build -o $(SDD) ./cmd/sdd
	@echo "built $(SDD)"

# build-release compiles the host platform with the published flags. Useful for
# reproducing a CI artifact locally without cross-compiling the whole set.
build-release:
	@mkdir -p $(dir $(SDD_RELEASE))
	@CGO_ENABLED=0 go build $(RELEASE_FLAGS) -o $(SDD_RELEASE) ./cmd/sdd
	@echo "built $(SDD_RELEASE)"

# build-all cross-compiles every platform in both variants. Pure Go with no
# cgo, so these build from any host without a cross toolchain.
#
# VARIANTS may be narrowed on the command line — CI publishes with
# `make build-all VARIANTS=release` and has no use for the debug half.
VARIANTS ?= debug release

build-all:
	@for tuple in $(PLATFORMS); do \
		goos=$${tuple%-*}; goarch=$${tuple#*-}; \
		for variant in $(VARIANTS); do \
			out=$(BUILD_DIR)/$$tuple-$$variant/sdd; \
			case $$tuple in windows-*) out=$$out.exe;; esac; \
			mkdir -p $$(dirname $$out); \
			if [ "$$variant" = release ]; then \
				CGO_ENABLED=0 GOOS=$$goos GOARCH=$$goarch \
					go build -trimpath -ldflags="$(RELEASE_LDFLAGS)" \
					-o $$out ./cmd/sdd || exit 1; \
			else \
				CGO_ENABLED=0 GOOS=$$goos GOARCH=$$goarch \
					go build -o $$out ./cmd/sdd || exit 1; \
			fi; \
			echo "built $$out"; \
		done; \
	done

# parity runs the differential oracle against the host debug binary, building
# it first so the comparison never runs against a stale artifact. Override the
# roots with `make parity PARITY_ROOTS="path1 path2"`. Exits non-zero on a
# mismatch, which is what makes it usable as a CI gate.
#
# The fixture corpus is the input that gives the gate teeth: run against a
# healthy planning root alone, both validators agree on zero diagnostics and
# the comparison exercises none of the ported rules.
PARITY_FIXTURES := tools/parity/fixtures
PARITY_MANIFEST := $(PARITY_FIXTURES)/MANIFEST
PARITY_ROOTS ?= .plans

PARITY_ALLOW := tools/parity/allow-missing.txt
PARITY_ALLOW_TEXT := tools/parity/allow-message-drift.txt

parity: build $(STAMP)
	@$(PYTHON) tools/parity/parity.py $(PARITY_ROOTS) \
		--manifest $(PARITY_MANIFEST) --allow $(PARITY_ALLOW) \
		--allow-message-drift $(PARITY_ALLOW_TEXT) --binary $(SDD)

# gen-fixtures regenerates the corpus from the rules' own Bad examples. Run it
# after adding or changing a rule; the result is committed.
gen-fixtures:
	@go run ./tools/genfixtures -out $(PARITY_FIXTURES)

# check-fixtures fails when the committed corpus no longer matches what the
# rules would generate — the drift that would otherwise let a rule change slip
# past a gate still comparing yesterday's fixtures.
check-fixtures:
	@go run ./tools/genfixtures -out $(PARITY_FIXTURES).check >/dev/null
	@diff -r $(PARITY_FIXTURES) $(PARITY_FIXTURES).check >/dev/null 2>&1 \
		&& { rm -rf $(PARITY_FIXTURES).check; echo "fixtures up to date"; } \
		|| { rm -rf $(PARITY_FIXTURES).check; \
		     echo "ERROR: committed fixtures differ from the rules' examples."; \
		     echo "Run 'make gen-fixtures' and commit the result."; exit 1; }

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
