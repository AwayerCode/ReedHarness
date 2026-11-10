# swe 应用

固定地址、多项目实时服务使用 `swe system-design serve --root <工作区> --expect-version <版本>`，浏览器访问 `http://127.0.0.1:47831/reed/`。DSH 使用同源固定路径代理，静态导出继续使用 `render`。详见[应用设计](../../docs/app-design.md#架构展示服务)。

统一应用入口，当前提供确定性校验与架构服务；管理和调度属于独立 reed 服务。通用核心负责 YAML、目录、文件归属与模块关系；语言适配器负责解释源码中的导入或引用。Skill 保存方法与规范，业务测试和构建仍由 Agent 按项目约定调用。

## 检查能力

| 入口 | 覆盖 |
| --- | --- |
| documents | 五类文档格式、表格、枚举、本地链接与 architecture 局部模式 |
| design | 模块依赖、协议索引与定义、所有权、代码映射及双向文档引用 |
| artifacts | UI 与协议、风险与场景的显式对应关系 |
| structure | 设计声明的代码位置、入口文件和范围映射 |
| scope snapshot/check/readonly | 可写范围基线、工作区变更和只读指纹，含子模块 |

检查不判断任务整体完成，不替代人工语义审查。`system-design check` 可以通过语言适配器检查已支持的源码依赖；其他入口不解析源码依赖。结果保留已检查证据和未覆盖项。只有 scope snapshot 创建全新的项目外基线；其他检查不写项目。

## YAML 模块架构

新增 `system-design list/validate/check/render/preview` 入口，执行结构化设计校验、语言无关的目录与文件归属检查，并生成可展开、可追踪依赖的只读 HTML。`check` 按检测到的源码调用内置 Go、TypeScript 或 C++ 适配器；其他语言在结构检查后明确列为源码依赖未验证。采用规则、限制与示例见 [YAML 模块架构](../../skills/swe-system-design/references/system-design/guide.md)。`validate --profile layers` 用于层设计交接，完整验收要求存在模块；该入口不并入 JSON 协议版本 2。

受控查看使用 `swe system-design preview --file /absolute/project/system-design.yaml --expect-version <固定版本>`。命令验证版本和 YAML 后输出本机 URL，默认打开系统浏览器，使用 `--no-open` 可由宿主自行展示。服务持续前台运行，Ctrl-C 或终止进程时停止；重复启动同一 YAML 和同一 CLI 构建会复用服务并退出新命令。页面每秒检查 YAML 并自动更新；校验失败或连接中断时遮罩标注旧图已过期或可能过期，修复后自动恢复。不接受磁盘 HTML，不输出中间页面文件。`render` 仅用于离线导出。版本声明不能替代安装包和运行权限保护。

## 使用

```bash
swe documents --root /absolute/project --kind system-design --file docs/system-design.md
swe design --root /absolute/project --system-design docs/system-design.md --interface-protocol docs/interface-protocol.md
swe artifacts --root /absolute/project --kind test --file docs/test-design.md
swe structure --root /absolute/project --design docs/system-design.md --scope src
swe scope snapshot --root /absolute/project --baseline /tmp/new-baseline.json --allow-dir src
swe scope check --root /absolute/project --baseline /tmp/new-baseline.json
swe scope readonly --root /absolute/project
```

完整参数与边界见 [应用设计的检查命令](../../docs/app-design.md)。readonly 的 sha256 字段含格式前缀，保存后原样传给 expected。旧脚本基线和指纹明确拒绝，不自动重建或迁移正在使用的基线。

## JSON 协议

普通检查支持 `swe check --request <file|-> --format json`。协议版本 2，旧扩展字段拒绝。退出码 0 为全部必需检查通过，1 为违规，2 为不可检查；不可检查优先，所有诊断仍保留。至少包含一个必需检查，同一请求不能重复 checkId。

```json
{
  "protocolVersion": 2,
  "taskId": "document-check",
  "round": 1,
  "mode": "review",
  "projectRoot": "/absolute/project",
  "checks": [
    {
      "checkId": "design",
      "required": true,
      "input": {
        "systemDesign": "docs/system-design.md",
        "protocol": "docs/interface-protocol.md"
      }
    }
  ]
}
```

各 input 字段：documents 使用 kind/file/profile；design 使用 systemDesign/protocol；artifacts 使用 kind/file/protocol；structure 使用 design/scopes。范围操作独立于 check，因为 snapshot 有明确写入副作用。

结果包含 runId、核心版本、规则与输入摘要，并复核观察到的文件变化。不是文件系统原子快照。scope 另使用版本化 Git 工作区清单与摘要。

## 开发与构建

需要 Go 1.24+；Git 用于范围检查。运行普通文档关系检查不需要 Git。二进制不依赖 Python、Node 或 Go 运行环境，YAML 与 JSON Schema 库编入二进制，无额外运行时依赖。

```bash
go generate ./...
scripts/verify.sh
```

统一验证入口需要 Go、Git 和 Node.js 18+，依次执行 swe 的竞态测试与 vet、架构图交互逻辑测试、Go 格式与脚本语法检查、六平台构建、插件打包与 DSH 集成测试。缺少工具或任一检查失败均返回非零，不跳过交互测试；可用 `NODE_BINARY` 指定开发工具路径。浏览器实际交互仍需人工或浏览器验收，Node 测试不替代该验证。

Schema 从 CLI 自有的 `resources/artifact-schemas/` 与 `resources/system-design/schema.json` 生成并编入二进制；测试检查生成来源一致性、单个 Skill 的资源闭包（含 contract 引用不越出自身目录），以及 Skill 参考副本与 CLI 资源、`docs/app-design.md`、`docs/distribution-design.md` 中对应章节的漂移。生成文件不可手改。

迁移用例保存在 internal/app/testdata，包含由旧 Python 检查器生成的 95 个正反例及预期退出码，执行不需要 Python。另有显式场景引用、权限、符号链接、子模块、基线保护、工具版本与摘要回归测试。独立二进制测试使用无语言运行时 PATH。

## 内部职责

命令参数与输出协议保留在 `internal/app`：`app.go` 只分派命令，`check_command.go` 解析请求，`check_execution.go` 执行检查并归并结果，`protocol.go` 维护协议与请求校验。`internal/workspace/observer.go` 负责文件读取和输入稳定性观察，规则包通过读取接口使用它。

`internal/systemdesign/execution.go` 组织单份设计的校验、实现检查或渲染；`source_structure.go` 先完成语言无关的必需路径、文件类型、目录归属与文件归属检查；`implementation.go` 再按文件类型选择 Go、TypeScript 或 C++ 适配器。适配器只提取引用并映射目标文件，模块关闭与允许依赖由共享核心统一裁决。`CheckGo` 保留为显式 Go 分析入口。适配器不能改变 YAML 结构规则或允许依赖语义。

架构图由 `diagram_model.go` 定义视图数据，`diagram_order.go` 调整模块顺序，`diagram_layout.go` 计算节点几何，`diagram_routes.go` 生成连线路径，`html.go` 将结果交给内嵌 HTML 模板。布局与路由不读取文件或操作浏览器，模板不承担设计规则。

## 配套分发

`scripts/build-release.sh <版本>` 构建 macOS、Linux、Windows 的 amd64/arm64 CLI，在 `dist/` 生成六个 `swe_<系统>_<架构>.tar.gz`，并为每个 Skill 生成独立的 `<skill-name>.tar.gz`。Skill 包只有自身资源，不携带二进制，也不依赖其他 Skill。CLI 包与 Skill 包各自独立下载，版本由同一次发行标签标识。

CLI 包包含 `swe/bin/swe`（Windows 为 `.exe`）及 `swe/runtime.json`。清单包含 schema_version、工具名称、版本和本平台 SHA256，不包含 URL。保留包目录结构，将其 bin 目录加入 PATH 后即可调用 `swe`；运行时核对工具版本和自身摘要。

包内容与安装行为有本地测试；远端安装需先发布对应资源。六平台构建不等于实机运行验证，当前实际运行覆盖 macOS arm64；最低系统版本仍需发行验收。
