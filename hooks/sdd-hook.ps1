# Locate the `sdd` binary and run one hook event with it. Windows.
#
# The PowerShell counterpart to sdd-hook.sh, and the reason this wrapper pair
# is not the shell dependency D-0013 removed: that dependency was a POSIX
# script with no Windows equivalent, so the hook simply did not work there.
# This pair covers both platforms.
#
# Resolution order and the fail-open rule match sdd-hook.sh exactly — see the
# comments there for why. Every path below exits 0.

param([Parameter(Position = 0)][string]$Event)

$ErrorActionPreference = 'SilentlyContinue'

if ([string]::IsNullOrWhiteSpace($Event)) { exit 0 }

# stdin carries the hook payload and must reach the binary intact, so it is
# piped through rather than read here.

# 1. The copy /setup places next to the plugin.
if ($env:CLAUDE_PLUGIN_ROOT) {
    $local = Join-Path $env:CLAUDE_PLUGIN_ROOT 'bin\sdd.exe'
    if (Test-Path -LiteralPath $local -PathType Leaf) {
        $input | & $local hook $Event
        exit 0
    }
}

# 2. Whatever the user installed with `go install`.
$onPath = Get-Command sdd -CommandType Application -ErrorAction SilentlyContinue
if ($onPath) {
    $input | & $onPath.Source hook $Event
    exit 0
}

# 3. No binary: silent no-op, as on POSIX.
exit 0
