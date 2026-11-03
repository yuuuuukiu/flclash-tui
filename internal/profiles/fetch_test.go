package profiles

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

type failingTransport struct{}

func (failingTransport) RoundTrip(*http.Request) (*http.Response, error) {
	return nil, context.DeadlineExceeded
}

func TestFetchFallbackToProxy(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("subscription-userinfo", "upload=1; download=2; total=100; expire=0")
		w.Header().Set("Content-Disposition", `attachment; filename="sub.yaml"`)
		_, _ = w.Write([]byte("proxies:\n  - name: a\ntype: ss\n"))
	}))
	defer srv.Close()

	// 直连必然失败，代理客户端正常 → 应回退成功并标记 viaProxy
	direct := &http.Client{Transport: failingTransport{}}
	data, meta, viaProxy, err := fetchWithClients(context.Background(), srv.URL+"/s", direct, srv.Client())
	if err != nil {
		t.Fatalf("回退拉取失败: %v", err)
	}
	if !viaProxy {
		t.Fatal("应标记为经代理拉取")
	}
	if !strings.Contains(string(data), "proxies:") {
		t.Fatalf("内容错误: %q", data)
	}
	if meta == nil || meta.subInfo == nil || meta.subInfo.Total != 100 {
		t.Fatalf("流量信息解析失败: %+v", meta)
	}
}

func TestFetchDirectSucceeds(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte("proxies: []\n"))
	}))
	defer srv.Close()

	data, _, viaProxy, err := fetchWithClients(context.Background(), srv.URL, srv.Client(), srv.Client())
	if err != nil || viaProxy {
		t.Fatalf("直连成功不应走代理: viaProxy=%v err=%v", viaProxy, err)
	}
	if len(data) == 0 {
		t.Fatal("内容为空")
	}
}

func TestFetchAllChannelsFail(t *testing.T) {
	_, _, usedProxy, err := fetchWithClients(context.Background(), "http://127.0.0.1:1/x",
		&http.Client{Transport: failingTransport{}}, &http.Client{Transport: failingTransport{}})
	if err == nil {
		t.Fatal("全部失败应返回错误")
	}
	if !usedProxy {
		t.Fatal("应标记已尝试代理通道")
	}
	if !strings.Contains(err.Error(), "直连失败") || !strings.Contains(err.Error(), "经代理重试仍失败") {
		t.Fatalf("错误信息应说明两个通道: %v", err)
	}
}

func TestFetchSingleChannelFail(t *testing.T) {
	_, _, usedProxy, err := fetchWithClients(context.Background(), "http://127.0.0.1:1/x",
		&http.Client{Transport: failingTransport{}})
	if err == nil || usedProxy {
		t.Fatalf("单通道失败不应标记代理: usedProxy=%v err=%v", usedProxy, err)
	}
}
