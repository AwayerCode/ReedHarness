package checks

import (
	"fmt"
	"net/url"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"unicode"
)

type Section struct {
	Heading string              `json:"heading"`
	Columns []string            `json:"columns"`
	Enums   map[string][]string `json:"enums"`
}
type Schema struct {
	Title        string              `json:"title"`
	Sections     []Section           `json:"sections"`
	Profiles     map[string][]string `json:"profiles"`
	RecordFields []string            `json:"record_fields"`
}
type Reader interface {
	Read(string) (string, error)
	Exists(string) (bool, error)
}

var fenceRE = regexp.MustCompile("^\\s*(`{3,}|~{3,})(.*)$")
var headingRE = regexp.MustCompile(`^(#{1,6}) (.+?)\s*$`)
var separatorRE = regexp.MustCompile(`^\s*\|(?:\s*:?-{3,}:?\s*\|)+\s*$`)
var placeholderRE = regexp.MustCompile(`\{\{[^\n{}]+\}\}|\{填写[^\n{}]*\}`)
var linkRE = regexp.MustCompile(`!?\[[^\]\n]*\]\((<[^>]+>|[^\s)]+)\)`)

func Visible(text string, errors *[]string) []string {
	lines := strings.Split(strings.ReplaceAll(text, "\r\n", "\n"), "\n")
	fence := ""
	for i, line := range lines {
		m := fenceRE.FindStringSubmatch(line)
		if m != nil {
			if fence == "" {
				fence = m[1]
				if strings.TrimSpace(m[2]) == "" {
					*errors = append(*errors, fmt.Sprintf("%d: 代码块必须声明语言", i+1))
				}
			} else if m[1][0] == fence[0] && len(m[1]) >= len(fence) && strings.TrimSpace(m[2]) == "" {
				fence = ""
			}
			lines[i] = ""
		} else if fence != "" {
			lines[i] = ""
		}
	}
	if fence != "" {
		*errors = append(*errors, "代码块未关闭")
	}
	return lines
}

type Table struct {
	Head []string
	Rows [][]string
}

