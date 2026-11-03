package coreasset

import (
	"os"
	"path/filepath"
	"testing"
)

func TestInfoMatchesBuild(t *testing.T) {
	info := InfoOf()
	if info.Platform == "" {
		t.Fatal("Platform 不应为空")
	}
	if info.Version != Version {
		t.Fatalf("Version = %q, 期望 %q", info.Version, Version)
	}
	if !info.Available {
		t.Skipf("当前平台 %s 未内置内核（先跑 python3 internal/coreasset/gen.py）", info.Platform)
	}
	if len(info.SHA256) != 64 {
		t.Fatalf("sha256 长度异常: %q", info.SHA256)
	}
	if info.Size <= 0 {
		t.Fatalf("解压后大小应为正数，得到 %d", info.Size)
	}
	// 内置 mihomo 都是 28MB 上下，明显小于这个值说明资产打错了
	if info.Size < 20*1024*1024 {
		t.Fatalf("解压后大小 %d 过小，内置内核可能不完整", info.Size)
	}
}

func TestEnsureExtractsAndIsIdempotent(t *testing.T) {
	if !Available() {
		t.Skip("当前平台未内置内核（先跑 python3 internal/coreasset/gen.py）")
	}
	dest := filepath.Join(t.TempDir(), "bin", "mihomo")

	p, err := Ensure(dest)
	if err != nil {
		t.Fatalf("Ensure 失败: %v", err)
	}
	if p != dest {
		t.Fatalf("Ensure 返回 %q, 期望 %q", p, dest)
	}
	fi, err := os.Stat(dest)
	if err != nil {
		t.Fatalf("释放后文件不存在: %v", err)
	}
	if fi.Size() != decompressedSize {
		t.Fatalf("释放后大小 = %d, 期望 %d", fi.Size(), decompressedSize)
	}
	if fi.Mode().Perm()&0o111 == 0 {
		t.Fatalf("释放后的内核不可执行: %v", fi.Mode())
	}
	// 版本标记应写盘
	if data, err := os.ReadFile(versionFile(dest)); err != nil || string(data) != Version+"\n" {
		t.Fatalf("版本标记异常: %q, %v", data, err)
	}

	// 第二次调用应复用同一个文件（不重写）而不是重新解压
	before := fi.ModTime()
	if _, err := Ensure(dest); err != nil {
		t.Fatalf("重复 Ensure 失败: %v", err)
	}
	fi2, err := os.Stat(dest)
	if err != nil {
		t.Fatal(err)
	}
	if !fi2.ModTime().Equal(before) {
		t.Fatal("重复 Ensure 不应重写已校验通过的文件")
	}
}

func TestEnsureRepairsCorruptedFile(t *testing.T) {
	if !Available() {
		t.Skip("当前平台未内置内核（先跑 python3 internal/coreasset/gen.py）")
	}
	dest := filepath.Join(t.TempDir(), "mihomo")
	if err := os.WriteFile(dest, []byte("not a binary"), 0o755); err != nil {
		t.Fatal(err)
	}
	if _, err := Ensure(dest); err != nil {
		t.Fatalf("损坏文件应被重新释放: %v", err)
	}
	fi, err := os.Stat(dest)
	if err != nil {
		t.Fatal(err)
	}
	if fi.Size() != decompressedSize {
		t.Fatalf("修复后大小 = %d, 期望 %d", fi.Size(), decompressedSize)
	}
}
