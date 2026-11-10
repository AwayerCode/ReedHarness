# ReedHarness

> 围绕目标持续推进工作的 Harness：Loop 推进目标，Task 明确每次工作，Skill 提供完成能力。

ReedHarness 实现 Harness 的 Business 层。人确定目标、评价依据、授权与预算；`reed` 根据现状形成任务，驱动现成 Agent 在隔离工作区中执行，用程序检查和独立审查验收结果，再用重新采集的效果证据判断目标进展，决定继续、等待、停止或请人决定。

模型调用与 Agent 循环使用 Codex、Claude Code、pi 等现成 Agent；`reed` 负责状态、授权、预算、隔离与检查等确定性部分。SWE 是首个验证领域，六个 `swe-*` Skill 提供业务建模、系统设计、实现、测试、交付与接入的专业方法。

## 当前状态

设计已按新的 Harness 定义重做，见[系统设计](docs/system-design.md)。目标、任务与 Loop 尚未实现，按 P0、P1 推进。当前可用的是：

- 六个自包含的 SWE Skill，位于 `skills/`。
- 确定性检查命令：YAML 架构、文档、产物关系、目录结构与改动范围，当前命令名为 `swe-cli`，P0 合并为 `reed`。
- 插件验证包与本地架构服务，见[分发设计](docs/distribution-design.md)。

已发布的 `v0.3.0` 是旧的平台 Skill 包，不对应本设计。

## 检查能力示例

以下示例演示当前已可用的架构检查，它在新设计中作为任务验收的程序检查使用。这份 `system-design.yaml` 定义两个模块：订单可以调用支付，支付不能反向依赖订单。

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

独立安装 CLI 并加入 PATH 后，在项目根目录执行：

```bash
swe-cli system-design validate --file system-design.yaml
swe-cli system-design check --file system-design.yaml
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

完整约束与架构图生成方法见 [YAML 模块架构](skills/swe-system-design/references/system-design/guide.md)。

## 快速开始

快速开始只覆盖当前可用的检查命令与 Skill，目标与 Loop 实现后改写。已发布的 `v0.3.0` 不能用于验证本文流程；以下使用本地构建的独立 CLI 与所选 Skill 包，在临时项目中体验，不依赖尚未发布的远端包。需要 Go 1.24+、Bash 和 tar；命令适用于 macOS / Linux。

### 准备 CLI 与 Skill

在本仓库根目录执行，构建六平台 CLI 和独立 Skill 文本包，并选择本机 CLI：

```bash
apps/swe-cli/scripts/build-package.sh
reed_package="$PWD/apps/swe-cli/dist/swe-cli_$(go env GOOS)_$(go env GOARCH).tar.gz"
reed_tools="$(mktemp -d)"
tar -xzf "$reed_package" -C "$reed_tools"
export PATH="$reed_tools/swe-cli/bin:$PATH"
swe-cli version
reed_demo="$(mktemp -d)"
mkdir -p "$reed_demo/.agents"
tar -xzf apps/swe-cli/dist/swe-implementation.tar.gz -C "$reed_demo/.agents"
cd "$reed_demo"
```

CLI 包与 Skill 包分别选择，Skill 仅包含文本方法和参考资源，不含二进制。该示例只安装代码实现 Skill；其他能力按需要独立安装。

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

在该项目打开支持 Skill 的 Agent。支持 `.agents/skills/` 的宿主可发现已安装入口；其他宿主可以显式读取对应专业入口，刷新与启用方式遵循宿主规则。向 Agent 提供任务，例如：

> 直接读取 `.agents/skills/swe-implementation/SKILL.md`，按当前 `system-design.yaml` 实现订单调用支付的最小示例。修改范围为 `src/`，保持现有架构约束，运行实现检查和 Go 测试，报告结果与未验证项。

接入已有项目时，可独立安装 [swe-adoption](skills/swe-adoption/SKILL.md) 维护 CLI 检查接线、接入记录与工具生命周期。现有设计仍是权威依据，不从代码自动覆盖重建。

## 文档与贡献

- [系统设计](docs/system-design.md)：定位、核心概念、硬规则、reed 职责与实施阶段。
- [应用设计](docs/app-design.md)：`reed` 模块、调度、接入、检查协议与架构服务。
- [分发设计](docs/distribution-design.md)：二进制、Skill 与插件的构建、安装、更新与卸载。
- [贡献指南](CONTRIBUTING.md)：Skill 维护、构建与发布；本地验证遵循 [AGENTS.md](AGENTS.md#本地验证)。

## 许可证

[MIT](LICENSE)
