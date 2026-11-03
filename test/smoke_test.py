#!/usr/bin/env python3
"""flclash-tui 端到端冒烟测试：在 pty 中驱动 TUI + 真实 mihomo 内核，校验完整交互链路。"""
import codecs
import http.server
import json
import os
import pty
import re
import select
import secrets
import subprocess
import sys
import threading
import time

COLS, ROWS = 120, 38
HOME = "/tmp/flt-test"
ANSI_RE = re.compile(r"\x1b\[[0-9;?]*[a-zA-Z]|\x1b\][^\x07]*\x07|\x1b[()][0B]|\x1b[=>]")


def strip_ansi(s: str) -> str:
    return ANSI_RE.sub("", s)


# ---------- 夹具重置（保证幂等） ----------

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
  - name: "美国01"
    type: ss
    server: 192.0.2.12
    port: 8388
    cipher: aes-128-gcm
    password: "dummy3"
proxy-groups:
  - name: "🚀 节点选择"
    type: select
    proxies: ["香港01", "香港02", "美国01", "DIRECT"]
  - name: "♻️ 自动选择"
    type: url-test
    url: http://www.gstatic.com/generate_204
    interval: 300
    proxies: ["香港01", "香港02"]
rules:
  - DOMAIN-SUFFIX,google.com,🚀 节点选择
  - DOMAIN-KEYWORD,baidu,DIRECT
  - GEOIP,CN,DIRECT
  - MATCH,🚀 节点选择
