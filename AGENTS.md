# ReedHarness 协作约定

本仓是 ReedHarness 的完整源码位置：`docs/` 保存系统设计与详细使用指南，根目录 `README.md` 保存项目介绍与快速开始，`CONTRIBUTING.md` 保存 Skill 维护与构建发布规则，`skills/` 保存可版本化的 Agent 能力，`apps/reed/` 保存临时 Skill 分发工具，测试与构建在本地执行，二进制手动发布到 GitHub Releases。业务项目只下载明确选择的资源，不复制整个仓库，CLI 独立安装到 PATH。

## Skill 入口

执行前按需读取目标 Skill 的入口与相关资源：

- [swe-business-model](skills/swe-business-model/SKILL.md)：业务实体、行为、流程与规则。
- [swe-system-design](skills/swe-system-design/SKILL.md)：系统架构、YAML 与数据契约。
- [swe-implementation](skills/swe-implementation/SKILL.md)：生产代码与内部组装。
- [swe-testing](skills/swe-testing/SKILL.md)：统一维护所有测试产物。
- [swe-delivery](skills/swe-delivery/SKILL.md)：发布、部署与恢复。
- [swe-adoption](skills/swe-adoption/SKILL.md)：项目接入、更新与退出。

## 协作约定

先明确任务范围、权威依据和可写目录，只修改当前任务涉及的内容。设计或修改 Skill 时保留有效方法、职责边界和检查要求，各 Skill 自带必要专业资源，不引用其他 Skill，不复制整套宿主行为规范。

按改动类型执行下列验证，检查受影响的链接、JSON、Python/YAML 语法和工作区差异。

文档修改保留有效内容与必要依据，删除或迁移时同步清理引用。Markdown 使用一个一级标题、连续的标题层级、带语言标识的代码块和 UTF-8/LF 格式；检查器位于 `scripts/knowledge_lint.py`。

部署路径和凭据不写入共享源码。提交、推送和发布遵循用户的具体授权。

## 本地验证

统一入口为 `scripts/verify.sh`，包含下列 Go 检查、架构图 JavaScript 交互测试、脚本语法、文档检查、六平台构建，以及插件打包与 DSH 集成测试；任一步失败立即退出。构建、打包和插件使用同一个版本值。开发验证需要 Node.js 18+，发布二进制不依赖 Node.js。

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

`gofmt -l` 应无输出。构建产物位于 `apps/reed/dist/` 和 `apps/swe-cli/dist/`，不提交。发行目录含 reed、六平台独立 `swe-cli_<os>_<arch>.tar.gz`、独立纯文本 `<skill-name>.tar.gz` 和摘要；Skill 不带二进制。交叉编译不代替 Windows 实际运行验证；在 Windows 上分别进入 `apps/reed` 和 `apps/swe-cli` 执行 `go test -race ./...` 和 `go vet ./...`。

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

`skills/` 下的六个目录就是唯一事实来源，没有登记表：构建、插件打包和安装都直接读取目录与 `SKILL.md`，版本由发行标签或 commit 标识。修改内容时同步该 Skill 的 `SKILL.md` 简介，规则见 [贡献指南的 Skill 维护](CONTRIBUTING.md#skill-维护)。
