#!/usr/bin/env python3
"""驱动 flclash-tui 并用微型 ANSI 屏幕模拟器重建终端画面，用于 README 截图与调试。"""
import codecs
import fcntl
import os
import pty
import re
import select
import struct
import subprocess
import sys
import termios
import time


class Screen:
    """最小 ANSI 终端模拟：CUP / CUU / CUD / CUF / CUB / EL / ED / \r \n。"""

    def __init__(self, cols, rows):
        self.cols, self.rows = cols, rows
        self.buf = [[" "] * cols for _ in range(rows)]
        self.x = self.y = 0

    def _put(self, ch):
        if self.x >= self.cols:
            self.x = 0
            self.y += 1
        if self.y >= self.rows:
            self.buf.pop(0)
            self.buf.append([" "] * self.cols)
            self.y = self.rows - 1
        self.buf[self.y][self.x] = ch
        self.x += 1

    def feed(self, data):
        i = 0
        n = len(data)
        while i < n:
            c = data[i]
            if c == "\x1b":
                m = re.match(r"\x1b\[([0-9;?]*)([a-zA-Z])", data[i:])
                if m:
                    params, cmd = m.group(1), m.group(2)
                    self._csi(params, cmd)
                    i += m.end()
                    continue
                m = re.match(r"\x1b\][^\x07]*\x07|\x1b[()][0B]|\x1b[=>]", data[i:])
                if m:
                    i += m.end()
                    continue
                i += 1
                continue
            if c == "\r":
                self.x = 0
            elif c == "\n":
                self.y += 1
                if self.y >= self.rows:
                    self.buf.pop(0)
                    self.buf.append([" "] * self.cols)
                    self.y = self.rows - 1
            elif c == "\x07" or c == "\b":
                pass
            else:
                self._put(c)
            i += 1

    def _csi(self, params, cmd):
        # altscreen 切换：清屏归位（bubbletea 在 1049h 后整帧重绘）
        if "1049" in params and cmd in "hl":
            self.buf = [[" "] * self.cols for _ in range(self.rows)]
            self.x = self.y = 0
            return
        if cmd == "h" or cmd == "l":
            return
        nums = [int(p) if p.isdigit() else 0 for p in params.split(";") if p != "" or cmd in "Hf"]
        if cmd in "Hf":  # CUP
            row = nums[0] - 1 if len(nums) > 0 and nums[0] else 0
            col = nums[1] - 1 if len(nums) > 1 and nums[1] else 0
            self.y = max(0, min(row, self.rows - 1))
            self.x = max(0, min(col, self.cols - 1))
        elif cmd == "A":
            self.y = max(0, self.y - (nums[0] if nums else 1))
        elif cmd == "B":
            self.y = min(self.rows - 1, self.y + (nums[0] if nums else 1))
        elif cmd == "C":
            self.x = min(self.cols - 1, self.x + (nums[0] if nums else 1))
        elif cmd == "D":
            self.x = max(0, self.x - (nums[0] if nums else 1))
        elif cmd == "G":
            self.x = max(0, min((nums[0] - 1) if nums else 0, self.cols - 1))
        elif cmd == "J":
            mode = nums[0] if nums else 0
            if mode == 2:
                self.buf = [[" "] * self.cols for _ in range(self.rows)]
            elif mode == 0:
                for x in range(self.x, self.cols):
                    self.buf[self.y][x] = " "
                for y in range(self.y + 1, self.rows):
                    self.buf[y] = [" "] * self.cols
        elif cmd == "K":
            mode = nums[0] if nums else 0
            if mode == 0:
                for x in range(self.x, self.cols):
                    self.buf[self.y][x] = " "
            elif mode == 2:
                self.buf[self.y] = [" "] * self.cols

    def text(self):
        return "\n".join("".join(row).rstrip() for row in self.buf).rstrip() + "\n"


def capture(page_keys, wait=2.0, cols=118, rows=34, out=None):
    master, slave = pty.openpty()
    fcntl.ioctl(slave, termios.TIOCSWINSZ, struct.pack("HHHH", rows, cols, 0, 0))
    env = dict(os.environ)
    env.update({"FLCLASH_TUI_HOME": "/tmp/flt-test", "TERM": "xterm-256color", "LANG": "zh_CN.UTF-8"})
    proc = subprocess.Popen(["./flclash-tui"], stdin=slave, stdout=slave, stderr=slave,
                            env=env, close_fds=True)
    os.close(slave)
    dec = codecs.getincrementaldecoder("utf-8")("replace")
    screen = Screen(cols, rows)
    data = ""

    def read(sec):
        nonlocal data
        end = time.time() + sec
        while time.time() < end:
            r, _, _ = select.select([master], [], [], 0.1)
            if r:
                try:
                    d = os.read(master, 65536)
                except OSError:
                    break
                if not d:
                    break
                data += dec.decode(d)

    read(8)
    if page_keys:
        os.write(master, page_keys.encode())
        time.sleep(0.1)
    read(wait)
    screen.feed(data)
    proc.terminate()
    proc.wait()
    try:
        os.close(master)
    except OSError:
        pass
    txt = screen.text()
    if out:
        with open(out, "w") as f:
            f.write(txt)
    return txt


if __name__ == "__main__":
    keys = sys.argv[1] if len(sys.argv) > 1 else ""
    out = sys.argv[2] if len(sys.argv) > 2 else None
    print(capture(keys, out=out))
