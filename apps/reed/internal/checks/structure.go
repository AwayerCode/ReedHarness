package checks

import (
	"fmt"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
)

type PathReader interface {
	Reader
	PathKind(string) (string, error)
}

func structurePath(root, value string) (string, error) {
	value = strings.Trim(strings.TrimSpace(value), "`")
	if value == "" || regexp.MustCompile(`[\s*{}<>]`).MatchString(value) || filepath.IsAbs(value) {
		return "", fmt.Errorf("无法直接检查的相对路径：%s", value)
	}
	p := filepath.Join(root, filepath.FromSlash(value))
	rel, e := filepath.Rel(root, p)
	if e != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return "", fmt.Errorf("路径越出项目：%s", value)
	}
	return p, nil
}
func Structure(root, design string, scopes []string, schemas map[string]Schema, read PathReader) (Report, error) {
	r := NewReport()
	text, e := validDoc(design, "system-design", schemas, read, &r)
	if e != nil || len(r.Errors) > 0 {
		return r, e
	}
	r.Unverified = append(r.Unverified, "不解析源码依赖、导出内容、路由配置或业务逻辑，须继续审查")
	selected := []string{}
	for _, scope := range scopes {
		p, e := structurePath(root, scope)
		if e != nil {
			return r, e
		}
		selected = append(selected, p)
	}
	tables := []Table{}
	for _, t := range Tables(sectionText(text)["代码结构映射"]) {
		if slices.Equal(t.Head, []string{"设计责任", "代码位置", "公开入口", "依赖约束"}) || slices.Equal(t.Head, []string{"设计责任", "代码位置", "公共入口", "约束"}) {
			tables = append(tables, t)
		}
	}
	if len(tables) != 1 {
		r.Errors = append(r.Errors, "设计缺少可检查的代码结构映射表")
		return r, nil
	}
	seen := map[string]string{}
	for _, row := range tables[0].Rows {
		label, raw, entry := row[0], row[1], row[2]
		location, e := structurePath(root, raw)
		if e != nil {
			r.Unverified = append(r.Unverified, label+": "+e.Error())
			continue
		}
		match := len(selected) == 0
		for _, scope := range selected {
			match = match || location == scope || strings.HasPrefix(location, scope+string(filepath.Separator)) || strings.HasPrefix(scope, location+string(filepath.Separator))
		}
		if !match {
			continue
		}
		if prior, ok := seen[location]; ok && prior != label {
			r.Errors = append(r.Errors, "同一路径映射多个责任："+raw)
		}
		seen[location] = label
		r.Checked = append(r.Checked, label+": 代码位置 "+raw)
		kind, e := read.PathKind(location)
		if e != nil {
			return r, e
		}
		if kind == "missing" {
			r.Errors = append(r.Errors, label+": 设计声明的位置不存在 "+raw)
			continue
		}
		if entry == "无" {
			continue
		}
		if !strings.Contains(entry, "/") && filepath.Ext(strings.Trim(entry, "`")) == "" {
			r.Unverified = append(r.Unverified, label+": 公开入口是符号或说明，需人工核对")
			continue
		}
		public, e := structurePath(root, entry)
		if e != nil {
			r.Unverified = append(r.Unverified, label+": "+e.Error())
			continue
		}
		r.Checked = append(r.Checked, label+": 公开入口 "+entry)
		pk, e := read.PathKind(public)
		if e != nil {
			return r, e
		}
		if pk != "file" {
			r.Errors = append(r.Errors, label+": 公开入口文件不存在 "+entry)
		}
		base := location
		if kind != "directory" {
			base = filepath.Dir(location)
		}
		if public != location && !strings.HasPrefix(public, base+string(filepath.Separator)) {
			r.Errors = append(r.Errors, label+": 公开入口位于代码位置之外，需确认设计约定 "+entry)
		}
	}
	if len(r.Checked) == 0 {
		r.Errors = append(r.Errors, "当前范围没有可检查的结构映射")
	}
	return r, nil
}
