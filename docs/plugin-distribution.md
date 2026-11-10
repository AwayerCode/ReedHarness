# 插件分发与架构服务

## 目标与当前状态

Harn 以插件市场作为正式安装、更新入口。发布包包含 SWE Skill、配套 Go 二进制和宿主适配，不包含完整源码仓库。Skill 方法与渲染核心只维护一份。

当前提供六平台插件验证包、Codex 和 Claude 清单，以及 DSH 后台和页签适配。尚未发布到市场，也未完成各宿主实机安装验收，因此现有安装入口暂时保留；新链路验收后再移除旧入口。

## 两种运行模式

| 入口 | 用途 | 行为 |
| --- | --- | --- |
| `system-design render --file <YAML>` | 分享、归档、离线查看 | 标准输出独立 HTML，随后退出 |
| `system-design serve --root <工作区> --expect-version <版本>` | 实时展示 | 固定监听 `127.0.0.1:47831`，持续运行 |
| 原 `system-design preview` | 已有单文件预览消费者 | 暂时保留原参数与行为 |

两种模式共用 YAML 校验、渲染核心。服务默认只读，不提供文件修改接口。YAML 是唯一持久化架构数据源。

访问 `http://127.0.0.1:47831/harn/` 查看工作区项目列表。`--root` 是允许访问的目录边界；其下多个包含 `system-design.yaml` 的目录共用同一服务。端口占用时明确失败，不更换端口。独立服务的终端使用 Ctrl-C 停止。

服务仅监听 IPv4 回环地址，检查 Host、请求方法和跨站访问。项目路径解析后不能越出工作区，不提供任意磁盘 HTML 或目录文件服务。

## DSH 接入

插件通过 `webServer` 注册 `/harn/`、`/harn/health`、`/harn/resolve`、`/harn/view`、`/harn/state` 固定路由，转发给 Go。前端把会话目录提交给 Go 解析真实路径并验证工作区边界，使用返回的项目 URL 展示同源 iframe。

插件通过 `skills.register` 注册 SWE 入口和资源目录，文档、Schema 和 CLI 从同一插件包加载。

插件配置 `root` 为项目共同工作区；未配置时使用 DSH 进程工作目录。配置条目示例：

```yaml
- insert:
    - id: harn
      name: dsh-harn
      config:
        root: /absolute/workspace
```

插件首次收到请求时连接服务，未启动时调用包内 CLI。复用前核对服务类型、CLI 版本和工作区，不接管不匹配的服务。工作区外的会话显示错误，不扩大授权范围。

服务区分所有者。插件启动时使用 `--parent-stdin`，退出时关闭管道，Go 停止服务；宿主异常退出导致管道断开时同样停止。插件只注销自己注册的 Skill、路由和服务，不停止复用的独立服务。升级后由新插件启动新版本；旧版本退出期间短暂重试。独立服务发生版本或工作区冲突时明确报错，由其使用者停止。插件管理的服务停止期间，独立浏览器页面会显示断连提示，新服务启动后继续重试。

Go 服务只允许同源嵌入，原单文件预览仍禁止嵌入。代理保留 CSP 和缓存策略。

## 实时更新与编辑

页面每秒请求状态，内容变化后整页刷新；校验失败或连接中断时提示旧图失效，恢复后继续更新。状态请求保留项目标识，多个项目不会串图。首次打开即无效的 YAML 返回诊断，修复后需重新打开页面。

后期页面编辑由 Go 提供保存接口，包含保存前校验、版本冲突检查和草稿管理。有未保存草稿时不能直接整页刷新。本次不提前实现编辑接口。

## 构建与市场接入

从仓库根目录运行：

```bash
bash plugins/build.sh 0.1.0
```

版本为无 `v` 前缀的 SemVer。复用现有 SWE 六平台构建，输出 `apps/swe-cli/dist/harn-plugin_<系统>_<架构>.tar.gz`，展开目录在 `apps/swe-cli/dist/plugins/<版本>/<平台>/`。包内 `runtime.json` 固定 CLI 版本与摘要。

Codex、Claude 清单由相同版本生成，均引用 `./skills/`，各平台验证包包含完整运行资源。两个市场的平台选择及实际安装仍需验证，不能将构建成功描述为市场可安装。

DSH npm 主包位于构建目录的 `dsh-harn/`，六个平台子包为 `dsh-harn-<系统>-<架构>/`。主包以固定版本的 `optionalDependencies` 选择平台，安装脚本校验版本和 SHA-256 后将二进制放入 Skill。禁用安装脚本时需显式运行包内 `node install.mjs`。发布顺序为六个子包、主包、市场登记；本地平台验证归档不是六次同名 npm 发布。

接口依据见 [Codex 插件打包](https://developers.openai.com/plugins/build/plugins)、[Claude 插件清单](https://code.claude.com/docs/en/plugins-reference)和 [DSH Skill 注册接口](https://github.com/deepseek-ai/deepseek-harness/blob/master/packages/skill/skill/src/index.ts)。
