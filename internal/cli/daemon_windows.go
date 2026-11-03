//go:build windows

package cli

import "syscall"

const (
	createNewProcessGroup = 0x00000200
	detachedProcess       = 0x00000008
)

// daemonSysProcAttr 让后台内核不依附当前控制台。
func daemonSysProcAttr() *syscall.SysProcAttr {
	return &syscall.SysProcAttr{
		CreationFlags: createNewProcessGroup | detachedProcess,
		HideWindow:    true,
	}
}
