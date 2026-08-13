#!/bin/sh
# Locate the `sdd` binary and run one hook event with it. macOS and Linux.
#
# This wrapper exists because hooks.json can interpolate only
# ${CLAUDE_PLUGIN_ROOT} — it has no PATH lookup of its own. Naming a static
# path there meant the hooks broke on every plugin upgrade: a new version is a
# new cache directory with no bin/, and nothing re-provisions until someone
# runs /setup again. Resolution belongs at hook time, not install time.
#
# Order is deliberate. The plugin-root copy wins when present so a session
# keeps using the binary /setup actually provisioned, even if a different one
# is earlier on PATH; PATH is the fallback that makes an unprovisioned or
# freshly upgraded install work anyway.
#
# Failing open is the hard requirement (D-0013, D-0014). A hook that exits
# nonzero or writes garbage to stdout degrades every later tool call in the
# session, which is strictly worse than a missed ledger injection or a missed
# guard denial. Every exit below is 0.

set -u

event="${1:-}"
[ -n "$event" ] || exit 0

# 1. The copy /setup places next to the plugin.
if [ -n "${CLAUDE_PLUGIN_ROOT:-}" ] && [ -x "${CLAUDE_PLUGIN_ROOT}/bin/sdd" ]; then
	exec "${CLAUDE_PLUGIN_ROOT}/bin/sdd" hook "$event"
fi

# 2. Whatever the user installed with `go install`.
if command -v sdd >/dev/null 2>&1; then
	exec sdd hook "$event"
fi

# 3. No binary: silent no-op. The session continues without ledger context or
# the reviewer guard, which is the documented behavior for an unprovisioned
# install — not an error to surface on every tool call.
exit 0
