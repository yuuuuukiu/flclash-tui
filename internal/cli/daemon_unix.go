//go:build !windows

package cli

import "syscall"

// daemonSysProcAttr 让后台内核脱离当前终端会话（SIGHUP 不会带走它）。
func daemonSysProcAttr() *syscall.SysProcAttr {
	return &syscall.SysProcAttr{Setsid: true}
}
