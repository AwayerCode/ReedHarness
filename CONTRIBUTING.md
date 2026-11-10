# 贡献指南

开发与验证遵循 [AGENTS.md](AGENTS.md)。修改范围、验证命令与发布授权以仓库约定为准。

## Skill 版本登记

[skills/index.json](skills/index.json) 统一记录自有与第三方 Skill 的独立版本、仓库路径和来源，`schema_version` 表示登记表结构版本。名称与目录中的 `SKILL.md` 一致，每个目录只登记一次。自有 Skill 位于 `skills/<name>`，第三方 Skill 位于 `skills/3rdparty/<name>`；`3rdparty` 只是维护归属分组，不是可安装 Skill。搜索读取登记表的名称、`path` 和 `description`。旧登记表缺少 `path` 时仍按 `skills/<name>` 解析。名称和简介与入口文件同步维护；构建测试检查这两类公开 Skill 是否完整登记，以及目录、名称、简介是否一致（简介比较忽略换行与重复空白）。

| 字段 | 含义 |
| --- | --- |
| `name` | 唯一名称，与目录名一致，使用小写字母、数字和单连字符，最长 64 字符 |
| `path` | 本仓相对路径，只接受 `skills/<name>` 或 `skills/3rdparty/<name>`，不改变安装后的名称 |
| `description` | 与 `SKILL.md` frontmatter 一致的简介，用于搜索 |
| `version` | 本仓库维护的 Skill 版本，与 ReedHarness 和 SWE CLI 版本独立 |
| `updated_at` | 当前 Skill 内容的更新时间；已有内容初始化时取最近一次目录提交时间 |
| `source.repository`、`source.path` | 自有 Skill 均为 `null`；外部 Skill 填上游仓库 URL 和原始目录，仓库根目录为 `"."`；此字段不是本仓 `path`，迁移本仓目录时不改写 |

时间使用带时区的 RFC 3339 格式。初始登记的 Skill 从 `0.1.0` 建立独立版本基线，不代表追溯认定历史发布版本。后续内容修正递增 PATCH、兼容能力增加递增 MINOR、不兼容的使用契约变化递增 MAJOR；内容与版本、`updated_at` 同次提交。只修订登记信息时不改变内容版本或内容更新时间。历史记录由 Git 保存。

所有 Skill 使用相同字段结构。首次登记时填写 `updated_at`，后续内容修改或同步上游时更新。引入外部 Skill 时保留原有许可文件和署名，确认许可允许分发；同步时核对本地修改，并递增本仓维护的 `version`。首次加入时间、导入内容及本地修改历史由本仓 Git 保存。现有导入未记录可靠的上游版本或 commit，不猜填；后续同步应在提交说明中记录核验的上游修订。仅迁移目录且文件内容不变时，不递增 Skill 内容版本或更新时间。

新版 CLI 使用登记表搜索，并将旧的 `skills/<name>` 安装路径解析到登记的仓库路径。新旧路径都安装为 `.agents/skills/<name>/`，同名目标仍拒绝覆盖。独立归档没有登记表时保持原有按路径安装方式。旧版 ReedHarness 不理解第三方新布局，需使用本次构建或包含该变更的后续发行版；历史标签仍可按历史路径安装。

CLI 尚不按这些独立版本解析安装目标，也不能用它们替代安装命令的 `--version`：安装仍选择仓库标签或 commit。SWE 配套二进制版本继续由其 `runtime.json` 固定。

## 开发版配套检查工具

仓库只创建 ReedHarness Release。`apps/reed/scripts/build-release.sh <版本>` 构建六平台 reed，并调用 SWE 打包脚本，将六个 `swe_<系统>_<架构>.tar.gz` 与 reed 一起放入 `apps/reed/dist/`，生成统一 `SHA256SUMS`。SWE CLI 只存在于 Skill 包内部，不发布裸二进制或 `swe-cli-v*` 标签。

`apps/swe-cli/scripts/build-package.sh <版本>` 可独立用于本地打包验收；中间二进制位于其 `dist/bin/`。打包前校验包含第三方目录的统一登记表，再从 SWE 源码复制方法与资源，加入对应平台 CLI，并生成不含下载地址的 `runtime.json`。第三方 Skill 继续通过仓库源码归档按需分发，不混入 SWE 平台包，不建立第二套安装机制。构建不修改 Skill 源码；完整包发布前不能承诺远端安装可用。

跨平台构建不替代实机验收。完整开发验证见[本地验证](AGENTS.md#本地验证)，检查核心的开发说明见 [SWE CLI](apps/swe-cli/README.md)。
