# 领域 Skill 与 CLI 配套

领域 Skill 定义方法、产物所有权与判断标准，项目设计提供具体规则，CLI 检查已编码约束，项目测试和运行工具提供行为证据。CLI 不选择任务顺序、不授予写入权限，也不以退出码证明整个任务完成。

## 安装与版本

CLI 独立发行 `swe-cli_<os>_<arch>.tar.gz`，归档包含 `swe-cli/bin/` 与版本摘要清单。校验发行摘要后将二进制目录加入 PATH，通过 `swe-cli version` 确认版本；Windows 使用 `swe-cli.exe`。Skill 是独立纯文本包，不携带二进制，也不绑定其他 Skill 的版本。项目记录所用 CLI 版本与来源；进行中的范围基线继续使用创建它的工具，不通过升级重建基线掩盖改动。

Skill 可用 `reed install --source <Skill包URL> --version tag:<标签> --path skills/<skill-name>` 独立安装。插件管理的资源通过宿主更新，不覆盖宿主管理目录。CLI 缺失或版本不兼容时报告无法检查，按项目约定安装独立 CLI。

## 选择检查

| 任务对象 | 检查 | 结论边界 |
| --- | --- | --- |
| 业务模型 | `documents --kind business-model` | 格式与本地链接，抽象与规则仍由领域判断 |
| YAML 层／模块设计 | `system-design validate`，局部层设计加 `--profile layers` | Schema、引用、路径冲突与声明依赖，不要求代码已存在 |
| YAML 设计视图 | `system-design serve` / `render` | serve 提供固定地址实时服务，DSH 使用架构页签；render 仅用于离线导出，旧版 preview 保留兼容，均不代表实现验收 |
| YAML 下的生产代码或测试 | `system-design check` | 当前 YAML 作用域的目录与文件归属、必要文件始终检查；源码依赖按匹配到的语言适配器检查，当前内置 Go、TypeScript 和 C++ |
| Markdown 系统设计及协议 | `documents`、`design` | 约定格式与显式关系，不等同 YAML 检查 |
| Markdown 代码映射 | `structure` | 显式位置与入口，不解析源码依赖 |
| 测试设计 | `documents --kind test-design`、`artifacts --kind test` | 风险、场景显式引用；断言与行为由测试验证 |
| 任意领域写入／只读审查 | `scope snapshot/check` 或 `scope readonly` | Git 工作区范围与指纹，不是权限隔离 |
| 构建、测试与部署 | 项目已有工具 | CLI 不替代语言工具、测试执行器和部署平台 |

