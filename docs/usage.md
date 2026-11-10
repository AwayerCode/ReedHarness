# 使用指南

Skill 是交付单元，方法、资源和配套 CLI 一起安装到 `.agents/skills/<名称>/`。reed 只负责查找与分发，不常驻业务项目，不保存下载记录或项目锁，不依赖全局安装。

## 安装分发工具

当前源码的新分发行为尚未随旧版 `v0.2.2` 发布。验证当前实现时，在本仓根目录构建到项目外临时目录：

```bash
go build -C apps/reed -o /tmp/reed .
/tmp/reed version
```

以下命令在目标业务项目根目录执行，使用这个临时二进制。Windows 可构建或下载对应平台的 `reed.exe`，放在项目外临时目录；下载发行物时用同页 `SHA256SUMS` 校验。旧发行版仍有旧安装行为，不能用它验证本文新布局。

`install.sh` 现在是临时启动器：下载并校验 reed，执行 `--` 后的命令，退出后删除临时二进制。无命令时执行 `search`；`--version` 在分隔符之前选择 reed 发行版，在分隔符之后属于 reed 命令。新版本发布后可这样使用：

```bash
curl -fsSL https://raw.githubusercontent.com/AwayerCode/ReedHarness/main/install.sh | sh -s -- -- search
```

启动器不安装 reed 到业务项目，也不自动安装 Skill。

Windows 在本仓根目录可用 PowerShell 构建临时分发工具：

```powershell
go build -C apps/reed -o "$env:TEMP/reed.exe" .
& "$env:TEMP/reed.exe" version
```

后续将 `/tmp/reed` 替换为 `& "$env:TEMP/reed.exe"`，选择 `swe_windows_amd64.tar.gz` 或 `swe_windows_arm64.tar.gz`。包内检查器为 `.agents/skills/swe/bin/swe-cli.exe`。

## 安装 Skill

SWE 使用包含 CLI 的平台 Skill 包。以下示例的 `vX.Y.Z` 需替换为实际 ReedHarness 发行标签，平台名按本机选择；新打包方式尚未发布：

```bash
/tmp/reed install --source https://github.com/AwayerCode/ReedHarness/releases/download/vX.Y.Z/swe_darwin_arm64.tar.gz --version tag:vX.Y.Z --path skills/swe
```

本仓只分发 SWE。源码目录不包含构建产物，直接安装源码归档不会自动获得 CLI，应选择完整平台包。`--path skills/swe` 安装到 `.agents/skills/swe/`；入口必须是具有名称与简介的 `SKILL.md`，名称须与目录一致，不分发普通文件或整个仓库。历史归档的路径兼容逻辑继续保留，当前登记表不再列出其他 Skill。

reed 只下载所选 Skill 归档，将包内文件原样安装，不解释 `runtime.json` 或额外下载 CLI。SWE 包已经包含本平台 `bin/swe-cli` 和打包生成的本地摘要清单；CLI 启动时核对自身版本和摘要。安装前可用发行页 `SHA256SUMS` 验证整个包，选择正确平台。版本支持 `tag:<标签>`、`commit:<SHA>` 和 `branch:<分支>`；需要可复现安装时使用固定标签或 commit。

`--dry-run` 会下载、检查 Skill 包结构，但不写业务项目。来源支持 GitHub 仓库 URL 或直接 `.tar.gz` URL；归档需包含所选 Skill 目录。存在索引时，用同一次下载中的索引解析路径，索引非法则拒绝安装；不跨两次分支下载混用索引与内容。下载上限为 128 MiB，整个解压流（含未选中内容、元数据与尾部填充）上限为 512 MiB，归档条目最多 100000 个；选中文件单个最多 32 MiB。安装前会完整读取 gzip 流并校验尾部，损坏或超限的包不会进入项目安装阶段。

## 项目接入

支持 `.agents/skills/` 的 Agent 可原生发现入口，无需追加 `AGENTS.md`。其他宿主可显式读取 `.agents/skills/swe/SKILL.md`。reed 不改写项目指令，发现后的刷新和启用方式遵循宿主规则。

任务指令示例：

> 使用 SWE 的代码审查方法，审查当前项目尚未提交的代码改动。只报告有依据的问题，说明文件位置、影响和原因，不修改文件。缺少必要依据时明确说明。

