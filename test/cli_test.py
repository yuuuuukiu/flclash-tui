#!/usr/bin/env python3
"""flclash-tui 非交互式 CLI 端到端测试：不依赖 TUI，直接驱动全部子命令。

覆盖：内置内核释放、core 生命周期、profile 增删改查与 $EDITOR 集成、
proxy/conn/rule/config/settings/sysproxy 只读与写操作、--json 输出与退出码。
"""
import http.server
import json
import os
import re
import secrets
import shutil
import subprocess
import sys
import tempfile
import threading
import time

BIN = os.path.abspath("./flclash-tui")
HOME = "/tmp/flt-cli-test"
CTRL = "127.0.0.1:39090"
MIXED = 37890

passed = failed = 0


def ok(name, cond, detail=""):
    global passed, failed
    if cond:
        passed += 1
        print(f"✅ {name}")
    else:
        failed += 1
        print(f"❌ {name}" + (f"  ← {detail}" if detail else ""))


def run(*args, stdin=None, timeout=120, expect=0):
    p = subprocess.run([BIN, "--home", HOME, *args], input=stdin,
                       capture_output=True, text=True, timeout=timeout)
    if expect is not None and p.returncode != expect:
        print(f"   [cmd] {' '.join(args)} -> rc={p.returncode}")
        print(f"   [out] {p.stdout.strip()[:400]}")
        print(f"   [err] {p.stderr.strip()[:400]}")
    return p


def runj(*args, **kw):
    p = run(*args, *(["--json"] if True else []), **kw)
    try:
        return json.loads(p.stdout), p.returncode
    except json.JSONDecodeError:
        print(f"   [非 JSON 输出] {p.stdout[:300]} {p.stderr[:300]}")
        return {}, p.returncode


SUB_YAML = """proxies:
  - name: "香港01"
    type: ss
    server: 192.0.2.10
    port: 8388
    cipher: aes-128-gcm
    password: "dummy1"
  - name: "香港02"
    type: ss
    server: 192.0.2.11
    port: 8388
    cipher: aes-128-gcm
    password: "dummy2"
proxy-groups:
  - name: "节点选择"
    type: select
    proxies: ["香港01", "香港02", "DIRECT"]
rules:
  - DOMAIN-SUFFIX,example.org,节点选择
  - MATCH,节点选择
"""

SUB2_YAML = """proxies:
  - name: "东京01"
    type: ss
    server: 192.0.2.20
    port: 8388
    cipher: aes-128-gcm
    password: "dummy9"
proxy-groups:
  - name: "自动选择"
    type: url-test
    url: http://www.gstatic.com/generate_204
    interval: 300
    proxies: ["东京01"]
rules:
  - DOMAIN-SUFFIX,newexample.com,自动选择
  - MATCH,自动选择
"""

JSON_CONF = json.dumps({
    "proxies": [{"name": "JSON节点", "type": "ss", "server": "192.0.2.30",
                 "port": 8388, "cipher": "aes-128-gcm", "password": "x"}],
    "rules": ["MATCH,JSON节点"],
})


# ---------- 本地订阅服务器（带 subscription-userinfo 头） ----------

class SubHandler(http.server.BaseHTTPRequestHandler):
    def do_GET(self):
        body = SUB2_YAML.encode()
        self.send_response(200)
        self.send_header("Content-Type", "text/yaml")
        self.send_header("Content-Disposition",
                         "attachment; filename=\"s.yaml\"; filename*=UTF-8''%E6%97%A5%E6%9C%AC%E8%AE%A2%E9%98%85.yaml")
        self.send_header("subscription-userinfo",
                         "upload=1073741824; download=2147483648; total=10737418240; expire=1893456000")
        self.send_header("profile-update-interval", "12")
        self.end_headers()
        self.wfile.write(body)

    def log_message(self, *a):
        pass


