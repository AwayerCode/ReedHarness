# 贡献指南

开发与验证遵循 [AGENTS.md](AGENTS.md)。修改范围、验证命令与发布授权以仓库约定为准。

## Skill 维护

`skills/` 下的六个目录就是唯一事实来源，仓库不维护登记表：构建、插件打包和安装都直接读取目录与 `SKILL.md`，无需同步第二份名称或简介清单。新增 Skill 只需创建 `skills/<name>/SKILL.md`，名称与目录一致，小写字母、数字和单连字符，最长 64 字符。

Skill 的版本由发行标签或 commit 标识，不单独维护语义版本号。修改内容时同步该 Skill 的 `SKILL.md` frontmatter 简介；历史由 Git 保存。独立文本包只包含选定 Skill；解压校验后放入宿主 Skill 目录，插件资源通过宿主管理。Skill 不生成编译资源；修改后验证独立资源、文档契约与打包。

每个 Skill 自带必要方法、模板、专业规范和参考 Schema，不链接或感知其他 Skill，不组织跨 Skill 交接。宿主已有的通用行为规范不整套复制。CLI 固有 Schema 位于 `apps/swe/resources/`，由 CLI 生成内嵌资源；Skill 中的参考副本用于理解自身输入输出，不是 CLI 运行时依赖，`(cd apps/swe && go test ./internal/checks)` 会拒绝两者漂移。

只有产出机器可校验产物的 Skill 才带 `contract.json`，用来声明适用产物类型和包内参考 Schema 位置；`swe-adoption` 不产出这类产物，因此没有该文件。

## 独立构建与发行

`apps/swe/scripts/build-release.sh <版本>` 构建六平台独立 `swe_<os>_<arch>.tar.gz`，内含 `swe/bin/` 与版本摘要清单；同时为 `skills/` 下每个 Skill 生成独立的 `<skill-name>.tar.gz` 文本包，归档保存 `skills/<skill-name>/`。Skill 包只接受 UTF-8 文本：含 NUL 或其他二进制内容的文件使构建失败，归档统一以 `0644` 保存。Skill 包不带二进制或运行时绑定清单，与操作系统无关。CLI 使用独立 PATH，不依赖 Skill 安装路径。

同次发行的二进制、Skill 包、插件使用同一版本。`apps/reed/scripts/build-release.sh <版本>` 单独构建管理服务；服务按配置读取 Skill 来源。构建不修改 Skill 源码，未上传的产物不能声称远端可安装。

跨平台构建不替代实机验收。完整开发验证见[本地验证](AGENTS.md#本地验证)，应用说明见 [reed 服务](apps/reed/README.md)和 [swe CLI](apps/swe/README.md)。

## 方法与规则维护

区分通用工具维护与项目采用；项目特例保存在项目约定，不自动提升为通用规则。修改方法时保留有效判断、案例和职责边界，为关键约束明确对象、依据、检查方式及失败处理。格式参考与 CLI 检查语义保持兼容，语义判断不强行脚本化。

检查器使用有意义的合法和违规样例，既验证合格产物通过，也验证违规被阻止。每个 Skill 应在仅有自身目录时通过链接与资源完整性检查，不依赖安装其他 Skill。检查版本兼容、引用、纯文本包内容和独立安装；构建或安装变化运行相应测试，不自动提交或发布。
