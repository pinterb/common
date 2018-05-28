// +build !linux,!windows,!darwin,!freebsd,!netbsd

package platform

import "runtime"

const (
	OperatingSystem = runtime.GOOS
)
