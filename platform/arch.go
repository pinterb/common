// +build !386,!amd64,!arm

package platform

import "runtime"

const (
	Architecture = runtime.GOARCH
)
