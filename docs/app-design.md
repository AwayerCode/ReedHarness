# 应用设计

## 定位与当前状态

本文负责 `reed` 管理与调度服务、`swe` 独立 CLI 的职责、命令、运行机制与交互协议。业务概念、状态归属、Loop 与硬规则以[系统设计](system-design.md)为准；构建、安装与宿主打包见[分发设计](distribution-design.md)。

源码已拆分为 `apps/reed/` 和 `apps/swe/`。`reed serve` 提供管理页面与事件、定时调度基础；`swe` 保留专业检查、架构渲染和兼容架构服务。已内置 Codex 非交互接入，并支持自定义宿主协议；GitHub 建任务、证据回写和任务状态推进通过持久化同步记录执行。本地端到端测试使用真实 Git 工作区和 GitHub 替身；真实 GitHub 账号验收不由这些测试代替。

## 模块职责与依赖

| 应用 | 职责 | 公开能力与边界 |
| --- | --- | --- |
| `reed` | 管理 | 目标、任务、Skill、运行的管理页面与操作入口；GitHub 保持业务状态权威 |
| `reed` | 调度 | 接收事件与定时触发、读取状态、调度判断或工作、落实决策并记录结果 |
| `reed` | 运行约束 | 核对已确认的目标、任务授权、预算和停止条件；不持有 SWE 专业规则 |
| `reed` | GitHub 与 Agent 接入 | 读写业务记录，启动、观察和停止 Agent，关联结果、会话与资源信息 |
| `reed` | 领域工具接入 | 调用 `swe` 等工具，记录工具版本、诊断、覆盖范围及证据，不自行解释为任务完成 |
| `swe` | 专业检查 | 文档、架构、产物关系、目录与文件归属、语言依赖及改动范围检查 |
| `swe` | 架构展示 | 设计发现、诊断与渲染，迁移阶段继续提供独立架构服务 |
| SWE Skills | 专业能力 | 组织知识、方法和工具用法，供 Agent 按任务选择，不依赖 `reed` 服务 |

`reed` 通过外部命令与结构化结果使用 `swe`，不导入其内部 Go 包。两套应用各自组装、构建与测试。`swe` 不依赖 `reed` 的运行状态；Agent 也可以直接调用它。只在真实接入缺口出现时增加适配，不预建通用插件框架。

## 代码归属与迁移

`apps/reed/internal/control/` 保存管理 HTTP、事件队列、运行记录、GitHub 与命令接入；管理页面编入服务。`apps/swe/internal/app/` 保存专业命令，`checks/`、`workspace/`、`systemdesign/` 保存检查规则、范围与架构能力，`resources/` 保存工具自有 Schema。`internal/skillpackage/` 留在 SWE 应用，负责领域工具与 Skill 打包。

已移除 `internal/skills/` 和 `internal/genskills/` 的 Skill 编译绑定。两套 Go 模块没有源码依赖，各自维护构建与验证脚本；`skills/` 与 `plugins/` 保留知识源码和宿主适配职责。

## 管理服务与页面

`reed serve --config <JSON> [--listen 127.0.0.1:47832]` 提供目标、任务、Skill、运行及系统设计页面。仅监听 IPv4 回环；检查 Host、Origin 与跨站来源；POST 要求同源读取的随机管理令牌和 JSON。令牌用于防跨站请求，本地操作系统账户仍是信任边界，不提供远程多用户认证。

目标与任务页从配置的 GitHub Project 和仓库读取，支持编辑已有目标与任务；写入还要求 `allow_github_write`。目标定义保存后需要重新确认。Skill 页读取配置目录中的来源、版本与内容，不编辑副本。运行页触发工作、查看结果、暂停、恢复、停止和核对中断。系统设计页通过固定版本的 `swe` 发现和渲染架构，以受限 iframe 展示 HTML，不读取任意磁盘 HTML。

GET 入口为 `/api/state`、`/api/goal`、`/api/tasks`、`/api/task?number=`、`/api/skills`、`/api/skill?name=&file=`、`/api/designs` 与 `/architecture?file=`。POST 入口为 `/api/event`、`/api/control` 和 `/api/edit`。配置、请求及 Agent 协议示例见 [reed 使用说明](../apps/reed/README.md)。

