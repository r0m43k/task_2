package main

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"

	"gopkg.in/yaml.v3"
)

type vErr struct {
	line     int
	msg      string
	withLine bool
}
type Ctx struct {
	file string
	errs []vErr
}

func (c *Ctx) L(line int, msg string) { c.errs = append(c.errs, vErr{line, msg, true}) }
func (c *Ctx) N(msg string)           { c.errs = append(c.errs, vErr{0, msg, false}) }
func (c *Ctx) Exit() int {
	if len(c.errs) == 0 {
		return 0
	}
	for _, e := range c.errs {
		if e.withLine && e.line > 0 {
			fmt.Fprintf(os.Stderr, "%s:%d %s\n", c.file, e.line, e.msg)
		} else {
			fmt.Fprintf(os.Stderr, "%s: %s\n", c.file, e.msg)
		}
	}
	return 1
}

func base(p string) string { return filepath.Base(p) }

func asMap(n *yaml.Node) (map[string]*yaml.Node, error) {
	if n == nil || n.Kind != yaml.MappingNode {
		return nil, errors.New("not a map")
	}
	m := make(map[string]*yaml.Node, len(n.Content)/2)
	for i := 0; i+1 < len(n.Content); i += 2 {
		m[n.Content[i].Value] = n.Content[i+1]
	}
	return m, nil
}
func asSeq(n *yaml.Node) ([]*yaml.Node, error) {
	if n == nil || n.Kind != yaml.SequenceNode {
		return nil, errors.New("not a seq")
	}
	return n.Content, nil
}
func str(n *yaml.Node) (string, bool) {
	if n != nil && n.Kind == yaml.ScalarNode {
		return n.Value, true
	}
	return "", false
}
func intv(n *yaml.Node) (int, bool) {
	if n == nil || n.Kind != yaml.ScalarNode {
		return 0, false
	}
	i, err := strconv.Atoi(strings.TrimSpace(n.Value))
	return i, err == nil
}
func portOK(p int) bool { return p > 0 && p < 65536 }

var (
	reSnake = regexp.MustCompile(`^[a-z0-9]+(?:_[a-z0-9]+)*$`)
	reImage = regexp.MustCompile(`^registry\.bigbrother\.io\/[a-z0-9][a-z0-9._\/-]*:[A-Za-z0-9][A-Za-z0-9._-]*$`)
	reMem   = regexp.MustCompile(`^\d+(?:Gi|Mi|Ki)$`)
)

