# reed 管理与调度服务

reed 提供 Business Harness 管理入口；专业检查与架构渲染调用独立 swe。目标和任务的权威来源仍是 GitHub Project 与 Issue，本地只保存调度与恢复记录。

## 启动

需要 Go 1.24+ 构建。运行二进制不需要 Go 或 Node；GitHub 接入需要已登录的 `gh`，架构展示需要独立安装的 `swe`。不配置 GitHub 或 Agent 仍可查看 Skill、运行页与系统设计；相关操作会明确报告尚未配置。

```bash
apps/reed/scripts/build-release.sh 0.6.0
# 解压适合本机的 reed_<os>_<arch>.tar.gz，将 reed/bin 加入 PATH。
reed version
reed serve --config /absolute/reed-config.json
```

默认访问 `http://127.0.0.1:47832/`。配置文件不提交到业务仓库，以下路径均为示例，改成实际本地路径：

```json
{
  "root": "/absolute/workspace",
  "state_dir": "/absolute/reed-state",
  "skills_dir": "/absolute/ReedHarness/skills",
  "skill_version": "selected-commit-or-release",
  "swe": "/absolute/tools/swe",
  "swe_version": "0.6.0",
  "gh": "gh",
  "repository": "owner/repository",
  "project_owner": "owner",
  "project_number": 1,
  "allow_github_write": false,
  "agent_backend": "codex",
  "codex": "codex",
  "check_commands": {
    "architecture": ["/absolute/tools/swe", "system-design", "check", "--root", "{workspace}", "--all"],
    "tests": ["go", "test", "./..."],
    "effect": ["/absolute/measure-effect"]
  },
  "allowed_paths": ["src"],
  "max_runs": 20,
  "run_timeout_seconds": 300,
  "interval_seconds": 0
}
```

`root` 和 `state_dir` 必填；其余可以省略。路径相对服务启动目录解释。`interval_seconds: 0` 禁用周期触发，大于零时产生目标评价事件。`max_runs` 是该运行目录累计事件预算，不是 token 或费用预算。配置变更使用新的运行目录，旧目录保留审计；不能通过复用旧授权运行新项目。

目标页确认 Project 的 ID、标题和 README 摘要及其中的授权契约后才能调度 GitHub 目标。定义变化使旧确认失效；任务必须属于配置的 Project 和仓库。`allow_github_write` 控制页面编辑已有目标、任务，默认关闭；不授权 Agent 直接写 GitHub；自动业务推进也由该配置开关授权。超过 1000 个 Project 条目会明确拒绝不完整观察。

无 GitHub 配置时可用显式事件输入进行本地协议测试；这不构成真实目标效果验收。使用 `agent_backend: "codex"` 选择内置适配器；自定义宿主使用 `agent_command` 数组，两者不能同时配置。不能把 Codex 原生命令填入自定义协议入口。

## 目标与任务契约

目标 README 保留业务叙述、基线、效果标准和停止条件，并包含一个 JSON 授权块。下面示例中的检查名称对应服务配置的 `check_commands`；命令由人固定，Agent 只能引用名称，不能传入任意命令。

````markdown
```reed-goal
{
  "allowed_paths": ["src"],
  "checks": ["architecture", "tests"],
  "effect_checks": ["effect"],
  "max_runs": 20,
  "timeout_seconds": 300
}
```
````

目标的路径、预算不得超过服务配置；任务必须包含目标要求的检查，不能增加未授权检查。`effect_checks` 必须重新采集实际效果，例如部署后指标或用户行为变化，不能只是重命名任务测试。检查命令在工作区执行，参数 `{workspace}` 替换为本次绝对路径；检查和交付命令是由人授权的可信程序，不在 Agent 沙箱中执行。它们须自行管理依赖、凭据、缓存和副作用。

任务 Issue 正文包含一个 `reed-task` JSON 块，字段为 title、body、acceptance、allowed_paths、checks、skills。Agent 的任务提案经服务校验后生成该块。acceptance 为非空验收条件数组，allowed_paths 必须是目标路径子集，skills 引用配置来源中的名称；任务运行只装载选中的 Skill。

工作开始前源 Git 工作区必须干净；服务在 state_dir 下创建独立克隆，保留交付。Codex 当前要求可写项是已存在的目录；不能表达的权限直接拒绝。项目根目录和运行目录须分开。任务执行后不能修改原始验收定义，变更要求用新任务表达。

合并、部署不是默认动作。要求这些交付时，将其写入验收并配置明确授权的交付检查命令；未满足时不能完成任务。

## 内置 Codex 接入

使用本机已认证的 Codex CLI，当前验证接口依据 `codex-cli 0.167.0`。每次调用新建非交互会话，忽略用户配置、项目启动目录和 execpolicy 规则；临时启动目录与任务工作区分离。执行使用 workspace-write 沙箱，只追加任务授权目录；规划、审查与评价使用 read-only。禁用审批提权、工具网络、自动临时目录写权限，并从 JSONL 的 thread.started 读取宿主会话身份，不能由模型自报身份替代。