def main():
    # ---------- 0) 干净环境 ----------
    if os.path.isdir(HOME):
        shutil.rmtree(HOME)
    os.makedirs(f"{HOME}/core", exist_ok=True)
    with open(f"{HOME}/settings.json", "w") as f:
        json.dump({
            "coreMode": "managed", "controllerAddr": CTRL, "secret": "clisecret",
            "mixedPort": MIXED, "mode": "rule", "logLevel": "info", "autoUpdate": False,
        }, f)

    http.server.HTTPServer.allow_reuse_address = True
    httpd = http.server.HTTPServer(("127.0.0.1", 38080), SubHandler)
    threading.Thread(target=httpd.serve_forever, daemon=True).start()

    # ---------- 1) version / help ----------
    p = run("version", expect=0)
    ok("version 输出内置内核", "mihomo v1.19.13" in p.stdout, p.stdout)
    d, _ = runj("version")
    ok("version --json", d.get("coreEmbedded") is True and d.get("platform", "").count("/") == 1, d)

    p = run("help", expect=0)
    ok("help 列出子命令", "profile" in p.stdout and "core" in p.stdout)
    p = run("help", "profile", expect=0)
    ok("help profile 显示用法", "profile edit" in p.stdout)

    p = run("nosuchcmd", expect=2)
    ok("未知子命令退出码 2", p.returncode == 2)

    # ---------- 2) core path / 内置内核释放 ----------
    d, _ = runj("core", "path")
    ok("core path 报告内置内核可用", d.get("embedded") is True, d)
    bin_path = d.get("extracted")
    ok("core path 给出释放路径", bool(bin_path), d)
    d, _ = runj("core", "path", "--extract")
    ok("core path --extract 释放内核", os.path.isfile(bin_path) and os.access(bin_path, os.X_OK), d)
    v = subprocess.run([bin_path, "-v"], capture_output=True, text=True)
    ok("释放的内核可执行", "Mihomo Meta" in v.stdout + v.stderr, v.stdout)

    # ---------- 3) profile create（stdin / file / --activate） ----------
    p = run("profile", "create", "--label", "甲", stdin=SUB_YAML, expect=0)
    ok("profile create 从 stdin 创建", "已创建配置「甲」" in p.stdout, p.stdout)
    d, _ = runj("profile", "list")
    ok("profile list --json 有一条", d.get("count") == 1, d)
    pid1 = d["profiles"][0]["id"]
    ok("第一条订阅自动激活", d["profiles"][0]["active"] is True, d)

    yaml_file = os.path.join(tempfile.mkdtemp(), "conf.yaml")
    with open(yaml_file, "w") as f:
        f.write(SUB2_YAML)
    p = run("profile", "create", "--label", "乙", "--file", yaml_file, expect=0)
    ok("profile create --file 创建", "已创建配置「乙」" in p.stdout, p.stdout)

    d, _ = runj("profile", "list")
    ok("profile list 两条", d.get("count") == 2, d)

    # JSON 粘贴自动规范化
    p = run("profile", "create", "--label", "JSON配置", stdin=JSON_CONF, expect=0)
    ok("JSON 内容可创建", "已创建配置「JSON配置」" in p.stdout, p.stdout)
    d, _ = runj("profile", "list")
    json_id = [x["id"] for x in d["profiles"] if x["label"] == "JSON配置"][0]
    p = run("profile", "show", json_id, expect=0)
    ok("JSON 已转成块状 YAML 落盘",
       "proxies:" in p.stdout and not p.stdout.strip().endswith("}") and '"name"' not in p.stdout,
       p.stdout)

    # 非法内容必须被拒绝且不落盘
    p = run("profile", "create", "--label", "坏的", stdin="not: [valid", expect=1)
    ok("非法 YAML 创建被拒", p.returncode == 1)
    d, _ = runj("profile", "list")
    ok("非法内容未落盘", d.get("count") == 3, d)

    # ---------- 4) core 生命周期 ----------
    d, rc = runj("core", "status")
    ok("core status 未运行时退出码 3", rc == 3, (d, rc))

    p = run("core", "start", "--daemon", expect=0)
    ok("core start --daemon", "后台启动" in p.stdout, p.stdout)
    d, rc = runj("core", "status")
    ok("core status 运行中", d.get("running") is True and rc == 0, (d, rc))
    ok("core status 版本正确", d.get("version") == "v1.19.13", d)
    daemon_pid = d.get("pid")

    # 重复启动应被拒绝
    p = run("core", "start", "--daemon", expect=1)
    ok("重复启动被拒（端口占用）", "已被占用" in p.stderr, p.stderr)

    # ---------- 5) 激活 / 热重载不换 pid ----------
    p = run("profile", "activate", pid1, expect=0)
    ok("profile activate", "已激活「甲」" in p.stdout, p.stdout)
    d, _ = runj("core", "status")
    ok("热重载不重启内核进程", d.get("pid") == daemon_pid, (d.get("pid"), daemon_pid))

    d, _ = runj("rule", "list")
    payloads = [r["payload"] for r in d.get("rules", [])]
    ok("规则来自激活订阅", "example.org" in payloads, payloads)

    # 切换订阅后规则跟着变
    d, _ = runj("profile", "list")
    pid2 = [x["id"] for x in d["profiles"] if x["label"] == "乙"][0]
    run("profile", "activate", pid2, expect=0)
    d, _ = runj("rule", "list")
    payloads = [r["payload"] for r in d.get("rules", [])]
    ok("切换订阅后规则同步", "newexample.com" in payloads and "example.org" not in payloads, payloads)

    # ---------- 6) proxy ----------
    d, rc = runj("proxy", "groups")
    names = [g["name"] for g in d.get("groups", [])]
    ok("proxy groups 列出策略组", "自动选择" in names, names)
    ok("GLOBAL 组排在最后", names[-1] == "GLOBAL", names)

    d, _ = runj("proxy", "list", "自动选择")
    ok("proxy list 组内节点", [n["name"] for n in d.get("nodes", [])] == ["东京01"], d)

    p = run("proxy", "select", "自动选择", "东京01", expect=0)
    ok("proxy select 切换节点", "已切换" in p.stdout, p.stdout)
    d, _ = runj("proxy", "groups")
    now = {g["name"]: g["now"] for g in d["groups"]}
    ok("切换结果已生效", now.get("自动选择") == "东京01", now)

    d, _ = runj("proxy", "list", "--all")
    ok("proxy list --all 列出全部节点", any(n["name"] == "东京01" for n in d.get("proxies", [])), d)

    p = run("proxy", "select", "不存在的组", "x", expect=1)
    ok("切换不存在的组报错", p.returncode == 1)

    # ---------- 7) config ----------
    d, _ = runj("config", "show")
    ok("config show 读出端口", d.get("config", {}).get("mixed-port") == MIXED, d)

    p = run("config", "mode", "global", expect=0)
    ok("config mode 切全局", "全局" in p.stdout, p.stdout)
    d, _ = runj("config", "show")
    ok("config show 反映新模式", d["config"]["mode"] == "global", d)
    run("config", "mode", "rule", expect=0)
    p = run("config", "mode", "bogus", expect=2)
    ok("非法模式退出码 2", p.returncode == 2, p.stderr)

    p = run("config", "patch", '{"log-level":"warning"}', expect=0)
    ok("config patch 热更新", "已应用配置补丁" in p.stdout, p.stdout)
    run("config", "patch", '{"log-level":"info"}', expect=0)

    p = run("config", "reload", expect=0)
    ok("config reload", "已让内核重载" in p.stdout, p.stdout)

    p = run("config", "path", expect=0)
    ok("config path 指向 core/config.yaml", p.stdout.strip().endswith("core/config.yaml"), p.stdout)

    p = run("config", "flush-fakeip", expect=0)
    ok("config flush-fakeip", "已清空" in p.stdout, p.stdout)

    # ---------- 8) conn / log ----------
    d, _ = runj("conn", "list")
    ok("conn list --json 结构完整", "connections" in d and "uploadTotal" in d, d)
    p = run("conn", "close-all", expect=0)
    ok("conn close-all", "已关闭全部连接" in p.stdout, p.stdout)

    # 日志流：限定 3 秒采集
    t0 = time.time()
    p = run("log", "tail", "--duration", "3s", expect=0, timeout=30)
    ok("log tail --duration 按时退出", time.time() - t0 < 12, f"{time.time() - t0:.1f}s")
    d_lines = [l for l in p.stdout.splitlines() if l.strip()]
    ok("log tail 收到日志（或至少不报错）", p.returncode == 0, p.stderr[:200])

    # 非交互 + 无限时长应被拒绝
    p = run("log", "tail", stdin="", expect=2, timeout=30)
    ok("log tail 非交互未限时长时报用法错误", p.returncode == 2, p.stderr[:200])

    # ---------- 9) 订阅 URL / 更新 ----------
    p = run("profile", "add", "http://127.0.0.1:38080/sub.yaml", "--activate", expect=0)
    ok("profile add URL 订阅", "已添加订阅" in p.stdout, p.stdout)
    d, _ = runj("profile", "list")
    url_prof = [x for x in d["profiles"] if x["type"] == "url"]
    ok("URL 订阅记录", len(url_prof) == 1, url_prof)
    ok("filename* 解码为订阅名", url_prof[0]["label"] == "日本订阅", url_prof[0]["label"])
    ok("subscription-userinfo 已解析", url_prof[0]["subscriptionInfo"]["total"] == 10737418240,
       url_prof[0].get("subscriptionInfo"))
    ok("profile-update-interval 已记录", url_prof[0].get("autoUpdate") is True, url_prof[0])

    p = run("profile", "update", url_prof[0]["id"], expect=0)
    ok("profile update 单条", "已更新" in p.stdout, p.stdout)
    p = run("profile", "update", "--all", expect=0)
    ok("profile update --all", "已更新" in p.stdout, p.stdout)

    p = run("profile", "add", "http://127.0.0.1:59999/nope.yaml", expect=1)
    ok("不可达订阅报错（含代理回退说明）", "经代理重试仍失败" in p.stderr, p.stderr)

    # ---------- 10) profile set / rename / show / remove ----------
    run("profile", "activate", pid2, expect=0)
    p = run("profile", "set", pid2, stdin=JSON_CONF, expect=0)
    ok("profile set 从 stdin 覆盖", "已更新" in p.stdout, p.stdout)
    d, _ = runj("rule", "list")
    ok("profile set 后规则生效",
       len(d["rules"]) == 1 and d["rules"][0]["proxy"] == "JSON节点", d)

    p = run("profile", "rename", pid2, "改过的名字", expect=0)
    ok("profile rename", "已重命名" in p.stdout, p.stdout)
    d, _ = runj("profile", "list")
    ok("重命名已持久化", any(x["label"] == "改过的名字" for x in d["profiles"]), d)

    p = run("profile", "show", pid2, "--raw", expect=0)
    ok("profile show --raw 只输出内容", p.stdout.lstrip().startswith("proxies:"), p.stdout[:80])

    # ---------- 11) profile edit（$EDITOR 集成） ----------
    editor = os.path.join(tempfile.mkdtemp(), "ed.sh")
    with open(editor, "w") as f:
        f.write("#!/bin/bash\nsed -i '' 's/MATCH,JSON节点/MATCH,DIRECT/' \"$1\"\n")
    os.chmod(editor, 0o755)
    env = dict(os.environ, EDITOR=editor, FLCLASH_TUI_HOME=HOME)
    # 需要 tty：用 script(1) 提供伪终端
    cmd = f"EDITOR={editor} {BIN} --home {HOME} profile edit {pid2}"
    p = subprocess.run(["script", "-q", "/dev/null", "sh", "-c", cmd],
                       capture_output=True, text=True, timeout=90)
    ok("profile edit 拉起编辑器并保存", "已保存" in p.stdout, (p.stdout[:300], p.stderr[:200]))
    d, _ = runj("rule", "list")
    # sed 把 MATCH,JSON节点 改成 MATCH,DIRECT，内核里应只剩一条 Match 且出口 DIRECT
    ok("编辑器保存的内容已生效", len(d["rules"]) == 1 and d["rules"][0]["proxy"] == "DIRECT", d)

    # 编辑器放弃修改（非零退出码）
    abort_ed = os.path.join(tempfile.mkdtemp(), "abort.sh")
    with open(abort_ed, "w") as f:
        f.write("#!/bin/bash\nexit 1\n")
    os.chmod(abort_ed, 0o755)
    cmd = f"EDITOR={abort_ed} {BIN} --home {HOME} profile edit {pid2}"
    p = subprocess.run(["script", "-q", "/dev/null", "sh", "-c", cmd],
                       capture_output=True, text=True, timeout=90)
    ok("编辑器非零退出视为取消", "已取消" in p.stdout, p.stdout[:300])

    # 编辑器保存非法 YAML
    bad_ed = os.path.join(tempfile.mkdtemp(), "bad.sh")
    with open(bad_ed, "w") as f:
        f.write("#!/bin/bash\necho 'not: [valid' > \"$1\"\n")
    os.chmod(bad_ed, 0o755)
    cmd = f"EDITOR={bad_ed} {BIN} --home {HOME} profile edit {pid2}"
    p = subprocess.run(["script", "-q", "/dev/null", "sh", "-c", cmd],
                       capture_output=True, text=True, timeout=90)
    ok("编辑器保存非法内容被拒（退出码 1）", p.returncode == 1 and "rejected" in p.stdout,
       (p.returncode, p.stdout[:300]))

    # 非 tty 下 edit 应给出明确指引
    p = subprocess.run([BIN, "--home", HOME, "profile", "edit", pid2],
                       stdin=subprocess.DEVNULL, capture_output=True, text=True, timeout=60)
    ok("非 tty 下 edit 提示改用 set", p.returncode == 1 and "profile set" in p.stderr, p.stderr[:200])

    # ---------- 12) settings ----------
    d, _ = runj("settings", "list")
    s = d.get("settings", {})
    ok("settings list --json", s.get("mixedPort") == MIXED and s.get("coreMode") == "managed", s)

    p = run("settings", "get", "logLevel", expect=0)
    ok("settings get", p.stdout.strip() == "info", p.stdout)
    p = run("settings", "set", "logLevel", "warning", expect=0)
    ok("settings set 持久化", json.load(open(f"{HOME}/settings.json"))["logLevel"] == "warning", p.stdout)
    run("settings", "set", "logLevel", "info", expect=0)

    p = run("settings", "set", "mixedPort", "abc", expect=2)
    ok("非法端口退出码 2", p.returncode == 2, p.stderr)
    p = run("settings", "set", "nosuchkey", "1", expect=2)
    ok("未知设置键退出码 2", p.returncode == 2, p.stderr)

    # 改端口 + apply 后新端口可用
    new_port = MIXED + 100
    p = run("settings", "set", "mixedPort", str(new_port), "--apply", expect=0)
    ok("settings set --apply", "内核已重启" in p.stdout or "已应用" in p.stdout, p.stdout)
    time.sleep(1.5)
    d, _ = runj("config", "show")
    ok("应用后内核端口已变", d["config"]["mixed-port"] == new_port, d["config"]["mixed-port"])

    # ---------- 13) 退出清理 ----------
    p = run("core", "stop", expect=0)
    ok("core stop", "已停止" in p.stdout, p.stdout)
    d, rc = runj("core", "status")
    ok("core stop 后状态为未运行", d.get("running") is False and rc == 3, (d, rc))
    p = run("core", "stop", expect=0)
    ok("重复 stop 幂等", "没有正在运行的内核" in p.stdout, p.stdout)

    # ---------- 14) core restart ----------
    p = run("core", "restart", expect=0)
    ok("core restart", "后台启动" in p.stdout or "已后台" in p.stdout, p.stdout)
    d, _ = runj("core", "status")
    ok("restart 后运行中", d.get("running") is True, d)

    # ---------- 15) profile remove ----------
    p = run("profile", "remove", pid1, "--yes", expect=0)
    ok("profile remove --yes", "已删除" in p.stdout, p.stdout)
    d, _ = runj("profile", "list")
    ok("删除后数量减少", d.get("count") == 3, d)

    # 删除激活订阅后内核仍可用
    d, _ = runj("profile", "list")
    active_id = [x["id"] for x in d["profiles"] if x["active"]][0]
    run("profile", "remove", active_id, "--yes", expect=0)
    d, _ = runj("core", "status")
    ok("删除激活订阅后内核仍运行", d.get("running") is True, d)

    # ---------- 16) 清理 ----------
    run("core", "stop", expect=0)
    httpd.shutdown()

    print(f"\n{passed} 项通过，{failed} 项失败")
    if failed:
        sys.exit(1)
    print("全部 CLI 测试通过 🎉")


if __name__ == "__main__":
    main()
