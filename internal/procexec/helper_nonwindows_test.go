//go:build !windows

package procexec

func runHandleProbe() int { return 0 }

func runNestedProbe() int { return 0 }