YAML 文件统一命名 `system-design.yaml`，每份只负责所在目录；`list` 只发现作用域，不解析或校验 YAML 内容，成功不代表设计合法；`--file` 单独检查，`validate/check --all` 批量检查。父级排除子设计目录，越界声明和已识别的本地跨作用域导入报错；隐藏目录及依赖目录也参与作用域发现，仅排除 Git 元数据；具体规则和 Go workspace 限制见 [作用域规范](system-design/guide.md#发现与目录作用域)。

只运行与当前对象相关的检查。YAML 项目不为满足旧 `design` 或 `structure` 命令复制 Markdown 架构表格。独立协议可运行 `documents --kind interface-protocol`，YAML 与协议的关联目前需要人工核对。`check` 的结构结论与源码依赖结论分开理解：没有语言适配器不等于结构检查失败，但导入或引用关系仍未验证；检测到语言但适配器无法可靠解析时返回无法检查。不支持的 workspace、构建结构或关系说明不当作通过。

## 命令入口

以下 `<CLI>` 指 PATH 中独立安装的 `swe-cli`，所有命令显式指定目标根目录，文件一般使用相对项目路径。

```bash
<CLI> version
<CLI> documents --root <项目根目录> --kind business-model --file <模型路径>
<CLI> system-design list --root <项目根目录>
<CLI> system-design validate --root <项目根目录> --all
<CLI> system-design check --root <项目根目录> --all
<CLI> system-design validate --root <项目根目录> --file <YAML路径>
<CLI> system-design validate --root <项目根目录> --file <YAML路径> --profile layers
<CLI> system-design check --root <项目根目录> --file <YAML路径>
<CLI> system-design preview --root <项目根目录> --file <YAML路径> --expect-version <固定版本>
<CLI> system-design render --root <项目根目录> --file <YAML路径>
<CLI> design --root <项目根目录> --system-design <Markdown路径> --interface-protocol <协议路径>
<CLI> structure --root <项目根目录> --design <Markdown路径> --scope <相对路径>
<CLI> documents --root <项目根目录> --kind test-design --file <测试设计路径>
<CLI> artifacts --root <项目根目录> --kind test --file <测试设计路径>
```

CLI 中已有交互文档检查能力保留兼容。渲染写入项目文件是架构领域的明确写入动作，须纳入范围，使用临时输出成功后替换，避免错误诊断覆盖原图；用户请求查看时的目标选择、项目外缓存、多图汇总与宿主打开统一遵循[查看与展示流程](system-design/guide.md#查看与展示流程)；CLI 不负责启动浏览器。

## 范围与结果

范围命令只验证实际文件边界，不授予权限。混合源码目录不能直接作为代码或测试的整目录写入范围；共享配置条目需要差异复核，CLI 目前只做到文件粒度。

```bash
<CLI> scope snapshot --root <Git根目录> --baseline <项目外全新文件> --allow-file <相对路径>
<CLI> scope check --root <Git根目录> --baseline <原文件>
<CLI> scope readonly --root <Git根目录>
<CLI> scope readonly --root <Git根目录> --expected <完整原指纹>
```

`allow-file` 或 `allow-dir` 可重复，不能通过扩大原基线消除违规。范围检查需要 Git，包含跟踪与非忽略文件和子模块，显式可写范围还覆盖其忽略文件；不初始化或拉取子模块，不允许父仓范围跨入子模块。

退出码 0 表示本次覆盖通过，1 表示违规，2 表示无法检查。`documents/design/artifacts/structure` 可由 JSON 协议版本 2 的 `check --request` 调用，结果数组为 `checks`，每项包含 `status`、`diagnostics`、`evidence` 和 `coverage`，`unverified` 为可选字段。输入观察不是原子快照。

`system-design` 是独立入口，单份检查及参数、文件读取等失败使用 `swe-system-design-v1` 结果，包含设计摘要、诊断、观察依赖和覆盖边界；它没有旧请求的输入稳定性复核，应在静止工作区运行。`render` 成功输出 HTML，检查或生成失败输出同一架构 JSON；输出通道自身不可写时只能以退出码 2 报告，不能保证诊断可送达。必须同时检查退出码。未能完整读取设计时 `design_sha256` 为空。`scope` 也使用独立格式。不要按一套 JSON 字段解析全部命令，不把工具成功扩大为语义、行为或部署完成。

列表与批量结果使用 `swe-system-design-batch-v1`，`scopes` 列出作用域。`list` 的 `results` 固定为空数组；`validate/check --all` 的 `results` 与 `scopes` 数量、顺序及作用域身份一一对应，包含失败报告；某份失败仍检查其余文件，整体退出码优先为 2、其次 1、全部通过才为 0。未发现 YAML 返回 2。单份报告的 `scope` 标明实际目录及排除的子作用域；`--root` 不能扩大单份 YAML 的范围。

批量输出的 `discovery_issues` 记录无法读取的路径，检查继续执行其余分支，整体返回 2。边界不确定的作用域标为 `unavailable`。`check` 在没有匹配语言适配器时可以返回结构通过，同时必须在 `unverified` 中说明源码依赖未检查。Go 适配器的本地模块定位错误不能当作外部依赖，只有能确定归属的导入继续检查。

递归遍历不跟随遇到的目录符号链接。显式 `--root` 及 `--file` 的父目录会解析到真实目录，报告使用真实路径；这不允许 YAML 内的源码路径借符号链接越界，也不接受 YAML 文件本身为符号链接。