## 命令与工具交互

独立路径是 Agent 读取 SWE Skill 后直接调用 `swe`，不要求启动 `reed`。调度路径是 `reed` 根据状态调度 Agent 或调用专业检查，再将结果作为后续判断的证据。

`reed` 调用 `swe` 时指定工作区、设计或产物及工具版本，读取退出码和对应命令的结构化结果。退出码 1 表示本次检查发现违规，2 表示无法检查；均不能被当作通过。渲染成功返回 HTML，失败按现有架构协议返回诊断，不能统一按普通 JSON 检查结果解析。保留检查对应的对象和版本，相关输入变化后重新判断证据有效性。

调用失败、超时或取消不意味着已有副作用撤销；涉及范围基线或导出文件时先核对结果再决定是否重试。外部进程输出分别限制为 8 MiB；超时取消进程树，保留未知结果供核对。工具版本、检查命令与运行配置固定绑定，不能通过 Agent 决策替换。

## Skill 来源与管理

`skills/` 是领域 Skill 的唯一源码来源，Skill 文本包与宿主插件从同一来源构建。`reed` 按明确来源加载 Skill 并记录来源与版本；管理先做查看和选择，编辑修改源码，不在服务中复制一套可编辑内容。`swe` 不承担 Skill 目录管理或内嵌 SWE Skill 的职责。

Skill 使用独立文本包或源码目录；`swe` 不提供 `skills list/read`。`go generate` 只生成工具自有 Schema。

## 事件与定时调度

每次事件或定时触发都定位相关 Project、Issue 与运行记录，读取当前状态并检查运行条件。确定性响应按规则处理，需要业务判断时调度 Agent；其结果形成新的触发，`reed` 核对约束后落实下一项调度。每轮只推进当前需要的工作，不等待规划、执行、检查、审查和目标评价全部完成。

触发必须能关联目标、任务及对应运行。相同工作已有有效运行时，重复或同时到达的触发不能再启动一份。事件不能直接覆盖当前状态；过期结果须核对所属运行和任务后再处理。管理 POST 接收显式事件标识和 plan/execute/check/review/evaluate 工作类型；`interval_seconds` 大于零时周期产生 evaluate 事件。单服务串行调度，相同事件标识只接受一次，同一 Issue 已排队或执行时拒绝重复工作。Agent 的 continue 结果产生新的事件，wait 等待后续触发，needs_input 暂停，只有 evaluate 重新运行目标效果检查、全部通过并携带效果证据才能返回 done。

运行记录原子写入 `state_dir/runs.json`，结果与后续事件一次保存；独占锁避免同一状态目录双重调度。运行目录绑定完整配置，变更配置使用新的授权运行目录。重启发现未结束工作、进程取消或启动后的未知结果，标记 interrupted，人工记录核对证据后才能恢复，不自动重放。暂停只停止后续调度；停止同时取消当前宿主命令并取消排队工作。Unix 取消进程组，Windows 请求终止进程树；外部宿主仍须负责其远程工作生命周期。`max_runs` 限制累计运行次数，`run_timeout_seconds` 限制每次运行时间，不宣称控制模型 token 或费用。

## GitHub 与 Agent 接入

GitHub 接入以系统设计中的状态归属为依据，只读取本次指定的 Project 和仓库范围。规划或评价可提出任务；服务核对授权后创建 Issue 并加入 Project。任务结果写评论，验收通过后更新 Project 状态并关闭 Issue；目标评价另存为 Project 草稿条目，不覆盖 README 定义。目标定义与运行进展分开处理；已确认的目标定义变化时，停止后续相关调度，待确认后继续。普通评论不构成授权变更。

Agent 接入接收目标或任务上下文、所需 Skill、允许的能力与工作区，返回工作结果、决策理由和证据引用。业务决策需要明确表达继续、等待、请人决定或结束等意图，不能把进程成功退出当成任务完成。独立审查使用独立会话，依据原始验收条件与交付证据。

