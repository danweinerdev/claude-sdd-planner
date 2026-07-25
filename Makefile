.PHONY: bump-patch bump-minor bump-major test venv clean-venv

VENV := .venv
PYTHON := $(VENV)/bin/python
STAMP := $(VENV)/.requirements-installed

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
