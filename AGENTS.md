# ReedHarness 协作约定

本仓是 ReedHarness 的完整源码位置：`docs/` 保存系统设计与详细使用指南，根目录 `README.md` 保存项目介绍与快速开始，`CONTRIBUTING.md` 保存 Skill 版本登记与构建发布规则，`skills/` 保存可版本化的 Agent 能力，`apps/reed/` 保存临时 Skill 分发工具，测试与构建在本地执行，二进制手动发布到 GitHub Releases。业务项目只下载明确选择的资源，不复制整个仓库，也不依赖全局安装。

## Skill 入口

执行前按需读取目标 Skill 的入口与相关资源：

- [swe](skills/swe/SKILL.md)：adoption 项目接入与五个领域能力的导航、共享规范和 CLI 入口；按任务选择业务模型、系统设计、代码实现、测试或部署。测试产物统一归测试领域。

## 协作约定

先明确任务范围、权威依据和可写目录，只修改当前任务涉及的内容。设计或修改 Skill 时保留有效方法、职责边界和检查要求，不复制第二套共享规则。

按改动类型执行下列验证，检查受影响的链接、JSON、Python/YAML 语法和工作区差异。

文档修改保留有效内容与必要依据，删除或迁移时同步清理引用。Markdown 使用一个一级标题、连续的标题层级、带语言标识的代码块和 UTF-8/LF 格式；检查器位于 `scripts/knowledge_lint.py`。

部署路径和凭据不写入共享源码。提交、推送和发布遵循用户的具体授权。

## 本地验证

统一入口为 `scripts/verify.sh`，包含下列 Go 检查、架构图 JavaScript 交互测试、脚本语法、文档检查及六平台构建；任一步失败立即退出。开发验证需要 Node.js 18+，发布二进制不依赖 Node.js。

命令从仓库根目录执行。修改 Go 代码后检查格式、运行测试与竞态检测、执行 vet，并构建六平台产物：

```bash
find apps/reed -name '*.go' -type f -print0 | xargs -0 gofmt -l
(cd apps/reed && go test -race ./...)
(cd apps/reed && go vet ./...)
apps/reed/scripts/build-release.sh
find apps/swe-cli -name '*.go' -type f -print0 | xargs -0 gofmt -l
(cd apps/swe-cli && go test -race ./...)
(cd apps/swe-cli && go vet ./...)
apps/swe-cli/scripts/build-package.sh
```

`gofmt -l` 应无输出。构建产物位于 `apps/reed/dist/` 和 `apps/swe-cli/dist/`，不提交。仓库只发布 ReedHarness：其发行目录含 reed 二进制、六平台完整 SWE Skill 包与统一摘要；swe-cli 仅作为 Skill 包内文件，不单独发布。交叉编译不代替 Windows 实际运行验证；在 Windows 上分别进入 `apps/reed` 和 `apps/swe-cli` 执行 `go test -race ./...` 和 `go vet ./...`。

修改 Skill 时，按涉及范围运行：

```bash
(cd apps/swe-cli && go test ./internal/checks ./internal/app ./internal/workspace)
```

修改安装或构建脚本时运行：

```bash
sh -n install.sh
bash -n apps/reed/scripts/build-release.sh
bash -n apps/swe-cli/scripts/build-package.sh
```

修改文档后运行：

```bash
python3 -B scripts/knowledge_lint.py --changed
git diff --check
```

Skill 登记表为 `skills/index.json`。仅登记产品能力 SWE，修改其内容时同步维护简介、独立版本和时间，规则见 [贡献指南的 Skill 版本登记](CONTRIBUTING.md#skill-版本登记)。运行 `(cd apps/reed && go test ./internal/installer -run Catalog)` 检查索引结构与 `SKILL.md` 的目录、名称和简介一致性。