func validateResMap(c *Ctx, n *yaml.Node, field string) {
	m, err := asMap(n)
	if err != nil {
		c.L(n.Line, fmt.Sprintf("%s должно быть объектом", field))
		return
	}
	if cpu, ok := m["cpu"]; ok {
		if cpu.Tag != "!!int" {
			c.L(cpu.Line, "cpu должно быть целым числом")
		}
	}
	if mem, ok := m["memory"]; ok {
		if s, ok := str(mem); !ok {
			c.L(mem.Line, "memory должно быть строкой")
		} else if !reMem.MatchString(s) {
			c.L(mem.Line, fmt.Sprintf("memory имеет неверный формат '%s'", s))
		}
	}
}
func validateResources(c *Ctx, n *yaml.Node) {
	m, err := asMap(n)
	if err != nil {
		c.L(n.Line, "resources должно быть объектом")
		return
	}
	if r, ok := m["requests"]; ok {
		validateResMap(c, r, "requests")
	}
	if l, ok := m["limits"]; ok {
		validateResMap(c, l, "limits")
	}
}
func validatePort(c *Ctx, n *yaml.Node) {
	m, err := asMap(n)
	if err != nil {
		c.L(n.Line, "ports должен быть объектом")
		return
	}
	if cp, ok := m["containerPort"]; !ok {
		c.N("containerPort обязателен")
	} else if p, ok := intv(cp); !ok {
		c.L(cp.Line, "containerPort должно быть целым числом")
	} else if !portOK(p) {
		c.L(cp.Line, "containerPort значение вне допустимого диапазона")
	}
	if pr, ok := m["protocol"]; ok {
		if s, ok := str(pr); !ok {
			c.L(pr.Line, "protocol должно быть строкой")
		} else {
			up := strings.ToUpper(s)
			if up != "TCP" && up != "UDP" {
				c.L(pr.Line, fmt.Sprintf("protocol имеет неподдерживаемое значение '%s'", s))
			}
		}
	}
}
func validateProbe(c *Ctx, n *yaml.Node, field string) {
	m, err := asMap(n)
	if err != nil {
		c.L(n.Line, fmt.Sprintf("%s должно быть объектом", field))
		return
	}
	hg, ok := m["httpGet"]
	if !ok {
		c.N("httpGet обязателен")
		return
	}
	hm, err := asMap(hg)
	if err != nil {
		c.L(hg.Line, "httpGet должен быть объектом")
		return
	}
	if p, ok := hm["path"]; !ok {
		c.N("path обязателен")
	} else if s, ok := str(p); !ok {
		c.L(p.Line, "path должно быть строкой")
	} else if !strings.HasPrefix(s, "/") {
		c.L(p.Line, fmt.Sprintf("path имеет неверный формат '%s'", s))
	}
	if pn, ok := hm["port"]; !ok {
		c.N("port обязателен")
	} else if pi, ok := intv(pn); !ok {
		c.L(pn.Line, "port должно быть целым числом")
	} else if !portOK(pi) {
		c.L(pn.Line, "port значение вне допустимого диапазона")
	}
}
func validateContainer(c *Ctx, n *yaml.Node, seen map[string]struct{}) {
	m, err := asMap(n)
	if err != nil {
		c.L(n.Line, "containers должен быть объектом")
		return
	}
	if nn, ok := m["name"]; !ok {
		c.N("name обязателен")
	} else if s, ok := str(nn); !ok {
		c.L(nn.Line, "name должно быть строкой")
	} else {
		if strings.TrimSpace(s) == "" {
			c.L(nn.Line, "name обязателен")
			return
		}
		if !reSnake.MatchString(s) {
			c.L(nn.Line, fmt.Sprintf("name имеет неверный формат '%s'", s))
		}
		if _, dup := seen[s]; dup {
			c.L(nn.Line, fmt.Sprintf("name имеет неверный формат '%s'", s))
		}
		seen[s] = struct{}{}
	}
	if in, ok := m["image"]; !ok {
		c.N("image обязателен")
	} else if s, ok := str(in); !ok {
		c.L(in.Line, "image должно быть строкой")
	} else if !reImage.MatchString(s) {
		c.L(in.Line, fmt.Sprintf("image имеет неверный формат '%s'", s))
	}
	if pn, ok := m["ports"]; ok {
		seq, err := asSeq(pn)
		if err != nil {
			c.L(pn.Line, "ports должен быть массивом")
		} else {
			for _, it := range seq {
				validatePort(c, it)
			}
		}
	}
	if r, ok := m["readinessProbe"]; ok {
		validateProbe(c, r, "readinessProbe")
	}
	if l, ok := m["livenessProbe"]; ok {
		validateProbe(c, l, "livenessProbe")
	}
	if rn, ok := m["resources"]; !ok {
		c.N("resources обязателен")
	} else {
		validateResources(c, rn)
	}
}
func validateSpec(c *Ctx, spec *yaml.Node) {
	m, err := asMap(spec)
	if err != nil {
		c.L(spec.Line, "spec должно быть объектом")
		return
	}
	if n, ok := m["os"]; ok {
		switch n.Kind {
		case yaml.ScalarNode:
			if s, ok := str(n); !ok {
				c.L(n.Line, "os должно быть строкой")
			} else if s != "linux" && s != "windows" {
				c.L(n.Line, fmt.Sprintf("os имеет неподдерживаемое значение '%s'", s))
			}
		case yaml.MappingNode:
			om, err := asMap(n)
			if err != nil {
				c.L(n.Line, "os должно быть объектом")
			} else {
				nn, ok := om["name"]
				if !ok {
					c.N("name обязателен")
				} else if s, ok := str(nn); !ok {
					c.L(nn.Line, "name должно быть строкой")
				} else if s != "linux" && s != "windows" {
					c.L(nn.Line, fmt.Sprintf("name имеет неподдерживаемое значение '%s'", s))
				}
			}
		default:
			c.L(n.Line, "os должно быть строкой")
		}
	}
	cn, ok := m["containers"]
	if !ok {
		c.N("containers обязателен")
		return
	}
	seq, err := asSeq(cn)
	if err != nil {
		c.L(cn.Line, "containers должен быть массивом")
		return
	}
	if len(seq) == 0 {
		c.L(cn.Line, "containers значение вне допустимого диапазона")
		return
	}
	seen := map[string]struct{}{}
	for _, it := range seq {
		validateContainer(c, it, seen)
	}
}
func validateMeta(c *Ctx, meta *yaml.Node) {
	m, err := asMap(meta)
	if err != nil {
		c.L(meta.Line, "metadata должно быть объектом")
		return
	}
	if n, ok := m["name"]; !ok {
		c.N("name обязателен")
	} else if _, ok := str(n); !ok {
		c.L(n.Line, "name должно быть строкой")
	}
	if n, ok := m["namespace"]; ok {
		if _, ok := str(n); !ok {
			c.L(n.Line, "namespace должно быть строкой")
		}
	}
	if n, ok := m["labels"]; ok {
		lm, err := asMap(n)
		if err != nil {
			c.L(n.Line, "labels должно быть объектом")
		} else {
			for _, v := range lm {
				if _, ok := str(v); !ok {
					c.L(v.Line, "labels должно быть строкой")
				}
			}
		}
	}
}
func validateDoc(c *Ctx, doc *yaml.Node) {
	top, err := asMap(doc)
	if err != nil {
		c.L(doc.Line, "spec должно быть объектом")
		return
	}
	if n, ok := top["apiVersion"]; !ok {
		c.N("apiVersion обязателен")
	} else if s, ok := str(n); !ok {
		c.L(n.Line, "apiVersion должно быть строкой")
	} else if s != "v1" {
		c.L(n.Line, fmt.Sprintf("apiVersion имеет неподдерживаемое значение '%s'", s))
	}
	if n, ok := top["kind"]; !ok {
		c.N("kind обязателен")
	} else if s, ok := str(n); !ok {
		c.L(n.Line, "kind должно быть строкой")
	} else if s != "Pod" {
		c.L(n.Line, fmt.Sprintf("kind имеет неподдерживаемое значение '%s'", s))
	}
	if n, ok := top["metadata"]; !ok {
		c.N("metadata обязателен")
	} else {
		validateMeta(c, n)
	}
	if n, ok := top["spec"]; !ok {
		c.N("spec обязателен")
	} else {
		validateSpec(c, n)
	}
}

func main() { os.Exit(run(os.Args)) }

func run(args []string) int {
	if len(args) != 2 {
		fmt.Fprintf(os.Stderr, "использование: %s <путь-к-yaml>\n", base(args[0]))
		return 2
	}
	path := args[1]
	data, err := os.ReadFile(path)
	if err != nil {
		fmt.Fprintf(os.Stderr, "%s: не удалось прочитать содержимое файла: %v\n", base(path), err)
		return 1
	}
	var root yaml.Node
	if err := yaml.Unmarshal(data, &root); err != nil {
		fmt.Fprintf(os.Stderr, "%s: не удалось разобрать содержимое файла: %v\n", base(path), err)
		return 1
	}
	ctx := &Ctx{file: base(path)}
	if len(root.Content) == 0 {
		ctx.N("spec обязателен")
		return ctx.Exit()
	}
	for _, doc := range root.Content {
		validateDoc(ctx, doc)
	}
	return ctx.Exit()
}
