package corecfg

import (
	"strings"
	"testing"
)

func TestNormalizeJSONToBlockYAML(t *testing.T) {
	in := `{"proxies":[{"name":"JP-01","type":"ss","server":"192.0.2.99","port":8388,"cipher":"aes-128-gcm","password":"x"}],"rules":["DOMAIN-SUFFIX,example.net,JP-01","MATCH,JP-01"]}`
	out, converted, err := Normalize([]byte(in))
	if err != nil {
		t.Fatalf("Normalize 失败: %v", err)
	}
	if !converted {
		t.Fatal("应识别为 JSON 输入")
	}
	s := string(out)
	if strings.HasPrefix(s, "{") {
		t.Fatalf("输出不应是 flow 风格:\n%s", s)
	}
	if !strings.HasPrefix(s, "proxies:") {
		t.Fatalf("键名不应带引号且应为块状 YAML:\n%s", s)
	}
	if !strings.Contains(s, "example.net") {
		t.Fatalf("内容丢失:\n%s", s)
	}
	// 数字端口不应变成浮点
	if strings.Contains(s, "8388.0") || strings.Contains(s, `port: "8388"`) {
		t.Fatalf("端口类型被改变:\n%s", s)
	}
}

func TestNormalizeYAMLPassthrough(t *testing.T) {
	in := "proxies:\n  - name: a # 注释\nrules:\n  - MATCH,DIRECT\n"
	out, converted, err := Normalize([]byte(in))
	if err != nil {
		t.Fatalf("Normalize 失败: %v", err)
	}
	if converted {
		t.Fatal("YAML 输入不应标记为转换")
	}
	if !strings.Contains(string(out), "# 注释") {
		t.Fatalf("注释应保留:\n%s", out)
	}
}

func TestNormalizeInvalid(t *testing.T) {
	if _, _, err := Normalize([]byte("not: [valid")); err == nil {
		t.Fatal("非法内容应报错")
	}
	if _, _, err := Normalize([]byte("   ")); err == nil {
		t.Fatal("空内容应报错")
	}
	if _, _, err := Normalize([]byte("- a\n- b")); err == nil {
		t.Fatal("根节点为序列应报错")
	}
}
