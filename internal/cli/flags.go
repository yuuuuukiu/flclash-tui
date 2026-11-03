package cli

import (
	"strings"
)

// argSpec 描述一次子命令调用的参数解析规则。
//
// 约定：
//   - 布尔开关用 boolFlags 声明（如 "activate"、"all"）；
//   - 其余 --name value / --name=value 都当作带值选项，值统一存进 vals；
//   - 支持短别名（alias 里 "a" -> "activate"）；
//   - 位置参数与选项可以任意交错（脚本里更顺手）。
type argSpec struct {
	boolFlags map[string]bool   // 长名 -> 默认值
	alias     map[string]string // 短名 -> 长名
	short     map[string]string // 短名 -> 长名（带值）
}

type parsedArgs struct {
	pos   []string
	vals  map[string]string
	bools map[string]bool
}

func (p *parsedArgs) has(name string) bool {
	_, ok := p.vals[name]
	return ok
}

func (p *parsedArgs) get(name, def string) string {
	if v, ok := p.vals[name]; ok {
		return v
	}
	return def
}

func (p *parsedArgs) boolOf(name string) bool {
	if v, ok := p.bools[name]; ok {
		return v
	}
	return false
}

// parseInt 解析整数选项，缺省时返回 def。
func (p *parsedArgs) parseInt(name string, def int) (int, error) {
	raw, ok := p.vals[name]
	if !ok {
		return def, nil
	}
	return atoi(raw, name)
}

func (s argSpec) parse(argv []string) (*parsedArgs, error) {
	out := &parsedArgs{vals: map[string]string{}, bools: map[string]bool{}}
	for k, v := range s.boolFlags {
		out.bools[k] = v
	}
	for i := 0; i < len(argv); i++ {
		a := argv[i]
		if a == "--" {
			out.pos = append(out.pos, argv[i+1:]...)
			break
		}
		if !strings.HasPrefix(a, "-") || a == "-" {
			out.pos = append(out.pos, a)
			continue
		}
		name := strings.TrimLeft(a, "-")
		value := ""
		hasValue := false
		if idx := strings.IndexByte(name, '='); idx >= 0 {
			value, hasValue = name[idx+1:], true
			name = name[:idx]
		}
		// 短别名归一
		if len(name) == 1 {
			if long, ok := s.alias[name]; ok {
				name = long
			} else if long, ok := s.short[name]; ok {
				name = long
			}
		}
		if _, ok := s.boolFlags[name]; ok {
			if hasValue {
				switch strings.ToLower(value) {
				case "1", "true", "yes", "on":
					out.bools[name] = true
				case "0", "false", "no", "off":
					out.bools[name] = false
				default:
					return nil, usageError("选项 --%s 只接受布尔值", name)
				}
				continue
			}
			out.bools[name] = true
			continue
		}
		if hasValue {
			out.vals[name] = value
			continue
		}
		if i+1 >= len(argv) {
			return nil, usageError("选项 --%s 缺少取值", name)
		}
		i++
		out.vals[name] = argv[i]
	}
	return out, nil
}

// atoi 把字符串转成整数，错误信息里带上选项名。
func atoi(s, name string) (int, error) {
	n := 0
	if s == "" {
		return 0, usageError("选项 --%s 需要一个整数", name)
	}
	neg := false
	for i, r := range s {
		if i == 0 && (r == '-' || r == '+') {
			neg = r == '-'
			continue
		}
		if r < '0' || r > '9' {
			return 0, usageError("选项 --%s 需要一个整数，收到 %q", name, s)
		}
		n = n*10 + int(r-'0')
	}
	if neg {
		n = -n
	}
	return n, nil
}

// parseNoOpts 用于没有声明任何选项的子命令（位置参数照常收集）。
func parseNoOpts(argv []string) (*parsedArgs, error) {
	return argSpec{}.parse(argv)
}
