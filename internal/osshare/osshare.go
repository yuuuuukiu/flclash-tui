// Package osshare 系统代理开关（当前实现 macOS networksetup；其他平台返回不支持）。
package osshare

import (
	"errors"
	"os/exec"
	"runtime"
	"strconv"
	"strings"

	"flclash-tui/internal/settings"
)

// Supported 报告当前平台是否支持系统代理设置。
func Supported() bool { return runtime.GOOS == "darwin" }

// CurrentStatus 读取设置并查询实际状态（尽力而为）。
func CurrentStatus(s *settings.Settings) bool {
	if !Supported() {
		return false
	}
	out, err := exec.Command("networksetup", "-getwebproxy", s.SystemProxyService).Output()
	if err != nil {
		return false
	}
	return strings.Contains(string(out), "Enabled: Yes")
}

// Enable 开启系统 HTTP/HTTPS 代理。
func Enable(s *settings.Settings) error {
	if !Supported() {
		return errors.New("当前平台暂不支持系统代理设置")
	}
	host := "127.0.0.1"
	port := strconv.Itoa(s.MixedPort)
	for _, args := range [][]string{
		{"-setwebproxy", s.SystemProxyService, host, port},
		{"-setsecurewebproxy", s.SystemProxyService, host, port},
		{"-setsocksfirewallproxy", s.SystemProxyService, host, port},
		{"-setwebproxystate", s.SystemProxyService, "on"},
		{"-setsecurewebproxystate", s.SystemProxyService, "on"},
		{"-setsocksfirewallproxystate", s.SystemProxyService, "on"},
	} {
		if out, err := exec.Command("networksetup", args...).CombinedOutput(); err != nil {
			return errors.New(strings.TrimSpace(string(out)))
		}
	}
	return nil
}

// Disable 关闭系统代理。
func Disable(s *settings.Settings) error {
	if !Supported() {
		return errors.New("当前平台暂不支持系统代理设置")
	}
	for _, args := range [][]string{
		{"-setwebproxystate", s.SystemProxyService, "off"},
		{"-setsecurewebproxystate", s.SystemProxyService, "off"},
		{"-setsocksfirewallproxystate", s.SystemProxyService, "off"},
	} {
		if out, err := exec.Command("networksetup", args...).CombinedOutput(); err != nil {
			return errors.New(strings.TrimSpace(string(out)))
		}
	}
	return nil
}
