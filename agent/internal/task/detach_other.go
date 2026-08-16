//go:build !linux

package task

import "syscall"

func detachAttr() *syscall.SysProcAttr {
	return nil
}