"""

os.makedirs(f"{HOME}/profiles", exist_ok=True)
os.makedirs(f"{HOME}/core", exist_ok=True)
with open(f"{HOME}/settings.json", "w") as f:
    json.dump({
        "coreMode": "managed", "corePath": "/tmp/mihomo",
        "controllerAddr": "127.0.0.1:19090", "secret": "testsecret123",
        "mixedPort": 17890, "mode": "rule", "logLevel": "info",
    }, f)
for fn in os.listdir(f"{HOME}/profiles"):
    os.remove(f"{HOME}/profiles/{fn}")
pid = secrets.token_hex(8)
with open(f"{HOME}/profiles/{pid}.yaml", "w") as f:
    f.write(SUB_YAML)
with open(f"{HOME}/profiles.json", "w") as f:
    json.dump({"profiles": [{"id": pid, "type": "file", "label": "测试订阅",
                             "updatedAt": "2026-11-03T07:00:00Z",
                             "active": True, "autoUpdate": False}]}, f)

# 第二个订阅：本地 HTTP 服务（带 subscription-userinfo 头）
with open("/tmp/flt-sub2.yaml", "w") as f:
    f.write(SUB_YAML.replace("香港01", "东京01"))


class SubHandler(http.server.BaseHTTPRequestHandler):
    def do_GET(self):
        body = open("/tmp/flt-sub2.yaml", "rb").read()
        self.send_response(200)
        self.send_header("Content-Type", "text/yaml")
        # RFC 5987 编码的 UTF-8 文件名（验证 TUI 的 filename* 解码路径）
        self.send_header("Content-Disposition",
                         "attachment; filename=\"sub2.yaml\"; filename*=UTF-8''%E6%97%A5%E6%9C%AC%E8%AE%A2%E9%98%85.yaml")
        self.send_header("subscription-userinfo",
                         "upload=1073741824; download=2147483648; total=10737418240; expire=1893456000")
        self.send_header("profile-update-interval", "12")
        self.end_headers()
        self.wfile.write(body)

    def log_message(self, *a):
        pass


http.server.HTTPServer.allow_reuse_address = True
httpd = http.server.HTTPServer(("127.0.0.1", 18080), SubHandler)
threading.Thread(target=httpd.serve_forever, daemon=True).start()


# ---------- pty 驱动 ----------

class TUI:
    def __init__(self, env_extra):
        master, slave = pty.openpty()
        import fcntl, termios, struct
        fcntl.ioctl(slave, termios.TIOCSWINSZ, struct.pack("HHHH", ROWS, COLS, 0, 0))
        env = dict(os.environ)
        env.update(env_extra)
        env.update({"TERM": "xterm-256color", "LANG": "zh_CN.UTF-8"})
        self.master = master
        self.decoder = codecs.getincrementaldecoder("utf-8")("replace")
        self.proc = subprocess.Popen(
            ["./flclash-tui"], stdin=slave, stdout=slave, stderr=slave,
            env=env, close_fds=True)
        os.close(slave)
        self.buf = ""

    def read(self, seconds):
        end = time.time() + seconds
        while time.time() < end:
            r, _, _ = select.select([self.master], [], [], 0.1)
            if r:
                try:
                    data = os.read(self.master, 65536)
                except OSError:
                    break
                if not data:
                    break
                self.buf += self.decoder.decode(data)
        return strip_ansi(self.buf)

    def send(self, keys, wait=0.8):
        os.write(self.master, keys.encode())
        time.sleep(0.05)
        return self.read(wait)

    def sendline(self, line, wait=0.8):
        os.write(self.master, (line + "\r").encode())
        return self.read(wait)

    def clear(self):
        """清空累积缓冲，使后续 check 只看新内容（避免历史帧污染断言）。"""
        self.buf = ""

    def close(self):
        try:
            self.proc.terminate()
            self.proc.wait(timeout=3)
        except Exception:
            self.proc.kill()
        try:
            os.close(self.master)
        except OSError:
            pass


def check(name, text, *needles, fatal=False):
    missing = [n for n in needles if n not in text]
    if missing:
        print(f"❌ {name}: 缺少 {missing}")
        tail = text[-900:].replace("\n", " | ")
        print(f"   现场片段: …{tail}")
        if fatal:
            tui.close()
            print("---- 完整末尾 ----")
            print(text[-2500:])
            sys.exit(1)
        return False
    print(f"✅ {name}")
    return True


def goto_field(tui, label, max_steps=30):
    """在设置页自适应定位到指定字段行（逐帧读屏匹配 ▶ 选中行），返回是否成功。"""
    tui.buf = ""
    tui.send("g", wait=0.3)  # 回到顶部，拿到第一帧
    for _ in range(max_steps):
        scr = strip_ansi(tui.buf)
        tui.buf = ""
        current = None
        for line in scr.splitlines():
            if "▶" in line:
                current = line
                break
        if current is not None and label in current:
            return True
        tui.send("j", wait=0.18)
    return False


tui = TUI({"FLCLASH_TUI_HOME": HOME})

# 1) 首屏
scr = tui.read(6)
check("首屏标题", scr, "FLClash TUI", fatal=True)
check("仪表盘页签", scr, "仪表盘", "代理", "订阅", "连接", "规则", "日志", "设置", fatal=True)
check("内核运行状态", scr, "内核运行中", fatal=True)

# 2) 代理页（组列表：♻️自动选择(URLTest) / 🚀节点选择(Selector) / GLOBAL）
scr = tui.send("2", wait=1.5)
check("代理页", scr, "策略组", "节点选择", "自动选择", "香港01", fatal=True)
tui.send("j", wait=0.3)  # 移到 🚀节点选择（Selector，可手动切换）
tui.send("J", wait=0.3)
scr = tui.send("T", wait=1.0)
check("组测速提示", scr, "测速中")
time.sleep(7)
tui.read(1)
scr = tui.send("K", wait=0.3)
scr = tui.send("\r", wait=2.5)
check("切换节点成功", scr, "已切换")

# 3) 订阅页
scr = tui.send("3", wait=1.0)
check("订阅页", scr, "测试订阅", "[文件]", "使用中", fatal=True)

# 3.1) 添加订阅（走本地 HTTP，验证下载+头解析+重命名来源）
scr = tui.send("a", wait=0.6)
check("添加订阅弹窗", scr, "添加订阅", "输入订阅链接", fatal=True)
scr = tui.sendline("http://127.0.0.1:18080/sub2.yaml", wait=3.0)
check("订阅下载成功", scr, "订阅 (2)", fatal=True)
time.sleep(1.5)  # 等待核心应用重启
tui.read(2)
scr = tui.read(1.5)
check("订阅流量信息解析", scr, "10.0 GB", fatal=True)

# 3.2) 手写配置：粘贴 JSON → 规范化为 YAML 落盘 → 激活 → 规则生效
JSON1 = ('{"proxies":[{"name":"JP-01","type":"ss","server":"192.0.2.99","port":8388,'
         '"cipher":"aes-128-gcm","password":"x"}],'
         '"rules":["DOMAIN-SUFFIX,example.net,JP-01","MATCH,JP-01"]}')
JSON2 = ('{"proxies":[{"name":"JP-01","type":"ss","server":"192.0.2.99","port":8388,'
         '"cipher":"aes-128-gcm","password":"x"}],'
         '"rules":["DOMAIN-SUFFIX,example.net,JP-01","DOMAIN-SUFFIX,example.org,DIRECT",'
         '"DOMAIN-KEYWORD,baidu,DIRECT","MATCH,JP-01"]}')

scr = tui.send("n", wait=0.6)
check("手写配置弹窗", scr, "手写配置", "配置名称", fatal=True)
scr = tui.sendline("手写JSON", wait=0.8)
check("编辑器打开", scr, "新建 · 手写JSON", "ctrl+s", fatal=True)
tui.send(JSON1, wait=0.5)
scr = tui.send("\x13", wait=2.0)  # ctrl+s
check("JSON保存成功", scr, "已创建「手写JSON」", "JSON 已转为 YAML", fatal=True)
time.sleep(1.0)
tui.read(1)
yaml_ok = False
for fn in os.listdir(f"{HOME}/profiles"):
    with open(f"{HOME}/profiles/{fn}") as fh:
        if fh.read(9).startswith("proxies:"):
            yaml_ok = True
print(("✅" if yaml_ok else "❌") + " JSON 已规范化为 YAML 落盘")
if not yaml_ok:
    tui.close()
    sys.exit(1)
# 激活：定位到新建条目（列表最后一项）并回车，等待内核重载
scr = tui.send("G", wait=0.3)
scr = tui.send("\r", wait=2.0)
check("激活并重启内核", scr, "已应用并重启内核", fatal=True)
time.sleep(4)
tui.read(1)
tui.clear()
scr = tui.send("5", wait=1.5)
check("手写配置规则生效", scr, "example.net", fatal=True)

# 3.3) 编辑源：预填内容 → 清空重写 → 保存自动应用
scr = tui.send("3", wait=0.5)
tui.clear()
scr = tui.send("e", wait=0.8)
check("编辑器预填内容", scr, "编辑 · 手写JSON", "proxies:", fatal=True)
tui.send("\x0b", wait=0.2)          # ctrl+k 删光标后内容
tui.send("\x7f" * 600, wait=1.0)    # 退格兜底清空
tui.send(JSON2, wait=0.5)
scr = tui.send("\x13", wait=2.0)    # ctrl+s
# 编辑激活订阅：保存 toast 会立刻被「已应用并重启内核」接替，两者任一出现即成功
if ("配置已保存" in scr) or ("已应用并重启内核" in scr):
    print("✅ 编辑保存")
else:
    print("❌ 编辑保存: 未出现保存/应用提示")
    tui.close()
    print(scr[-1500:])
    sys.exit(1)
time.sleep(4)  # 等待内核重载
tui.read(1)
tui.clear()
scr = tui.send("5", wait=1.5)
check("编辑后的新规则生效", scr, "example.org", fatal=True)

# 3.4) 编辑取消路径
scr = tui.send("3", wait=0.5)
tui.clear()
scr = tui.send("e", wait=0.8)
scr = tui.send("\x1b", wait=0.6)
check("编辑取消", scr, "编辑已取消", fatal=True)
scr = tui.send("3", wait=0.5)

# 3.5) 订阅拉取失败自动走代理重试（双通道均失败的错误路径）
scr = tui.send("a", wait=0.6)
tui.clear()
scr = tui.sendline("http://127.0.0.1:59999/sub.yaml", wait=40.0)
check("直连失败后经代理重试(仍失败有明确提示)", scr, "添加订阅", "经代理重试仍失败", fatal=True)
scr = tui.send("3", wait=0.5)

# 4) 连接页（产生真实流量）
traffic = subprocess.Popen(
    ["curl", "-x", "http://127.0.0.1:17890", "-s", "-o", "/dev/null",
     "--max-time", "20", "http://www.baidu.com"],
    stdout=subprocess.DEVNULL, stderr=subprocess.DEVNULL)
scr = tui.send("4", wait=2.5)
check("连接页", scr, "连接 (", fatal=True)
ok = ("主机" in scr) or ("暂无连接" in scr)
print(("✅" if ok else "❌") + " 连接页内容(表格或空状态)")
if not ok:
    tui.close()
    sys.exit(1)

# 5) 规则页（当前激活的是手写配置：2 条 DomainSuffix + DomainKeyword + Match）
scr = tui.send("5", wait=1.5)
check("规则页", scr, "规则 (4)", "DomainSuffix", "example.org", fatal=True)

# 6) 日志页
scr = tui.send("6", wait=1.0)
check("日志页", scr, "日志", fatal=True)

# 7) 设置页
scr = tui.send("7", wait=1.0)
check("设置页", scr, "mihomo 内核路径", "/tmp/mihomo", "混合端口", "17890", fatal=True)

# 7.1) 编辑混合端口 17890 -> 17990：光标移到 混合端口(第5项)
for _ in range(4):
    tui.send("j", wait=0.15)
scr = tui.send("\r", wait=0.6)
check("编辑端口弹窗", scr, "编辑 · 混合端口", fatal=True)
scr = tui.send("\x01\x0b17990\r", wait=0.8)  # ctrl+a ctrl+k 清空后输入
check("端口已修改提示", scr, "已修改", fatal=True)
scr = tui.send("s", wait=0.8)
with open(f"{HOME}/settings.json") as f:
    saved = json.load(f)
if saved.get("mixedPort") == 17990:
    print("✅ 设置持久化(17990)")
else:
    print(f"❌ 设置持久化失败: {saved.get('mixedPort')}")
    tui.close()
    sys.exit(1)

# 7.2) 应用并重启内核，验证新端口生效
scr = tui.send("R", wait=5.0)
time.sleep(2)
tui.read(1)
r = subprocess.run(["curl", "-x", "http://127.0.0.1:17990", "-s", "-o", "/dev/null",
                    "-w", "%{http_code}", "--max-time", "10", "http://www.baidu.com"],
                   capture_output=True, text=True)
if r.stdout.strip() == "200":
    print("✅ 应用重启后新端口(17990)可代理")
else:
    print(f"❌ 新端口代理失败: {r.stdout} {r.stderr}")
    tui.close()
    sys.exit(1)

# 8) 帮助浮层
scr = tui.send("?", wait=0.8)
check("帮助浮层", scr, "全局快捷键", "当前页", fatal=True)
tui.send("x", wait=0.5)

# 8.5) 事件弹窗开关：关闭后模式切换不再弹出通知，重新开启后恢复
scr = tui.send("7", wait=1.0)
if not goto_field(tui, "事件弹窗(右下角)"):
    print("❌ 未能定位到事件弹窗开关")
    tui.close()
    sys.exit(1)
print("✅ 定位到事件弹窗开关")
tui.send("\r", wait=0.5)  # 关闭
tui.clear()
scr = tui.send("m", wait=1.5)
if "模式已切换" in scr:
    print("❌ 事件弹窗关闭后仍显示通知")
    tui.close()
    sys.exit(1)
print("✅ 事件弹窗关闭后不再显示通知")
if not goto_field(tui, "事件弹窗(右下角)"):
    print("❌ 未能再次定位到事件弹窗开关")
    tui.close()
    sys.exit(1)
tui.send("\r", wait=0.5)  # 重新开启
tui.clear()
scr = tui.send("m", wait=1.5)
check("事件弹窗重新开启", scr, "模式已切换", fatal=True)
scr = tui.send("m", wait=1.2)  # 恢复到规则模式
check("模式恢复规则", scr, "模式已切换: 规则", fatal=True)
tui.send("1", wait=0.5)

# 9) 模式切换
scr = tui.send("m", wait=1.2)
check("模式切换", scr, "模式已切换: 全局", fatal=True)
scr = tui.send("m", wait=1.2)
check("模式切回直连", scr, "模式已切换: 直连")
scr = tui.send("m", wait=1.2)
check("模式切回规则", scr, "模式已切换: 规则")

# 10) 仪表盘 + 事件栈：两条事件同时堆叠显示
scr = tui.send("1", wait=2.0)
check("仪表盘卡片", scr, "实时流量", "当前订阅", fatal=True)
tui.clear()
tui.send("m", wait=0.6)  # → 全局（通知1）
tui.send("c", wait=0.6)  # 关闭全部连接（通知2）
scr = tui.read(0.5)
check("事件栈多条堆叠", scr, "模式已切换: 全局", "已关闭全部连接", fatal=True)
scr = tui.send("m", wait=1.2)
scr = tui.send("m", wait=1.2)
check("模式恢复", scr, "模式已切换: 规则", fatal=True)

# 11) 退出
tui.send("Q", wait=1.5)
tui.close()
traffic.terminate()
httpd.shutdown()

time.sleep(1)
r = subprocess.run(["pgrep", "-f", "mihomo -d " + HOME], capture_output=True)
if r.returncode == 0:
    print("❌ 内核未随 TUI 退出")
    subprocess.run(["pkill", "-f", "mihomo -d " + HOME])
    sys.exit(1)
print("✅ 内核随 TUI 退出")

print("\n全部冒烟测试通过 🎉")
