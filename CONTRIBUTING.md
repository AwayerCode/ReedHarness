# 贡献指南

开发与验证遵循 [AGENTS.md](AGENTS.md)。修改范围、验证命令与发布授权以仓库约定为准。

## Skill 版本登记

[skills/index.json](skills/index.json) 只登记 SWE 的独立版本、仓库路径和来源字段，`schema_version` 表示登记表结构版本。名称和简介与 `skills/swe/SKILL.md` 同步维护；构建测试检查登记是否完整，以及目录、名称、简介是否一致（简介比较忽略换行与重复空白）。

| 字段 | 含义 |
| --- | --- |
| `name` | 唯一名称，与目录名一致，使用小写字母、数字和单连字符，最长 64 字符 |
| `path` | 本仓相对路径，SWE 为 `skills/swe`，不改变安装后的名称 |
| `description` | 与 `SKILL.md` frontmatter 一致的简介，用于搜索 |
| `version` | 本仓库维护的 Skill 版本，与 ReedHarness 和 SWE CLI 版本独立 |
| `updated_at` | 当前 Skill 内容的更新时间；已有内容初始化时取最近一次目录提交时间 |
| `source.repository`、`source.path` | SWE 为自有能力，两项均为 `null`；保留字段以维持登记表结构兼容 |

时间使用带时区的 RFC 3339 格式。初始登记的 Skill 从 `0.1.0` 建立独立版本基线，不代表追溯认定历史发布版本。后续内容修正递增 PATCH、兼容能力增加递增 MINOR、不兼容的使用契约变化递增 MAJOR；内容与版本、`updated_at` 同次提交。只修订登记信息时不改变内容版本或内容更新时间。历史记录由 Git 保存。

SWE 内容修改时更新 `updated_at` 和独立版本；仅清理其他登记项或迁移目录且 SWE 内容不变时，不递增 SWE 版本或更新时间。历史内容由 Git 保存。

现有 CLI 使用登记表搜索，SWE 安装到 `.agents/skills/swe/`，同名目标拒绝覆盖。历史归档的路径解析及相应兼容测试保留，不表示当前仓库继续分发其他 Skill。

CLI 尚不按这些独立版本解析安装目标，也不能用它们替代安装命令的 `--version`：安装仍选择仓库标签或 commit。SWE 配套二进制版本继续由其 `runtime.json` 固定。

## 开发版配套检查工具

仓库只创建 ReedHarness Release。`apps/reed/scripts/build-release.sh <版本>` 构建六平台 reed，并调用 SWE 打包脚本，将六个 `swe_<系统>_<架构>.tar.gz` 与 reed 一起放入 `apps/reed/dist/`，生成统一 `SHA256SUMS`。SWE CLI 只存在于 Skill 包内部，不发布裸二进制或 `swe-cli-v*` 标签。

`apps/swe-cli/scripts/build-package.sh <版本>` 可独立用于本地打包验收；中间二进制位于其 `dist/bin/`。打包前校验 SWE 登记表，再从 SWE 源码复制方法与资源，加入对应平台 CLI，并生成不含下载地址的 `runtime.json`。构建不修改 Skill 源码；完整包发布前不能承诺远端安装可用。

跨平台构建不替代实机验收。完整开发验证见[本地验证](AGENTS.md#本地验证)，检查核心的开发说明见 [SWE CLI](apps/swe-cli/README.md)。
