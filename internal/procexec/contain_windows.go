//go:build windows

package procexec

func platformContainmentSupported() (bool, string) { return true, "" }