项目中的主要入口与配套文件为：

```plaintext
.agents/skills/swe/
  SKILL.md
  references/
  runtime.json
  bin/swe-cli
```

Skill 文本可随项目保存；本平台二进制及随包生成的本地摘要清单由 `.gitignore` 排除：

```gitignore
.agents/skills/swe/bin/
.agents/skills/swe/runtime.json
```

```bash
git add .agents/skills/swe
```

配套 CLI 跟随 Skill 使用和替换。跨平台克隆后，二进制补装目前需先向临时项目安装相同修订，对比 Skill 文本后，将该平台 `bin/` 与 `runtime.json` 一起复制；没有专用修复命令。

## 查找与查看

```bash
/tmp/reed search
/tmp/reed list
```

`search` 从所选来源的 `skills/index.json` 按名称、路径和简介查找，默认主分支；可加 `--version` 指定含登记表的修订。没有登记表时会报错，不回退扫描旧仓库。

`list` 直接扫描 `.agents/skills/` 下含 `SKILL.md` 的目录，也能列出手动安装的 Skill。输出名称、路径和简介，不推断来源、已安装版本或本地修改状态；无下载记录也能使用。

## 换版与移除

reed 不再提供 `update`、`status`、`remove`。没有持久记录，就不声称能识别上游文件、本地新增文件或本地修改。

`install` 遇到已有目标目录一律拒绝覆盖。换版时先向另一临时项目安装指定版本，对比现有 Skill、保留本地修改，再显式替换整个 Skill 目录。移除时先核对并备份自有内容，再删除该 Skill 目录；配套 CLI 随之移除。项目 Git 负责历史与差异追踪。

安装先暂存并检查全部所选 Skill 入口，再独占创建各目标目录，完整复制资源和 CLI 后发布顶层 `SKILL.md`。普通错误会清理本次创建的目录，不覆盖并发安装者的目录。没有持久事务日志或崩溃自动恢复；异常终止留下的不完整目录需人工核对清理后重装。不要在安装期间编辑目标目录或运行尚未完成的 Skill。

## 旧安装迁移

新 reed 不读取、创建或迁移旧 `.harness/`、下载记录和锁。它只扫描当前 Skill 目录，不会自动删除旧数据。

1. 备份旧 Skill 中的本地修改和自有文件；从原记录或项目 Git 找回需安装的来源与版本。
2. 使用新版 reed 向 `.agents/skills/` 安装所选版本；如果已有同名目录，先在临时项目安装并比较。
3. 确认 Skill 和配套 CLI 可用后，移入需保留的自有内容并更新旧路径引用。
4. 确认旧目录无独有数据后，由项目维护者清理 `.harness/` 及其旧忽略规则。

## 接入、五个领域与共享配套

SWE 统一安装 `skills/swe`，内部包含 `business-model/`、`system-design/`、`implementation/`、`testing/`、`delivery/` 五个领域入口。它们代表责任与改动范围，不代表执行顺序。顶层入口按任务加载内部 Skill，不依赖宿主发现嵌套目录。旧设计／实现／Review 和交互目录已删除，有效专业方法已迁入新领域。

[adoption](../skills/swe/adoption/SKILL.md) 是同包内的项目接入能力，负责已有项目 YAML 的建立与修订组织，以及更新和退出；它按需使用五个领域，不独立安装或登记版本。退出先解除检查入口和指引引用，默认保留项目设计、代码与测试。

安装时只传入 `--path skills/swe`，使用包含新结构的 commit 或后续标签。五个领域不单独安装、更新或登记版本；`skills/index.json` 只登记一个 SWE 版本。此前试用的同级 `swe-*` 领域包应在核对并保留本地修改后移除，避免旧入口继续被发现。

共享规则、Schema 和唯一的 `runtime.json` 位于包内，五个领域共用 `bin/swe-cli`。各领域的 `contract.json` 仅描述包内 Schema 引用，由测试检查完整性，不控制安装与权限。完整平台 Skill 包随 ReedHarness 发布，开发环境可使用本仓打包产物。

领域负责专业判断与产物边界，CLI 检查已编码规则，项目测试与运行工具提供行为证据。文件范围检查不替代共享文件内部条目的差异复核。

返回[项目介绍与快速开始](../README.md)。
