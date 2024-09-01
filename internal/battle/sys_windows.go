//go:build windows

package battle

import (
	"syscall"
)

func engineSysProcAttr() *syscall.SysProcAttr {
	return &syscall.SysProcAttr{}
}
