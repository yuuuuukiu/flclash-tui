// Package coreasset 把 mihomo 内核二进制打包进可执行文件，并在首次运行时释放到数据目录。
//
// 内核二进制不进 git 仓库（每个约 11MB × 8 平台 = 85MB，每次升级都会在历史里留一份）。
// 改为构建期从 assets.lock.json 拉取：
//
//	go generate ./internal/coreasset   # 或 make core-assets
//
// gen.py 会按 lock 文件里的 sha256 下载/校验各平台的内核，生成 assets/*.gz 与
// asset_<os>_<arch>.go（后者只含 //go:embed 与校验值）。assets/ 已被 .gitignore 忽略。
//
// 未打包的平台 compressed 为空，Available() 返回 false，调用方回退到 PATH 里的 mihomo。
package coreasset

import (
	"bytes"
	"compress/gzip"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime"
)

// Version 是打包进二进制内的 mihomo 版本（与 assets.lock.json 保持一致）。
const Version = "v1.19.13"

// ErrUnavailable 表示当前构建平台没有内置内核。
var ErrUnavailable = errors.New("当前平台未内置 mihomo 内核")

// Platform 返回运行平台标识（如 darwin/arm64）。
func Platform() string { return runtime.GOOS + "/" + runtime.GOARCH }

// Available 报告当前构建是否内置了可用的内核。
func Available() bool { return len(compressed) > 0 }

// Sum 返回内置内核解压后的 sha256（未内置时为空串）。
func Sum() string { return embeddedSum }

// Info 汇总内置内核信息，用于 CLI 展示。
type Info struct {
	Platform  string
	Version   string
	Available bool
	Size      int64 // 解压后大小
	SHA256    string
}

func InfoOf() Info {
	return Info{
		Platform:  Platform(),
		Version:   Version,
		Available: Available(),
		Size:      decompressedSize,
		SHA256:    embeddedSum,
	}
}

// Ensure 把内置内核释放到 dest（已存在且校验通过时直接复用）。
// 返回可执行文件路径。调用方负责保证 dest 的父目录存在。
func Ensure(dest string) (string, error) {
	if !Available() {
		return "", fmt.Errorf("%w（%s）", ErrUnavailable, Platform())
	}
	if ok, err := verify(dest); err == nil && ok {
		return dest, nil
	}
	if err := os.MkdirAll(filepath.Dir(dest), 0o755); err != nil {
		return "", err
	}
	// 解压到临时文件后原子替换，避免并发/中断留下半个可执行文件
	tmp := dest + ".tmp"
	f, err := os.OpenFile(tmp, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0o755)
	if err != nil {
		return "", err
	}
	gz, err := gzip.NewReader(bytes.NewReader(compressed))
	if err != nil {
		f.Close()
		os.Remove(tmp)
		return "", fmt.Errorf("内置内核数据损坏: %w", err)
	}
	if _, err := io.Copy(f, gz); err != nil {
		f.Close()
		os.Remove(tmp)
		return "", fmt.Errorf("释放内核失败: %w", err)
	}
	gz.Close()
	if err := f.Close(); err != nil {
		os.Remove(tmp)
		return "", err
	}
	if err := os.Chmod(tmp, 0o755); err != nil {
		os.Remove(tmp)
		return "", err
	}
	if ok, err := verify(tmp); err != nil || !ok {
		os.Remove(tmp)
		return "", fmt.Errorf("释放的内核校验失败（%v）", err)
	}
	if err := os.Rename(tmp, dest); err != nil {
		os.Remove(tmp)
		return "", err
	}
	_ = os.WriteFile(versionFile(dest), []byte(Version+"\n"), 0o644)
	return dest, nil
}

// verify 校验目标文件是否存在且 sha256 与内置值一致。
func verify(path string) (bool, error) {
	fi, err := os.Stat(path)
	if err != nil {
		return false, err
	}
	if fi.IsDir() || fi.Size() == 0 {
		return false, nil
	}
	if embeddedSum == "" {
		return false, nil
	}
	f, err := os.Open(path)
	if err != nil {
		return false, err
	}
	defer f.Close()
	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return false, err
	}
	if hex.EncodeToString(h.Sum(nil)) != embeddedSum {
		return false, nil
	}
	// 版本标记也要匹配（升级二进制后强制重新释放）
	if data, err := os.ReadFile(versionFile(path)); err == nil {
		if v := string(bytes.TrimSpace(data)); v != "" && v != Version {
			return false, nil
		}
	}
	return true, nil
}

func versionFile(corePath string) string { return corePath + ".version" }
