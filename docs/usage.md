# 使用指南

六个 Skill 各自安装和升级，彼此没有依赖。每个目录自带必要方法、模板和参考 Schema；独立 CLI 安装到 PATH，Skill 包不带二进制。reed 只负责查找与分发选定 Skill，不常驻业务项目，不保存下载记录或项目锁。

## 安装分发工具

已发布的 `v0.3.0` 仍是旧分发方式（含 CLI 的平台 Skill 包），不含本文的独立 Skill 与独立 CLI。验证当前源码时，在本仓根目录构建到项目外临时目录：

```bash
go build -C apps/reed -o /tmp/reed .
/tmp/reed version
```

以下命令在目标业务项目根目录执行，使用这个临时二进制。Windows 可构建或下载对应平台的 `reed.exe`，放在项目外临时目录；下载发行物时用同页 `SHA256SUMS` 校验。旧发行版仍有旧安装行为，不能用它验证本文新布局。

`install.sh` 是临时启动器：下载并校验 reed，执行 `--` 后的命令，退出后删除临时二进制。无命令时执行 `list`；`--version` 在分隔符之前选择 reed 发行版，在分隔符之后属于 reed 命令。例如：

```bash
curl -fsSL https://raw.githubusercontent.com/AwayerCode/ReedHarness/main/install.sh | sh -s -- -- list
```

启动器不安装 reed 到业务项目，也不自动安装 Skill。

Windows 在本仓根目录可用 PowerShell 构建临时分发工具：

```powershell
go build -C apps/reed -o "$env:TEMP/reed.exe" .
& "$env:TEMP/reed.exe" version
```

后续将 `/tmp/reed` 替换为 `& "$env:TEMP/reed.exe"`，CLI 选择 `swe-cli_windows_amd64.tar.gz` 或 `swe-cli_windows_arm64.tar.gz`，解压后将 `swe-cli/bin/` 加入 PATH，调用 `swe-cli.exe`。Skill 文本包无平台差异。

## 安装 Skill

每个 Skill 使用独立 `<skill-name>.tar.gz`，归档包含 `skills/<skill-name>/`。示例的 `vX.Y.Z` 替换为包含该布局的发行标签；已发布的 `v0.3.0` 与更早版本仍是含 CLI 的旧平台包，未发布的构建不能用于远端安装：

```bash
/tmp/reed install --source https://github.com/AwayerCode/ReedHarness/releases/download/vX.Y.Z/swe-implementation.tar.gz --version tag:vX.Y.Z --path skills/swe-implementation
```

`--path` 只选择对应 Skill，安装至 `.agents/skills/<skill-name>/`，不展开其他入口。入口必须有名称与简介且名称和目录一致。Skill 为纯文本资源，不携带 CLI，也不要求任何同级目录。版本支持 `tag:<标签>`、`commit:<SHA>` 和 `branch:<分支>`；可复现安装使用固定标签或 commit。

CLI 单独下载 `swe-cli_<os>_<arch>.tar.gz`，用发行页 `SHA256SUMS` 校验，解压后将 `swe-cli/bin/` 加入 PATH。保留其配套版本摘要清单并执行 `swe-cli version` 确认。reed 不安装 CLI，不解释其运行时清单，也不从 Skill 里寻找二进制。

`--dry-run` 会下载、检查 Skill 包结构，但不写业务项目；它不因目标目录已存在而失败，可在已安装的项目中拉取指定版本用于对比。来源支持 GitHub 仓库 URL 或直接 `.tar.gz` URL；归档需包含所选 Skill 目录。存在索引时，用同一次下载中的索引解析路径，索引非法则拒绝安装；不跨两次分支下载混用索引与内容。下载上限为 128 MiB，整个解压流（含未选中内容、元数据与尾部填充）上限为 512 MiB，归档条目最多 100000 个；选中文件单个最多 32 MiB。安装前会完整读取 gzip 流并校验尾部，损坏或超限的包不会进入项目安装阶段。

## 项目接入

支持 `.agents/skills/` 的 Agent 可原生发现入口，无需追加 `AGENTS.md`。其他宿主可显式读取所需的 `.agents/skills/swe-<专业>/SKILL.md`。reed 不改写项目指令，发现后的刷新和启用方式遵循宿主规则。

