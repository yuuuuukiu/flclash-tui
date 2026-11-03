package cli

import (
	"bufio"
	"fmt"
	"io"
	"os"
	"strings"
	"time"
)

// readTail 返回文件最后 n 行（文件不存在时返回 nil）。
func readTail(path string, n int) []string {
	lines, err := readTailErr(path, n)
	if err != nil {
		return nil
	}
	return lines
}

func readTailErr(path string, n int) ([]string, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	if n <= 0 {
		n = 100
	}
	ring := make([]string, 0, n)
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 0, 64*1024), 4*1024*1024)
	for sc.Scan() {
		ring = append(ring, sc.Text())
		if len(ring) > n {
			ring = ring[1:]
		}
	}
	return ring, sc.Err()
}

// followFile 等价于 tail -f -n N：先打印末尾 N 行，再持续输出新增内容。
func followFile(c *Ctx, path string, n int) int {
	if lines, err := readTailErr(path, n); err == nil {
		for _, l := range lines {
			fmt.Fprintln(c.Out, l)
		}
	}
	f, err := os.Open(path)
	if err != nil {
		return c.fail(err)
	}
	defer f.Close()
	if _, err := f.Seek(0, io.SeekEnd); err != nil {
		return c.fail(err)
	}
	buf := make([]byte, 0, 32*1024)
	chunk := make([]byte, 8192)
	for {
		nr, err := f.Read(chunk)
		if nr > 0 {
			buf = append(buf, chunk[:nr]...)
			if idx := strings.LastIndexByte(string(buf), '\n'); idx >= 0 {
				fmt.Fprint(c.Out, string(buf[:idx+1]))
				buf = buf[idx+1:]
			}
		}
		if err == io.EOF {
			time.Sleep(200 * time.Millisecond)
			continue
		}
		if err != nil {
			return c.fail(err)
		}
	}
}