func cells(line string) []string {
	line = strings.Trim(strings.TrimSpace(line), "|")
	out := []string{}
	start := 0
	for i := 0; i < len(line); i++ {
		if line[i] == '|' && (i == 0 || line[i-1] != '\\') {
			out = append(out, strings.ReplaceAll(strings.TrimSpace(line[start:i]), `\|`, `|`))
			start = i + 1
		}
	}
	return append(out, strings.ReplaceAll(strings.TrimSpace(line[start:]), `\|`, `|`))
}
func Tables(lines []string) []Table {
	out := []Table{}
	for i := 0; i+1 < len(lines); {
		if strings.HasPrefix(strings.TrimSpace(lines[i]), "|") && separatorRE.MatchString(lines[i+1]) {
			t := Table{Head: cells(lines[i]), Rows: [][]string{}}
			i += 2
			for i < len(lines) && strings.HasPrefix(strings.TrimSpace(lines[i]), "|") {
				t.Rows = append(t.Rows, cells(lines[i]))
				i++
			}
			out = append(out, t)
		} else {
			i++
		}
	}
	return out
}
func checkTable(lines, columns []string, errors *[]string, label string, enums map[string][]string) [][]string {
	candidates := []Table{}
	for _, t := range Tables(lines) {
		if slices.Equal(t.Head, columns) {
			candidates = append(candidates, t)
		}
	}
	if len(candidates) != 1 {
		*errors = append(*errors, label+": 必须有且仅有一个匹配字段的表格")
		return nil
	}
	rows := candidates[0].Rows
	if len(rows) == 0 {
		*errors = append(*errors, label+": 表格必须包含数据行")
	}
	for _, r := range rows {
		if len(r) != len(columns) || slices.Contains(r, "") {
			*errors = append(*errors, label+": 表格列数错误或存在空字段")
			continue
		}
		for field, values := range enums {
			idx := slices.Index(columns, field)
			if idx < 0 || !slices.Contains(values, r[idx]) {
				*errors = append(*errors, label+": "+field+" 枚举值无效")
			}
		}
	}
	return rows
}
func first(rows [][]string) []string {
	out := []string{}
	for _, r := range rows {
		if len(r) > 0 {
			out = append(out, r[0])
		}
	}
	return out
}
func slug(title string) string {
	return strings.ReplaceAll(strings.Map(func(r rune) rune {
		if unicode.IsLetter(r) || unicode.IsNumber(r) || unicode.IsSpace(r) || r == '_' || r == '-' {
			return r
		}
		return -1
	}, strings.ToLower(title)), " ", "-")
}
func Validate(file, kind string, schema Schema, read Reader, profile string) ([]string, error) {
	errors := []string{}
	text, err := read.Read(file)
	if err != nil {
		return nil, err
	}
	text = strings.ReplaceAll(text, "\r\n", "\n")
	if profile != "" {
		owned, ok := schema.Profiles[profile]
		if !ok || len(owned) == 0 {
			return nil, fmt.Errorf("文档类型 %s 不支持局部校验 %s", kind, profile)
		}
		sections := []Section{}
		for _, s := range schema.Sections {
			if slices.Contains(owned, s.Heading) {
				sections = append(sections, s)
			}
		}
		schema.Sections = sections
		raw := strings.Split(text, "\n")
		clean := Visible(text, &errors)
		selected := []string{}
		keep := true
		for i, l := range raw {
			if strings.HasPrefix(clean[i], "## ") {
				keep = slices.Contains(owned, strings.TrimSpace(clean[i][3:]))
			}
			if keep {
				selected = append(selected, l)
			}
		}
		if !strings.HasSuffix(text, "\n") || strings.HasSuffix(text, "\n\n") {
			errors = append(errors, "文件末尾必须恰好一个换行")
		}
		text = strings.TrimRightFunc(strings.Join(selected, "\n"), unicode.IsSpace) + "\n"
	}
	if !strings.HasSuffix(text, "\n") || strings.HasSuffix(text, "\n\n") {
		errors = append(errors, "文件末尾必须恰好一个换行")
	}
	if placeholderRE.MatchString(text) {
		errors = append(errors, "存在未替换的模板占位符")
	}
	lines := Visible(text, &errors)
	type heading struct {
		i, level int
		title    string
	}
	heads := []heading{}
	h1, h2 := []string{}, []string{}
	previous := 0
	for i, l := range lines {
		m := headingRE.FindStringSubmatch(l)
		if m == nil {
			continue
		}
		level := len(m[1])
		heads = append(heads, heading{i, level, m[2]})
		if level == 1 {
			h1 = append(h1, m[2])
		}
		if level == 2 {
			h2 = append(h2, m[2])
		}
		if level > previous+1 {
			errors = append(errors, fmt.Sprintf("%d: 标题层级跳跃", i+1))
		}
		previous = level
		if i > 0 && strings.TrimSpace(lines[i-1]) != "" {
			errors = append(errors, fmt.Sprintf("%d: 标题前缺少空行", i+1))
		}
		if i+1 < len(lines) && strings.TrimSpace(lines[i+1]) != "" {
			errors = append(errors, fmt.Sprintf("%d: 标题后缺少空行", i+1))
		}
	}
	if !slices.Equal(h1, []string{schema.Title}) {
		errors = append(errors, "必须只有一个一级标题："+schema.Title)
	}
	expected := []string{}
	for _, s := range schema.Sections {
		expected = append(expected, s.Heading)
	}
	if !slices.Equal(h2, expected) {
		errors = append(errors, "二级章节必须完整且按契约顺序")
	}
	chunks := map[string][]string{}
	for i, h := range heads {
		if h.level != 2 {
			continue
		}
		end := len(lines)
		for _, next := range heads[i+1:] {
			if next.level == 2 {
				end = next.i
				break
			}
		}
		chunks[h.title] = lines[h.i+1 : end]
	}
	for _, s := range schema.Sections {
		body := chunks[s.Heading]
		content := []string{}
		for _, l := range body {
			if !strings.HasPrefix(l, "#") {
				content = append(content, l)
			}
		}
		value := strings.TrimSpace(strings.Join(content, "\n"))
		if value == "" {
			errors = append(errors, s.Heading+": 内容不能为空")
		}
		na := strings.HasPrefix(value, "不适用：") && strings.TrimSpace(strings.TrimPrefix(value, "不适用：")) != ""
		if s.Columns != nil && !(na && s.Heading != "文档信息") {
			checkTable(body, s.Columns, &errors, s.Heading, s.Enums)
		}
	}
	metadata := []Table{}
	for _, t := range Tables(chunks["文档信息"]) {
		if slices.Equal(t.Head, []string{"字段", "值"}) {
			metadata = append(metadata, t)
		}
	}
	if len(metadata) == 1 {
		rows := metadata[0].Rows
		values := map[string]string{}
		for _, r := range rows {
			if len(r) == 2 {
				values[r[0]] = r[1]
			}
		}
		if !slices.Equal(first(rows), []string{"类型", "任务", "范围", "依据", "状态"}) {
			errors = append(errors, "文档信息字段缺失、重复或顺序错误")
		}
		if values["类型"] != kind {
			errors = append(errors, "文档信息类型与所选契约不一致")
		}
		if !slices.Contains([]string{"草案", "待验收", "已验收"}, values["状态"]) {
			errors = append(errors, "状态必须为草案、待验收或已验收")
		}
	}
	if schema.RecordFields != nil {
		body := chunks["协议定义"]
		records := []int{}
		for i, l := range body {
			if strings.HasPrefix(l, "### ") {
				records = append(records, i)
			}
		}
		if len(records) == 0 && !strings.HasPrefix(strings.TrimSpace(strings.Join(body, "\n")), "不适用：") {
			errors = append(errors, "协议定义: 必须有能力三级标题及字段表")
		}
		for i, start := range records {
			end := len(body)
			if i+1 < len(records) {
				end = records[i+1]
			}
			rows := checkTable(body[start+1:end], []string{"字段", "值"}, &errors, body[start], nil)
			if !slices.Equal(first(rows), schema.RecordFields) {
				errors = append(errors, body[start]+": 协议字段缺失、重复或顺序错误")
			}
		}
	}
	for _, m := range linkRE.FindAllStringSubmatch(strings.Join(lines, "\n"), -1) {
		target := strings.Trim(m[1], "<>")
		u, e := url.Parse(target)
		if e != nil {
			return nil, e
		}
		if u.Scheme != "" || u.Host != "" {
			continue
		}
		local := file
		if u.Path != "" {
			if filepath.IsAbs(u.Path) {
				local = u.Path
			} else {
				local = filepath.Join(filepath.Dir(file), filepath.FromSlash(u.Path))
			}
		}
		exists, e := read.Exists(local)
		if e != nil {
			return nil, e
		}
		if !exists {
			errors = append(errors, "本地引用不存在："+target)
			continue
		}
		if u.Fragment != "" && filepath.Ext(local) == ".md" {
			other, e := read.Read(local)
			if e != nil {
				return nil, e
			}
			ignored := []string{}
			anchors := []string{}
			for _, l := range Visible(other, &ignored) {
				h := headingRE.FindStringSubmatch(l)
				if h != nil {
					anchors = append(anchors, slug(h[2]))
				}
			}
			if !slices.Contains(anchors, u.Fragment) {
				errors = append(errors, "章节引用不存在："+target)
			}
		}
	}
	return errors, nil
}