内置 Codex 接入使用全新非交互会话，忽略用户配置与规则，并从宿主事件取得真实会话标识。工作目录为独立临时目录，execute 只额外开放任务授权目录，plan/review/evaluate 使用只读沙箱；禁止审批提权与工具网络访问。具体字段见 reed 使用说明。权限隔离复用宿主能力；约束模块只承诺可实际核对和落实的授权与预算指标。凭据不写入共享源码。

## 运行状态、工作区与恢复

`reed` 本地记录运行标识、触发处理情况、目标定义标识、Agent 会话、工作区、检查点及待回写结果。GitHub 中的业务状态仍是权威来源，本地记录不扩展为 Goal/Task 数据库。

调度前准备任务工作区并核对权限。中断后先核对 Agent 运行状态及已有交付，再决定继续；外部操作结果未知时先查询，不直接重复操作。GitHub 回写失败保留待同步结果，不重新执行已完成工作。读取不到必要业务状态时不推进相关工作。本地记录丢失后不承诺恢复原会话。

任务从干净 Git HEAD 创建独立本地克隆，不修改源工作区；工作区保存在运行目录，供交付和恢复。Issue 标题、正文摘要固定验收版本，执行后不再直接修改任务定义；需要改变验收时创建新任务。

execute 交付后只能形成同任务 check 事件；check 执行目标授权的命令，失败回到当前任务修正，通过才进入 review。review 必须独立会话且工作区指纹、任务版本与检查证据一致；通过才可标记任务完成，失败形成修正事件，受阻暂停。合并、部署等交付条件应明确写入验收，并配置对应检查或授权交付命令；服务不会默认合并或部署。

GitHub 同步前保存 publication 检查点，失败标为 sync_failed。重启仅查询远端操作标记并补同步，不重跑 Agent；查询不到上次非幂等写入时仍视为未知，要求人提供“确实未写入”的核对证据后才能再次尝试。状态更新等幂等操作可安全重试。跨版本自动迁移和自动清理暂不承诺，工作区和证据由使用者保留或归档。

## 检查职责

领域 Skill 定义方法、产物所有权与判断标准，项目设计提供具体规则，CLI 检查已编码约束，项目测试和运行工具提供行为证据。CLI 不选择任务顺序、不授予写入权限，也不以退出码证明整个任务完成。

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

