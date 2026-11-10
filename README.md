# ReedHarness

面向 AI 编程 Agent 的软件工程 Harness。

> 用 YAML 定义架构，用 CLI 校验设计与实现。

ReedHarness 通过 SWE Skill 指导 Agent 完成业务建模、系统设计、代码实现、测试和交付，让代码改动遵循明确的模块边界与依赖规则，并提供可核验的检查结果。

插件市场分发与固定地址架构服务正在接入：支持静态 HTML 导出、Go 本地实时服务和 DSH 固定路径代理。构建、运行及待完成的市场验收见[插件分发与架构服务](docs/plugin-distribution.md)。市场安装链路验收前保留现有安装方式。

自有 Skill 保存在 `skills/<名称>/`，第三方 Skill 集中在 `skills/3rdparty/<名称>/`，统一由 `skills/index.json` 登记。两者均安装到 `.agents/skills/<名称>/`；兼容路径与使用方式见[安装指南](docs/usage.md#安装-skill)。

## 为什么需要 ReedHarness

Agent 能快速编写代码，但架构约定如果只停留在文字里，目录、模块职责和依赖关系就容易在持续修改中发生偏离。测试通过也不代表代码遵守了设计边界。

ReedHarness 将架构约束写入 `system-design.yaml`，让设计和代码有共同的检查依据：哪些模块存在、文件放在哪里、允许依赖谁，都可以显式声明并校验。Skill 提供专业方法与任务边界，项目已有测试和构建继续验证实际行为。

## 工作方式

1. 定义架构：用 YAML 声明层、模块、职责、目录、必要文件与允许依赖。CLI 校验设计，并可生成只读 HTML 架构图。
2. 按设计实现：Agent 读取 SWE Skill 与项目依据，在授权范围内修改代码、测试和交付配置。
3. 检查实现偏差：CLI 始终核对目录、文件归属和必要文件；匹配到语言适配器时再检查源码依赖，返回违规位置。Agent 根据反馈修正，再执行项目测试与构建。

> YAML 定义约束 → Agent 实现 → CLI 检查 → 根据反馈修正。

检查通过仅证明覆盖的规则满足。Agent 仍需结合任务目标判断是否完成；只读审查报告有依据的问题，不自动修复，受阻时说明当前状态和所需决策。改进方法不能通过降低验收标准完成。

## 一个最小示例

这份 `system-design.yaml` 定义两个模块：订单可以调用支付，支付不能反向依赖订单。

```yaml
schema_version: 1
source_root: src
layers:
  - id: application
    directory: application
    responsibility: 应用流程
    allowed_dependencies: []
modules:
  - id: orders
    layer: application
    directory: orders
    responsibility: 订单处理
    required_files: [service.go]
    allowed_dependencies: [payments]
  - id: payments
    layer: application
    directory: payments
    responsibility: 支付处理
    required_files: [service.go]
    allowed_dependencies: []
```

对应目录为 `src/application/orders/` 和 `src/application/payments/`，各包含一个 `service.go`。设计校验不要求源码已存在；实现检查始终要求目录和必要文件存在。当前内置 Go、TypeScript 和 C++ 适配器，只在检测到对应源码时检查导入或引用；其他语言需要新增适配器才能验证源码依赖，没有适配器时结构检查与源码依赖检查必须分开理解。

安装 SWE 后，在项目根目录执行：

```bash
.agents/skills/swe/bin/swe-cli system-design validate --file system-design.yaml
.agents/skills/swe/bin/swe-cli system-design check --file system-design.yaml
```

例如项目的 Go module 为 `example.com/shop`，若支付模块导入 `example.com/shop/src/application/orders`，`check` 会报告未允许的模块依赖及文件、行号，并以退出码 `1` 结束。退出码 `0` 表示本次范围通过，`2` 表示无法检查；结果为 JSON。

该违规的 JSON 诊断节选：

```json
{
  "rule": "dependency.forbidden",
  "location": "src/application/payments/service.go:2",
  "message": "payments -> orders is not allowed"
}
```

完整约束与架构图生成方法见 [YAML 模块架构](skills/swe/references/system-design/guide.md)。

## 快速开始

当前源码包含新的分发方式与 YAML 架构检查，旧版 `v0.2.2` 不能用于验证本文流程。快速开始使用本地构建的完整 SWE 包，在临时项目中体验，不依赖尚未发布的远端包。需要 Go 1.24+、Bash 和 tar；命令适用于 macOS / Linux。

### 准备 SWE

在本仓库根目录执行，构建六平台完整 Skill 包并选择本机平台：

```bash
apps/swe-cli/scripts/build-package.sh
reed_package="$PWD/apps/swe-cli/dist/swe_$(go env GOOS)_$(go env GOARCH).tar.gz"
reed_demo="$(mktemp -d)"
mkdir -p "$reed_demo/.agents"
tar -xzf "$reed_package" -C "$reed_demo/.agents"
cd "$reed_demo"
.agents/skills/swe/bin/swe-cli version
```

包内已经包含 Skill、平台二进制和版本摘要清单。这里只把选择的 SWE 包解压到新项目，不复制整个 ReedHarness 仓库，也不需要全局安装。

### 完成一次检查

继续在同一终端执行：

```bash
go mod init example.com/shop
mkdir -p src/application/orders src/application/payments
printf 'package orders\n' > src/application/orders/service.go
printf 'package payments\n' > src/application/payments/service.go
```

将上面的 YAML 保存为当前目录的 `system-design.yaml`，再运行示例中的 `validate` 和 `check`。两项检查应均返回退出码 `0`。此时验证的是最小结构与依赖约束，还没有实现订单业务。

### 让 Agent 开始工作

在该项目打开支持 Skill 的 Agent。支持 `.agents/skills/` 的宿主可发现 SWE；其他宿主可以显式读取入口，刷新与启用方式遵循宿主规则。向 Agent 提供任务，例如：

> 读取 `.agents/skills/swe/SKILL.md`，按当前 `system-design.yaml` 实现订单调用支付的最小示例，并补充测试。修改范围为 `src/`，保持现有架构约束，运行实现检查和 Go 测试，报告结果与未验证项。

接入已有项目时，使用包内的 [adoption](skills/swe/adoption/SKILL.md) 组织 YAML 建立、修订和检查接入。现有设计仍是权威依据，不从代码自动覆盖重建。

实际业务项目的安装、Windows 使用、版本管理与旧安装迁移见[使用指南](docs/usage.md)。

## 能力与当前范围

SWE 统一安装，按任务选择业务模型、系统设计、代码实现、测试或发布交付五个领域；它们表示职责与产物边界，不要求顺序执行。接入、更新和退出由同包的 adoption 组织。

`swe-cli` 提供 YAML 架构、文档、产物关系、目录结构和改动范围检查。YAML 的目录、文件归属与必要文件检查不依赖语言；源码依赖由可选语言适配器检查，当前内置 Go、TypeScript 和 C++。未匹配适配器的语言会明确标记为源码依赖未验证。即使启用适配器，也不验证函数调用、类型语义或业务行为；各适配器的解析限制见 [YAML 规范](skills/swe/references/system-design/guide.md#语言适配器边界)。

`reed` 负责查找与分发选定的 Skill，`swe-cli` 随 SWE 包交付。ReedHarness 不接管 Agent 运行时；当前聚焦独立任务，不提供任务调度器，Bug 修复与需求开发等专门流程暂未建设。

## 文档与贡献

- [使用指南](docs/usage.md)：安装、项目接入、换版、移除与迁移。
- [SWE 入口](skills/swe/SKILL.md)：专业方法、共享规范和项目接入。
- [CLI 参考](skills/swe/references/cli.md)：命令参数、输出与检查边界。
- [系统设计](docs/system-design.md)与[接口协议](docs/interface-protocol.md)：内部职责与目标方案；其中待实施内容不代表当前能力。
- [贡献指南](CONTRIBUTING.md)：Skill 版本登记、构建与发布；本地验证遵循 [AGENTS.md](AGENTS.md#本地验证)。

## 许可证

[MIT](LICENSE)
