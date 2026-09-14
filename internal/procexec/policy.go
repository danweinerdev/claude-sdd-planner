package procexec

import "time"

// Defaults per Designs/TestSuiteReliability DD-2 and DD-9.
const (
	DefaultTimeout         = 30 * time.Second
	DefaultCleanup         = 5 * time.Second
	DefaultDiagnosticLimit = 64 << 10 // 64 KiB per stream
	DefaultMachineLimit    = 64 << 20 // 64 MiB
)

// Policy is the explicit execution policy for one command. Zero fields take
// the package defaults; every limit is finite.
type Policy struct {
	// Timeout bounds admission plus execution. It is always clipped to the
	// caller's context deadline.
	Timeout time.Duration
	// Cleanup is the additional drain/cleanup allowance after the command is
	// cancelled or exits with pipes still open. Shared across all resources,
	// never restarted per resource.
	Cleanup time.Duration
	// DiagnosticLimit is the retained stderr excerpt size in bytes. Excess is
	// drained and discarded; the result flags truncation.
	DiagnosticLimit int
	// MachineLimit bounds stdout, the machine-consumed stream. Exceeding it
	// cancels the command and returns CauseOverflow.
	MachineLimit int64
	// Dir is the working directory ("" = inherit).
	Dir string
	// Env is the explicit child environment. nil inherits the process
	// environment (production default). As with os/exec, an empty non-nil
	// slice is empty except that Windows adds SYSTEMROOT when not supplied.
	Env []string
}

func (p Policy) withDefaults() Policy {
	if p.Timeout <= 0 {
		p.Timeout = DefaultTimeout
	}
	if p.Cleanup <= 0 {
		p.Cleanup = DefaultCleanup
	}
	if p.DiagnosticLimit <= 0 {
		p.DiagnosticLimit = DefaultDiagnosticLimit
	}
	if p.MachineLimit <= 0 {
		p.MachineLimit = DefaultMachineLimit
	}
	return p
}