任务指令示例：

> 直接使用 swe-implementation 的代码审查方法，审查当前项目尚未提交的代码改动。只报告有依据的问题，说明文件位置、影响和原因，不修改文件。缺少必要依据时明确说明。

安装所选 Skill 后，目录结构为；只会出现实际选择的入口：

```plaintext
.agents/skills/
  swe-implementation/
    SKILL.md
    contract.json
    references/
```

`contract.json` 只出现在声明适用机器产物类型的 Skill 中，`swe-adoption` 没有该文件。目录内容按 Skill 实际需要确定，不预置空目录。
Skill 文本可随项目 Git 保存，不需排除包内二进制，因为包内没有二进制。CLI 保存在独立工具目录并加入 PATH，跨平台克隆只需另行安装对应平台 CLI；项目记录 Skill 与 CLI 的来源、版本及兼容要求。

## 查看已安装

```bash
/tmp/reed list
```

`list` 直接扫描 `.agents/skills/` 下含 `SKILL.md` 的目录，也能列出手动安装的 Skill。输出名称、路径和简介，不推断来源、已安装版本或本地修改状态；无下载记录也能使用。

## 换版与移除

reed 不再提供 `update`、`status`、`remove`。没有持久记录，就不声称能识别上游文件、本地新增文件或本地修改。

`install` 遇到已有目标目录一律拒绝覆盖。换版时先向另一临时项目安装指定版本，对比现有 Skill、保留本地修改，再显式替换选定 Skill 的目录。移除时先核对并备份自有内容、解除项目接线，再删除该 Skill 目录；CLI 独立管理，仍有消费者时保留。项目 Git 负责历史与差异追踪。

安装先暂存并检查全部所选 Skill 入口，再独占创建各目标目录，完整复制文本资源后发布顶层 `SKILL.md`。普通错误会清理本次创建的目录，不覆盖并发安装者的目录。没有持久事务日志或崩溃自动恢复；异常终止留下的不完整目录需人工核对清理后重装。不要在安装期间编辑目标目录或运行尚未完成的 Skill。

## 旧安装迁移

新 reed 不读取、创建或迁移旧 `.harness/`、下载记录和锁。它只扫描当前 Skill 目录，不会自动删除旧数据。

1. 备份旧 Skill 中的本地修改和自有文件；从原记录或项目 Git 找回需安装的来源与版本。
2. 使用新版 reed 向 `.agents/skills/` 安装所选版本；如果已有同名目录，先在临时项目安装并比较。
3. 分别确认 Skill 与独立 CLI 可用后，移入需保留的自有内容并更新旧路径引用。
4. 确认旧目录无独有数据后，由项目维护者清理 `.harness/` 及其旧忽略规则。

## 独立能力与旧套件迁移

六个入口是 `swe-business-model`、`swe-system-design`、`swe-implementation`、`swe-testing`、`swe-delivery` 和 `swe-adoption`。每个入口只定义自身职责与方法，不识别、调用或交接其他 Skill。由用户或宿主选择当前任务需要的能力，不设置总协调入口。

[swe-adoption](../skills/swe-adoption/SKILL.md) 维护本身及独立 CLI 的项目接线、记录和生命周期，不组织其他 Skill，不建立或重写业务设计。项目已有 YAML、代码与测试默认保留。

迁移旧整套安装时，先保留本地修改，在临时项目分别安装所需 Skill，再独立安装 CLI。验证 PATH、版本和项目检查入口，更新旧 `swe/<领域>/SKILL.md` 与 `swe/bin/swe-cli` 引用。确认旧公共目录不再被项目引用且没有独有内容后，再核对绝对路径并删除。不要先删旧工具再尝试验证原基线。

当前六项版本各自从 `4.0.0` 开始，未来只更新变化的 Skill；同时发行不意味着版本绑定。带机器产物的 Skill 用 `contract.json` 只引用自身参考 Schema；机器校验资源归 CLI 所有，运行不依赖 Skill 目录。

CLI 只检查已编码规则，项目测试和运行工具提供行为证据。文件范围检查不代替共享配置内部条目的差异复核。

返回[项目介绍与快速开始](../README.md)。
