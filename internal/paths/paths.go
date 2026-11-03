// Package paths 管理应用的目录布局与文件路径。
//
// 目录布局（默认在 ~/.config/flclash-tui/，可用 FLCLASH_TUI_HOME 覆盖）：
//
//	settings.json      应用设置
//	profiles.json      订阅元数据
//	profiles/          订阅原始 YAML（与 FLClash 相同的存储方式）
//	core/              mihomo 工作目录（config.yaml、cache.db、geo 数据）
//	core/bin/          内置内核释放目录（mihomo 二进制）
package paths

import (
	"os"
	"path/filepath"
	"runtime"
)

// Home 返回应用数据根目录。
func Home() string {
	if v := os.Getenv("FLCLASH_TUI_HOME"); v != "" {
		return v
	}
	base, err := os.UserConfigDir()
	if err != nil {
		home, _ := os.UserHomeDir()
		base = filepath.Join(home, ".config")
	}
	return filepath.Join(base, "flclash-tui")
}

func EnsureHome() error {
	for _, dir := range []string{Home(), ProfilesDir(), CoreDir(), CoreBinDir()} {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return err
		}
	}
	return nil
}

func SettingsFile() string   { return filepath.Join(Home(), "settings.json") }
func ProfilesFile() string   { return filepath.Join(Home(), "profiles.json") }
func ProfilesDir() string    { return filepath.Join(Home(), "profiles") }
func CoreDir() string        { return filepath.Join(Home(), "core") }
func CoreConfigFile() string { return filepath.Join(CoreDir(), "config.yaml") }
func CoreLogFile() string    { return filepath.Join(CoreDir(), "core-stdout.log") }
func CoreBinDir() string     { return filepath.Join(CoreDir(), "bin") }

// CorePIDFile 是后台（daemon）内核的 pid 文件。
func CorePIDFile() string { return filepath.Join(CoreDir(), "core.pid") }

// CoreBinFile 返回内置内核释放后的路径（Windows 带 .exe 后缀）。
func CoreBinFile() string {
	name := "mihomo"
	if runtime.GOOS == "windows" {
		name = "mihomo.exe"
	}
	return filepath.Join(CoreBinDir(), name)
}
