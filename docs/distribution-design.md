# 分发设计

## 目标与当前状态

本文负责二进制、Skill 和插件的构建、打包、安装、更新与卸载。系统职责与阶段见[系统设计](system-design.md)，命令、结果协议与服务运行约束见[应用设计](app-design.md)。

当前提供独立 `swe-cli`、纯文本 Skill、六平台插件验证包、Codex 和 Claude 清单，以及 DSH 后台和页签适配。尚未发布到市场，也未完成各宿主实机安装验收。当前安装入口仍有效；P0 的目标是统一为 `reed`，移除旧 Skill 安装器与 `install.sh`，同步全部分发入口。

插件市场作为宿主内的正式安装、更新入口，独立二进制仍可单独使用。本文的 `swe-cli` 包名、路径与安装命令描述现状，不表示合并后继续保留旧程序。

## 产物组成与来源

| 产物 | 内容与边界 |
| --- | --- |
| 独立二进制 | 当前为 `swe-cli`；目标为内嵌 Skill 的 `reed`，提供调度、约束与检查 |
| 纯文本 Skill 包 | 每个 Skill 自包含知识、方法、模板与 Schema，不携带二进制，不依赖其他 Skill |
| 宿主插件 | 组合 Skill、独立工具目录中的二进制与宿主适配；二进制不放进 Skill 目录 |

仓库 `skills/` 下的六个目录是 Skill 的唯一源码来源；目标 `reed` 内嵌与插件打包读取同一份内容，不维护第二份源码。插件组合不改变 Skill 的独立资源边界。

## 安装与版本

CLI 独立发行 `swe-cli_<os>_<arch>.tar.gz`，归档包含 `swe-cli/bin/` 与版本摘要清单。校验发行摘要后将二进制目录加入 PATH，通过 `swe-cli version` 确认版本；Windows 使用 `swe-cli.exe`。Skill 是独立纯文本包，不携带二进制，也不绑定其他 Skill 的版本。项目记录所用 CLI 版本与来源；进行中的范围基线继续使用创建它的工具，不通过升级重建基线掩盖改动。

Skill 可用 `reed install --source <Skill包URL> --version tag:<标签> --path skills/<skill-name>` 独立安装。插件管理的资源通过宿主更新，不覆盖宿主管理目录。CLI 缺失或版本不兼容时报告无法检查，按项目约定安装独立 CLI。

## 构建、版本与市场接入

从仓库根目录运行：

```bash
bash plugins/build.sh 0.1.0
```

版本为无 `v` 前缀的 SemVer。复用独立 CLI 六平台构建及六个纯文本 Skill 包，输出 `apps/swe-cli/dist/reed-plugin_<系统>_<架构>.tar.gz`，展开目录在 `apps/swe-cli/dist/plugins/<版本>/<平台>/`。包内 `runtime.json` 固定 CLI 版本与摘要。

Codex、Claude 清单由相同版本生成，均引用 `./skills/`，各平台验证包包含完整运行资源。两个市场的平台选择及实际安装仍需验证，不能将构建成功描述为市场可安装。

DSH npm 主包位于构建目录的 `dsh-reed/`，六个平台子包为 `dsh-reed-<系统>-<架构>/`。主包以固定版本的 `optionalDependencies` 选择平台，安装脚本校验子包 SHA-256 后将二进制放入独立工具目录并向宿主暴露命令；插件版本不与 CLI 版本绑定，`runtime.json` 自带 CLI 版本由服务启动时核对。禁用安装脚本时需显式运行包内 `node install.mjs`。发布顺序为六个子包、主包、市场登记；本地平台验证归档不是六次同名 npm 发布。

接口依据见 [Codex 插件打包](https://developers.openai.com/plugins/build/plugins)、[Claude 插件清单](https://code.claude.com/docs/en/plugins-reference)和 [DSH Skill 注册接口](https://github.com/deepseek-ai/deepseek-harness/blob/master/packages/skill/skill/src/index.ts)。

构建、打包与生成清单使用同一次发行的版本值；现有协议允许 Skill、插件与 CLI 独立演进，不意味着每次发行必须人为指定不同版本。运行时实际使用的 CLI 版本与摘要由 `runtime.json` 明确记录，服务复用按应用设计核对。

目标产物改为 `reed` 后，构建脚本、目录、命令暴露与清单必须一起迁移；迁移前继续使用上述现有路径，不将尚未实现的路径作为安装说明。

## DSH 宿主接入

插件通过 `skills.register` 分别注册六个 `swe-*` 入口，不注册总协调入口。每个 Skill 从自身目录加载必要文档和参考 Schema，彼此无依赖；当前宿主将独立 CLI 暴露为 `swe-cli` 命令，P0 后改为 `reed`。

插件通过 `webServer` 注册并代理[应用设计定义的固定路由](app-design.md#路由与宿主协议)，前端使用 Go 返回的项目 URL 展示同源 iframe。插件配置 `root` 为项目共同工作区；未配置时使用 DSH 进程工作目录。

```yaml
- insert:
    - id: reed
      name: dsh-reed
      config:
        root: /absolute/workspace
```

插件首次收到请求时连接服务，未启动时调用包内 CLI。复用、启动和关闭遵循[服务所有权与生命周期](app-design.md#所有权与生命周期)。插件卸载只注销自己注册的 Skill、路由与自己启动的服务。

## 更新、卸载与迁移

插件管理的资源通过宿主更新和卸载，独立安装不覆盖宿主管理目录。独立二进制按发行摘要校验后替换；移除独立安装时只清理其二进制与 PATH 配置，不清理业务项目的目标、任务或交付。

P0 移除旧入口前，替代的二进制、Skill 获取方式及插件调用链必须可用并完成相应验证。市场尚未验收时不能把市场安装作为唯一替代入口，也不能将本地验证包描述为市场可安装。旧安装器不继续保留为备用路径；迁移完成条件统一以系统设计为准。

当前范围基线不得借升级重建；目标调度运行记录的跨版本读取与迁移须遵循应用设计确定的格式兼容规则，格式未确定前不承诺无损接续升级。

## 待验证事项

1. Codex、Claude 市场的平台选择与实机安装、更新、卸载。
2. P0 合并后的二进制与 Skill 获取方式、插件命令暴露和资源完整性。
3. 目标运行记录格式确定后的升级兼容要求。
