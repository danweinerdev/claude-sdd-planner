.PHONY: bump-patch bump-minor bump-major test \
        build build-release build-all gen-fixtures check-fixtures check-templates clean-build \
        plugins plugins-check

# Python remains only for bump-version.py. The validators, the parity harness,
# PyYAML, and the virtualenv bootstrap are all gone (tasks 5.1 and the Go
# regression port).
PYTHON := python3

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
# day-to-day work uses, since a failure is something you then go and debug.
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
	@rm -f $(SDD).exe
	@echo "built $(SDD)"

# The rm -f above is a Windows footgun guard, not dead code: `go build -o
# .../sdd` produces a file literally named `sdd`, but when make's shell later
# executes `$(SDD)`, Windows PATHEXT resolution prefers a SIBLING `sdd.exe`
# if one exists — so a stale .exe left by an older toolchain silently
# shadows every freshly built binary in every make target that runs $(SDD),
# including the template drift gate. That exact failure shipped: a v2.3.5
# sdd.exe from before the graph-proposal pair check sat in build/ for a week
# while `make test` reported the stale schema copy as clean.

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

# The corpus of fixture roots the regression test in tools/regression runs
# against. It is exercised by `go test ./...` like any other test, so there is
# no separate target: a corpus check you have to remember to run is one that
# eventually is not run.
REGRESSION_FIXTURES := tools/regression/fixtures

# gen-fixtures regenerates the corpus from the rules' own Bad examples. Run it
# after adding or changing a rule; the result is committed.
gen-fixtures:
	@go run ./tools/genfixtures -out $(REGRESSION_FIXTURES)

# check-fixtures fails when the committed corpus no longer matches what the
# rules would generate — the drift that would otherwise let a rule change slip
# past a gate still comparing yesterday's fixtures.
check-fixtures:
	@go run ./tools/genfixtures -out $(REGRESSION_FIXTURES).check >/dev/null
	@diff -r $(REGRESSION_FIXTURES) $(REGRESSION_FIXTURES).check >/dev/null 2>&1 \
		&& { rm -rf $(REGRESSION_FIXTURES).check; echo "fixtures up to date"; } \
		|| { rm -rf $(REGRESSION_FIXTURES).check; \
		     echo "ERROR: committed fixtures differ from the rules' examples."; \
		     echo "Run 'make gen-fixtures' and commit the result."; exit 1; }

# check-templates regenerates every committed template from its schema and
# fails on structural drift (task 6.2). The templates and the schemas state the
# same structure twice, and the drift is otherwise silent: a template can grow
# a heading the schema does not declare, and nothing fails until an author uses
# it and `sdd apply` refuses a document that looked correct.
check-templates: build
	@$(SDD) template --check

clean-build:
	@rm -rf $(BUILD_DIR)
	@echo "Removed $(BUILD_DIR)"

# The Go suite plus the differential gate. Task 3.4 requires the append-only
# ordering check to be machine-enforced rather than trusted, so `make test`
# runs the corpus: SDD154/155/156 fire against real git history built by each
# fixture's SETUP script, and a regression there fails the build.
test: check-templates
	@go test ./...

# --- Portable (OpenCode/Codex) tree --------------------------------------
#
# .codex-plugin/ and .opencode-plugin/ are generated from the canonical
# Claude tree at the repo root by `sdd plugin sync` (see internal/portable) —
# the same content, written once per harness install convention. They are
# committed, because they are the artifacts the other harnesses install, and
# drift-gated: the Go suite's TestCheckClean (run by `make test`) fails when
# either does not match a fresh generation. Never edit them by hand — edit
# the canonical file, its .portable.md variant, or portable-overrides/, then
# `make plugins`.
plugins:
	@go run ./cmd/sdd plugin sync

plugins-check:
	@go run ./cmd/sdd plugin check

# Version bumps are gated on the test suite. `test` runs as a prerequisite, so
# a failing suite aborts before bump-version.py or any git write happens.
bump-patch: test
	$(eval VERSION := $(shell python3 bump-version.py patch))
	@test -n "$(VERSION)" || { echo "ERROR: bump-version.py produced no version — aborting"; exit 1; }
	@go run ./cmd/sdd plugin sync >/dev/null
	@git add .claude-plugin/plugin.json internal/version/version.go .codex-plugin .opencode-plugin && git commit -m "v$(VERSION)" && git tag -a "v$(VERSION)" -m "v$(VERSION)"
	@echo "Bumped to v$(VERSION)"

bump-minor: test
	$(eval VERSION := $(shell python3 bump-version.py minor))
	@test -n "$(VERSION)" || { echo "ERROR: bump-version.py produced no version — aborting"; exit 1; }
	@go run ./cmd/sdd plugin sync >/dev/null
	@git add .claude-plugin/plugin.json internal/version/version.go .codex-plugin .opencode-plugin && git commit -m "v$(VERSION)" && git tag -a "v$(VERSION)" -m "v$(VERSION)"
	@echo "Bumped to v$(VERSION)"

bump-major: test
	$(eval VERSION := $(shell python3 bump-version.py major))
	@test -n "$(VERSION)" || { echo "ERROR: bump-version.py produced no version — aborting"; exit 1; }
	@go run ./cmd/sdd plugin sync >/dev/null
	@git add .claude-plugin/plugin.json internal/version/version.go .codex-plugin .opencode-plugin && git commit -m "v$(VERSION)" && git tag -a "v$(VERSION)" -m "v$(VERSION)"
	@echo "Bumped to v$(VERSION)"