服务不会继承全局 MCP、插件或宽权限设置。使用本机自定义模型接入地址时，在服务配置中显式指定 `codex_model` 和 `codex_base_url`；不通过重新加载全局配置恢复连接，也不把带凭据的地址提交到源码。账号认证仍由 Codex 管理；服务不保存 API Key。若安装版本不支持这些参数、沙箱不可用或模型连接失败，运行失败并保留诊断，不退回无沙箱模式。官方接口依据：[非交互模式](https://developers.openai.com/codex/noninteractive/)和[配置参考](https://developers.openai.com/codex/config-reference/)。

## Agent 宿主协议

服务执行 `agent_command` 数组，不经过 shell。先附加 `--capabilities` 读取：

```json
{"protocol":"reed-agent-v1","enforces_scope":true}
```

能力声明来自可信宿主适配器，不能证明其实现了隔离。宿主必须落实 `allowed_paths`、`read_only`、独立会话、运行时限与子进程关闭；无法落实时拒绝执行。服务不自建沙箱；自定义适配器必须是可信的宿主实现，不能把普通脚本返回 true 当作隔离证明。

正常调用从标准输入读取单个 JSON 对象：

```json
{
  "protocol": "reed-agent-v1",
  "event": {"id":"delivery:1","kind":"review","issue":1,"input":"根据原始验收条件审查"},
  "goal": {},
  "task": {},
  "history": [],
  "root": "/absolute/workspace",
  "allowed_paths": ["src"],
  "skills": [],
  "swe": "/absolute/tools/swe",
  "timeout_seconds": 300,
  "fresh_session": true,
  "read_only": true,
  "github_write": false
}
```

`goal/task` 是当前 GitHub 观察；本地模式为 null。`skills` 包含名称、来源、版本与 SKILL.md 内容；其余资源从对应来源读取。每次调用使用新会话；只有 execute 请求允许写入配置范围，plan/review/evaluate 为只读。任务文本和 Agent 输出不能扩大这些权限。stdout 只返回 JSON，日志写 stderr，两通道各限 8 MiB。

```json
{
  "protocol": "reed-agent-v1",
  "status": "continue",
  "summary": "交付已通过独立审查，等待效果评价",
  "evidence": ["检查报告或实际效果证据位置"],
  "session": "unique-host-session-id",
  "next": {"kind":"evaluate","issue":1,"input":"重新采集目标效果"}
}
```

`status` 为 continue、wait、needs_input 或 done。continue 必须附下一事件；新任务创建后由服务绑定返回的 Issue。跨任务调度必须重新读取 Project 成员和授权契约，服务生成后续事件标识。wait 等待下一次事件或定时器；needs_input 暂停；done 仅允许 evaluate 且必须有证据和本轮重新采集并通过的效果检查。review 必须返回未在历史中出现过的会话标识，以及 pass、fail 或 blocked verdict；验收通过、返工和受阻由服务据此处理。服务校验协议和身份，证据的语义与实际效果仍由具体场景验证，不能用“测试通过”替代目标效果。

## 调度与恢复

页面或本机调用者通过 `/api/event` 提交事件。先 GET `/api/state` 获取管理令牌，POST 使用 `X-Reed-Token` 和 `Content-Type: application/json`。这是本地同源防护，不是远程认证接口。

```json
{"id":"issue-1-updated-v2","kind":"execute","issue":1,"input":"本次明确任务输入"}
```

相同 ID 去重，同一任务已排队或运行时拒绝重复。单实例串行处理；结果和后续事件原子持久化。暂停不取消当前工作，停止取消当前宿主命令与排队工作，已发生的外部副作用不会自动撤销。服务取消 Unix 进程组，Windows 请求终止进程树；第三方宿主的远程任务仍须由适配器负责停止。

重启或启动后的未知结果标记 interrupted，不能直接恢复；核对宿主已停止、工作区和远端副作用后，在运行页提交证据，再恢复后续调度。不会自动重跑中断事件。异常退出残留 `service.lock` 时，先核对旧服务与宿主均已停止，再删除锁；不可直接启动第二个写入者。

GitHub 同步前保存 publication；失败显示 sync_failed。运行页可“核对远端并重试同步”，服务先按操作标记查找已写入结果，只补同步、不重跑 Agent。若查询不到上次非幂等写入，服务仍不自动重发；用户核对远端确认未写入后，填写证据再授权重试。目标评价保存在 Project 草稿条目，目标 README 不受结果回写修改。

运行记录为本地 JSON，当前不提供跨版本自动迁移。已停止或达成的运行不可恢复；新授权使用新配置目录。

## 验证与边界

```bash
apps/reed/scripts/verify.sh
apps/swe/scripts/verify.sh
```

reed 验证覆盖事件、定时、去重、预算、暂停停止、重启核对、目标变更、会话与 HTTP 边界，并构建六平台服务。swe 单独验证专业能力、架构交互、分发与 DSH 插件。交叉构建不替代目标平台运行。

本地端到端验收使用真实临时 Git 仓库、检查与 GitHub/Agent 替身，覆盖完整业务状态和故障恢复。真实 Codex 接入可额外执行：

```bash
REED_CODEX_SMOKE=1 go -C apps/reed test ./internal/control -run 'TestCodexLive(Smoke|Scope|BusinessLoop)'  -v -count=1
```

该命令会使用已认证的 Codex 账号进行只读协议、临时目录沙箱边界和完整业务闭环测试，不写 GitHub。BusinessLoop 使用真实 Codex 规划、执行、独立审查和评价，在真实临时 Git 克隆中交付文件并重新测量结果，仅 GitHub 传输使用替身。自定义接入地址可通过测试环境变量 `REED_CODEX_MODEL`、`REED_CODEX_BASE_URL` 传入。真实账号、GitHub 权限、Windows 运行与实际业务效果需要单独核对；不能用交叉构建或替身测试代替。
