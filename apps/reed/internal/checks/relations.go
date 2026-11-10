package checks

import (
	"fmt"
	"net/url"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
)

type Report struct {
	Errors     []string `json:"errors"`
	Checked    []string `json:"checked"`
	Unverified []string `json:"unverified"`
}

func NewReport() Report { return Report{[]string{}, []string{}, []string{}} }
func sectionText(text string) map[string][]string {
	result := map[string][]string{}
	current := ""
	ignored := []string{}
	for _, line := range Visible(text, &ignored) {
		if strings.HasPrefix(line, "## ") {
			current = line[3:]
			result[current] = []string{}
		} else if current != "" {
			result[current] = append(result[current], line)
		}
	}
	return result
}
func records(lines, columns []string, optional bool, errors *[]string, label string) []map[string]string {
	if optional && strings.HasPrefix(strings.TrimSpace(strings.Join(lines, "\n")), "不适用：") {
		return nil
	}
	rows := checkTable(lines, columns, errors, label, nil)
	result := []map[string]string{}
	for _, row := range rows {
		if len(row) != len(columns) {
			continue
		}
		item := map[string]string{}
		for i, k := range columns {
			item[k] = row[i]
		}
		result = append(result, item)
	}
	return result
}
func unique(rows []map[string]string, key string, errors *[]string) map[string]map[string]string {
	result := map[string]map[string]string{}
	for _, row := range rows {
		value := strings.Trim(row[key], "`")
		if _, ok := result[value]; ok {
			*errors = append(*errors, key+"重复："+value)
		}
		result[value] = row
	}
	return result
}
func Names(value string) []string {
	parts := regexp.MustCompile(`[、,，;；]`).Split(value, -1)
	out := []string{}
	for _, p := range parts {
		p = strings.Trim(strings.TrimSpace(p), "`")
		if p != "" {
			out = append(out, p)
		}
	}
	return out
}
func validDoc(file, kind string, schemas map[string]Schema, read Reader, r *Report) (string, error) {
	problems, err := Validate(file, kind, schemas[kind], read, "")
	if err != nil {
		return "", err
	}
	r.Errors = append(r.Errors, problems...)
	return read.Read(file)
}
func linksTo(text, source, destination string) bool {
	ignored := []string{}
	for _, m := range linkRE.FindAllStringSubmatch(strings.Join(Visible(text, &ignored), "\n"), -1) {
		u, e := url.Parse(strings.Trim(m[1], "<>"))
		if e != nil || u.Scheme != "" || u.Host != "" || u.Path == "" {
			continue
		}
		p := u.Path
		if !filepath.IsAbs(p) {
			p = filepath.Join(filepath.Dir(source), filepath.FromSlash(p))
		}
		if filepath.Clean(p) == filepath.Clean(destination) {
			return true
		}
	}
	return false
}
func Design(system, protocol string, schemas map[string]Schema, read Reader) (Report, error) {
	r := NewReport()
	st, e := validDoc(system, "system-design", schemas, read, &r)
	if e != nil {
		return r, e
	}
	pt, e := validDoc(protocol, "interface-protocol", schemas, read, &r)
	if e != nil {
		return r, e
	}
	if len(r.Errors) > 0 {
		return r, nil
	}
	ds, ps := sectionText(st), sectionText(pt)
	modules := unique(records(ds["模块与依赖"], []string{"模块", "责任层", "职责", "公开能力", "允许依赖", "能力来源"}, false, &r.Errors, "模块与依赖"), "模块", &r.Errors)
	index := unique(records(ps["协议索引"], []string{"协议标识", "能力", "提供方", "调用方", "边界类型", "交互方式"}, true, &r.Errors, "协议索引"), "协议标识", &r.Errors)
	definitions := map[string]map[string]string{}
	body := ps["协议定义"]
	for i, line := range body {
		if !strings.HasPrefix(line, "### ") {
			continue
		}
		end := len(body)
		for j := i + 1; j < len(body); j++ {
			if strings.HasPrefix(body[j], "### ") {
				end = j
				break
			}
		}
		for _, t := range Tables(body[i+1 : end]) {
			if !slices.Equal(t.Head, []string{"字段", "值"}) {
				continue
			}
			row := map[string]string{}
			for _, v := range t.Rows {
				if len(v) == 2 {
					row[v[0]] = v[1]
				}
			}
			id := strings.Trim(row["协议标识"], "`")
			if _, ok := definitions[id]; ok {
				r.Errors = append(r.Errors, "协议标识重复："+id)
			}
			definitions[id] = row
			break
		}
	}
	for id := range index {
		if _, ok := definitions[id]; !ok {
			r.Errors = append(r.Errors, "协议索引与定义不对应："+id)
		}
	}
	for id := range definitions {
		if _, ok := index[id]; !ok {
			r.Errors = append(r.Errors, "协议索引与定义不对应："+id)
		}
	}
	for id, item := range index {
		if !regexp.MustCompile(`^[A-Za-z0-9_.-]+$`).MatchString(id) {
			r.Errors = append(r.Errors, "非法协议标识："+id)
		}
		boundary := item["边界类型"]
		if !slices.Contains([]string{"模块", "外部", "持久化"}, boundary) {
			r.Errors = append(r.Errors, id+": 未知边界类型")
		}
		for _, field := range []string{"提供方", "调用方", "边界类型", "交互方式"} {
			if def, ok := definitions[id]; ok && item[field] != def[field] {
				r.Errors = append(r.Errors, id+": 索引与定义的"+field+"不一致")
			}
		}
		participates := false
		for _, field := range []string{"提供方", "调用方"} {
			for _, name := range Names(item[field]) {
				_, ok := modules[name]
				participates = participates || ok
				external := boundary == "外部" && strings.HasPrefix(name, "外部:") && len(name) > len("外部:")
				storage := boundary == "持久化" && strings.HasPrefix(name, "存储:") && len(name) > len("存储:")
				if !ok && !external && !storage {
					r.Errors = append(r.Errors, id+": "+field+"引用未定义模块或错误边界主体 "+name)
				}
			}
		}
		if !participates {
			r.Errors = append(r.Errors, id+": 协议没有当前系统的参与模块")
		}
	}
	graph := map[string][]string{}
	for name, module := range modules {
		layer := module["责任层"]
		if !slices.Contains([]string{"交互层", "业务编排层", "业务能力层", "技术实现层", "横切基础设施", "运行组装"}, layer) {
			r.Errors = append(r.Errors, name+": 未知责任层")
		}
		for _, dep := range Names(module["允许依赖"]) {
			if dep == "无" {
				continue
			}
			if strings.HasPrefix(dep, "抽象:") {
				item, ok := index[strings.TrimPrefix(dep, "抽象:")]
				if !ok || !slices.Contains(Names(item["调用方"]), name) {
					r.Errors = append(r.Errors, name+": 抽象依赖没有相符的协议与调用方 "+dep)
				} else if layer == "业务能力层" {
					for _, provider := range Names(item["提供方"]) {
						if modules[provider]["责任层"] == "业务能力层" {
							r.Errors = append(r.Errors, name+": 抽象引用仍依赖平级业务能力")
						}
					}
				}
				continue
			}
			target, ok := modules[dep]
			if !ok {
				r.Errors = append(r.Errors, name+": 依赖未定义模块 "+dep)
				continue
			}
			graph[name] = append(graph[name], dep)
			if layer == "业务能力层" && target["责任层"] == layer {
				r.Errors = append(r.Errors, name+": 平级业务能力直接依赖 "+dep)
			}
			if (layer == "业务编排层" || layer == "业务能力层") && target["责任层"] == "技术实现层" {
				r.Errors = append(r.Errors, name+": 业务直接依赖具体技术实现 "+dep)
			}
		}
	}
	visited, active := map[string]bool{}, map[string]bool{}
	var visit func(string)
	visit = func(name string) {
		if active[name] {
			r.Errors = append(r.Errors, "模块依赖存在环："+name)
			return
		}
		if visited[name] {
			return
		}
		active[name] = true
		for _, dep := range graph[name] {
			visit(dep)
		}
		delete(active, name)
		visited[name] = true
	}
	for name := range modules {
		visit(name)
	}
	owners := records(ds["状态与资源所有权"], []string{"状态或资源", "所有者", "作用域", "修改权", "生命周期"}, true, &r.Errors, "状态与资源所有权")
	unique(owners, "状态或资源", &r.Errors)
	for _, item := range owners {
		for _, field := range []string{"所有者", "修改权"} {
			if field == "修改权" && item[field] == "无" {
				continue
			}
			for _, name := range Names(item[field]) {
				if _, ok := modules[name]; !ok {
					r.Errors = append(r.Errors, item["状态或资源"]+": "+field+"引用未定义模块 "+name)
				}
			}
		}
		if len(Names(item["所有者"])) != 1 {
			r.Errors = append(r.Errors, item["状态或资源"]+": 须明确唯一责任所有者")
		}
	}
	mapped := map[string]bool{}
	for _, item := range records(ds["代码结构映射"], []string{"设计责任", "代码位置", "公开入口", "依赖约束"}, false, &r.Errors, "代码结构映射") {
		for _, name := range Names(item["设计责任"]) {
			mapped[name] = true
			if _, ok := modules[name]; !ok {
				r.Errors = append(r.Errors, "代码结构映射引用未定义模块："+name)
			}
		}
	}
	for name := range modules {
		if !mapped[name] {
			r.Errors = append(r.Errors, "模块没有代码结构映射："+name)
		}
	}
	refs := regexp.MustCompile(`协议:([A-Za-z0-9_.-]+)`).FindAllStringSubmatch(strings.Join(ds["关键协作路径"], "\n"), -1)
	if len(index) > 0 && len(refs) == 0 {
		r.Errors = append(r.Errors, "关键协作路径没有协议引用")
	}
	for _, ref := range refs {
		if _, ok := index[ref[1]]; !ok {
			r.Errors = append(r.Errors, "关键协作路径引用未定义协议："+ref[1])
		}
	}
	for _, item := range records(ds["技术选型"], []string{"技术项", "选择", "理由与取舍", "决策状态"}, true, &r.Errors, "技术选型") {
		if !slices.Contains([]string{"用户已确认", "设计建议", "待确认"}, item["决策状态"]) {
			r.Errors = append(r.Errors, item["技术项"]+": 未知决策状态")
		}
	}
	if !linksTo(st, system, protocol) || !linksTo(pt, protocol, system) {
		r.Errors = append(r.Errors, "系统设计与接口协议须以本地 Markdown 链接相互引用")
	}
	r.Checked = append(r.Checked, system, protocol)
	r.Unverified = append(r.Unverified, "业务语义与源码依赖仍需审查")
	slices.Sort(r.Errors)
	return r, nil
}
func references(text, prefix, label string, errors *[]string) []string {
	re := regexp.MustCompile(regexp.QuoteMeta(prefix) + ":([^\\s`、,，;；()（）\\[\\]。]*)")
	out := []string{}
	for _, m := range re.FindAllStringSubmatch(text, -1) {
		if !regexp.MustCompile(`^[\p{L}\p{N}_.-]+$`).MatchString(m[1]) {
			*errors = append(*errors, label+": 非法"+prefix+"标识 "+m[1])
		} else {
			out = append(out, m[1])
		}
	}
	return out
}
func Artifacts(kind, file, protocol string, schemas map[string]Schema, read Reader) (Report, error) {
	r := NewReport()
	dtype := ""
	switch kind {
	case "ui":
		dtype = "interaction-standards"
	case "test":
		dtype = "test-design"
	default:
		return r, fmt.Errorf("unknown artifacts kind %q", kind)
	}
	text, e := validDoc(file, dtype, schemas, read, &r)
	if e != nil || len(r.Errors) > 0 {
		return r, e
	}
	ignored := []string{}
	lines := Visible(text, &ignored)
	if kind == "ui" {
		ids := map[string]bool{}
		abilities := map[string][]string{}
		if protocol != "" {
			pt, e := validDoc(protocol, "interface-protocol", schemas, read, &r)
			if e != nil || len(r.Errors) > 0 {
				return r, e
			}
			for _, t := range Tables(Visible(pt, &ignored)) {
				pi, ai := slices.Index(t.Head, "协议标识"), slices.Index(t.Head, "能力")
				if pi < 0 || ai < 0 {
					continue
				}
				for _, row := range t.Rows {
					if len(row) != len(t.Head) {
						continue
					}
					id := strings.Trim(row[pi], "`")
					if ids[id] {
						r.Errors = append(r.Errors, "重复协议标识："+id)
					}
					ids[id] = true
					abilities[row[ai]] = append(abilities[row[ai]], id)
				}
			}
		}
		for _, t := range Tables(lines) {
			if !slices.Equal(t.Head, []string{"用户操作", "公共能力", "可见状态", "反馈", "后续动作"}) {
				continue
			}
			for _, row := range t.Rows {
				if len(row) != len(t.Head) {
					r.Errors = append(r.Errors, "用户操作表格列数错误")
					continue
				}
				ability, label := row[1], row[0]
				before := len(r.Errors)
				refs := references(ability, "协议", label, &r.Errors)
				pure := strings.HasPrefix(ability, "界面:") && strings.TrimSpace(strings.TrimPrefix(ability, "界面:")) != ""
				switch {
				case protocol == "" && !pure:
					r.Errors = append(r.Errors, label+": 业务操作缺少接口协议依据")
				case strings.Contains(ability, "协议:"):
					for _, id := range refs {
						if !ids[id] {
							r.Errors = append(r.Errors, label+": 引用不存在的协议 "+id)
						}
					}
					if before == len(r.Errors) {
						r.Checked = append(r.Checked, label)
					}
				case pure:
					r.Unverified = append(r.Unverified, label+": 纯界面动作，需审查是否确实不依赖业务能力")
				case len(abilities[ability]) == 1:
					r.Checked = append(r.Checked, label)
				default:
					r.Unverified = append(r.Unverified, label+": 公共能力无法唯一对应协议，人工核对")
				}
			}
		}
		r.Unverified = append(r.Unverified, "可见状态、恢复承诺和验收场景仍需内容审查")
	} else {
		ids := map[string]bool{}
		for _, line := range lines {
			if !strings.HasPrefix(line, "### 场景:") {
				continue
			}
			id := strings.TrimSpace(strings.TrimPrefix(line, "### 场景:"))
			if !regexp.MustCompile(`^[\p{L}\p{N}_.-]+$`).MatchString(id) {
				r.Errors = append(r.Errors, "非法场景定义标识："+id)
			} else {
				if ids[id] {
					r.Errors = append(r.Errors, "场景标识重复："+id)
				}
				ids[id] = true
			}
		}
		risks := map[string]bool{}
		for _, t := range Tables(lines) {
			ri, si := slices.Index(t.Head, "风险标识"), slices.Index(t.Head, "场景")
			if ri < 0 || si < 0 {
				continue
			}
			for _, row := range t.Rows {
				if len(row) != len(t.Head) {
					continue
				}
				id, scenario := row[ri], row[si]
				if risks[id] {
					r.Errors = append(r.Errors, "风险标识重复："+id)
				}
				risks[id] = true
				before := len(r.Errors)
				refs := references(scenario, "场景", id, &r.Errors)
				if strings.Contains(scenario, "场景:") {
					for _, sid := range refs {
						if !ids[sid] {
							r.Errors = append(r.Errors, id+": 场景未定义 "+sid)
						}
					}
					if before == len(r.Errors) {
						r.Checked = append(r.Checked, id)
					}
				} else {
					r.Unverified = append(r.Unverified, id+": 场景为自然语言，人工核对其定义与覆盖")
				}
			}
		}
		r.Unverified = append(r.Unverified, "风险完整性、预期正确性及替身证明能力仍需内容审查")
	}
	return r, nil
}