YAML 文件统一命名 `system-design.yaml`，每份只负责所在目录；`list` 只发现作用域，不解析或校验 YAML 内容，成功不代表设计合法；`--file` 单独检查，`validate/check --all` 批量检查。父级排除子设计目录，越界声明和已识别的本地跨作用域导入报错；隐藏目录及依赖目录也参与作用域发现，仅排除 Git 元数据；具体规则和 Go workspace 限制见 [作用域规范](../skills/swe-system-design/references/system-design/guide.md#发现与目录作用域)。

只运行与当前对象相关的检查。YAML 项目不为满足旧 `design` 或 `structure` 命令复制 Markdown 架构表格。独立协议可运行 `documents --kind interface-protocol`，YAML 与协议的关联目前需要人工核对。`check` 的结构结论与源码依赖结论分开理解：没有语言适配器不等于结构检查失败，但导入或引用关系仍未验证；检测到语言但适配器无法可靠解析时返回无法检查。不支持的 workspace、构建结构或关系说明不当作通过。

## 命令入口

以下 `<CLI>` 指 PATH 中独立安装的 `swe`，所有命令显式指定目标根目录，文件一般使用相对项目路径。

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

CLI 中已有交互文档检查能力保留兼容。渲染写入项目文件是架构领域的明确写入动作，须纳入范围，使用临时输出成功后替换，避免错误诊断覆盖原图；用户请求查看时的目标选择、项目外缓存、多图汇总与宿主打开统一遵循[查看与展示流程](../skills/swe-system-design/references/system-design/guide.md#查看与展示流程)；CLI 不负责启动浏览器。

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

## 架构展示服务

本节记录 `swe system-design serve` 的兼容协议与安全约束。管理服务通过外部 `swe render` 提供另一个系统设计入口，专业规则和渲染仍由 `swe` 承担。

现有 `127.0.0.1:47831` 和 `/reed/` 路由是架构服务的兼容接口，不同时分配给新的管理服务。管理页采用命令渲染，默认监听 47832；DSH 继续使用 47831 上的独立架构服务，不将两者视为可互换服务。

### 运行模式与访问边界

| 入口 | 用途 | 行为 |
| --- | --- | --- |
| `system-design render --file <YAML>` | 分享、归档、离线查看 | 标准输出独立 HTML，随后退出 |
| `system-design serve --root <工作区> --expect-version <版本>` | 实时展示 | 固定监听 `127.0.0.1:47831`，持续运行 |
| 原 `system-design preview` | 已有单文件预览消费者 | 暂时保留原参数与行为 |

两种模式共用 YAML 校验、渲染核心。服务默认只读，不提供文件修改接口。YAML 是唯一持久化架构数据源。

访问 `http://127.0.0.1:47831/reed/` 查看工作区项目列表。`--root` 是允许访问的目录边界；其下多个包含 `system-design.yaml` 的目录共用同一服务。端口占用时明确失败，不更换端口。独立服务的终端使用 Ctrl-C 停止。

服务仅监听 IPv4 回环地址，检查 Host、请求方法和跨站访问。项目路径解析后不能越出工作区，不提供任意磁盘 HTML 或目录文件服务。

### 路由与宿主协议

Go 提供 `/reed/`、`/reed/health`、`/reed/resolve`、`/reed/view`、`/reed/state` 固定路由。前端提交会话目录，由 Go 解析真实路径并验证工作区边界，返回项目 URL 供同源 iframe 展示。工作区外的会话返回错误，不扩大授权范围。Go 服务只允许同源嵌入，原单文件预览仍禁止嵌入；代理必须保留 CSP 和缓存策略。

复用前核对服务类型、CLI 版本与工作区，不接管不匹配的服务。独立服务发生版本或工作区冲突时明确报错，由其使用者停止。

### 所有权与生命周期

服务区分启动者与复用者。宿主管理的服务使用 `--parent-stdin`，宿主退出关闭管道或异常退出导致管道断开时，Go 停止服务；复用者不停止独立服务。升级后由新插件启动新版本，旧版本退出期间短暂重试。插件管理的服务停止期间，独立浏览器页面显示断连提示，新服务启动后继续重试。

### 实时更新与编辑

页面每秒请求状态，内容变化后整页刷新；校验失败或连接中断时提示旧图失效，恢复后继续更新。状态请求保留项目标识，多个项目不会串图。首次打开即无效的 YAML 返回诊断，修复后需重新打开页面。

后期页面编辑由 Go 提供保存接口，包含保存前校验、版本冲突检查和草稿管理。有未保存草稿时不能直接整页刷新。本次不提前实现编辑接口。

## 验收与扩展边界

核心实现的验收覆盖授权子集、事件去重、任务创建、隔离执行、程序检查、独立审查、任务状态回写、效果重新采集、暂停停止、重启和未知写入恢复。Agent 与 GitHub 的测试替身只证明协议及控制逻辑；具体账号权限和实际业务效果须在接入场景核对。

本次本地验收（2026-11-10，macOS arm64）包括：两套应用的竞态测试、vet、六平台交叉构建；SWE 架构图交互与 DSH 插件集成；真实 reed → swe HTTP 发现与渲染；Playwright 管理页运行控制与架构展示；真实 Codex 结构化会话、授权目录写入与越界拒绝；真实 Codex 在临时 Git 仓库中的规划、执行、独立审查和效果评价。完整业务测试只替换 GitHub 传输，未对真实 GitHub 账号写入。重启、未知写入、验收版本变化、证据冲突、审查返工和目标效果未达成由故障注入测试覆盖。

当前预算指标是运行次数和时限，不宣称模型费用或 token 限额。首个内置 Agent 为 Codex；更多 Agent、并发调度、多租户访问、自动升级运行记录与系统设计页面编辑均为后续扩展，不纳入本次功能。
