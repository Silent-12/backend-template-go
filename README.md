# 🧱 GoFrame 后端模板

基于 **GoFrame v2.10.4** 的后端基础模板，能力对齐同系列 Nest 基础模板。最低 Go 1.23，建议使用受支持的较新 Go 版本；当前在 Go 1.27.3 / Windows 上验证。

模板开箱即用：默认关闭 MySQL、Redis 和限流，无需外部依赖即可启动。适合作为多个独立后端服务的起点，复制派生后按需接入数据库、缓存和业务模块。示例模块在依赖关闭时返回明确的 503 / 501 降级提示，不阻塞启动。

> 派生新服务时，先替换 `go.mod` 模块名与内部导入，再设置独立的 `APP_NAME`、API 前缀、数据库和 Redis 命名空间。`APP_NAME` 同时用于运行时服务名、文档标题及默认 Redis 命名空间，同一服务的多副本保持一致，不同服务必须不同。

---

## ✨ 已内置能力

| 能力 | 说明 |
| --- | --- |
| 标准分层 | `api → controller → service → repository`，依赖构造函数注入 |
| 示例业务纵切 | `api/example → controller/example → service/example → repository/example` 只读范例，接线事务、缓存和出站客户端 |
| 统一响应与错误 | `code` / `message` / `data` / `timestamp` / `traceId`，HTTP 状态码与业务码一致 |
| 参数校验 | 路径、查询、Body 标量类型检查，请求体上限 2 MiB |
| 在线接口文档 | Knife4x Go v0.9.2 内嵌，无需 Node / CDN，可在线调试 |
| 健康探针 | `/livez`、`/readyz`，就绪探针并行检查已启用依赖 |
| 请求追踪 | 服务端生成追踪号，拒绝入站 `traceparent` / `tracestate`，出站自动透传 |
| 访问日志 | 有界异步队列写入 `access_logs`，不阻塞业务请求 |
| 外部 HTTP | 统一 Resty v2.17.2 客户端工厂与 Registry 生命周期管理 |
| Redis 能力 | 公共 Cache、Lua 原子限流、未知路由封禁 |
| 安全基础 | CSP 等安全响应头、CORS 白名单、可信代理 CIDR、错误脱敏 |
| 工程质量 | goimports、Revive、模板约定检查、Git Hooks、Docker 多阶段构建 |

---

## 🚀 快速开始

在项目根目录使用 PowerShell：

```powershell
Copy-Item .env.example .env
go mod download
go run .
```

启动后可直接访问：

| 入口 | 默认地址 |
| --- | --- |
| 健康接口 | `http://localhost:3000/backend/v1/health` |
| 接口文档 | `http://localhost:3000/backend/doc.html` |
| OpenAPI JSON | `http://localhost:3000/backend/api-json` |
| 存活探针 | `http://localhost:3000/backend/livez` |
| 就绪探针 | `http://localhost:3000/backend/readyz` |

文档由 GoFrame 生成 OpenAPI 3 JSON，Knife4x 提供模型浏览与在线调试；UI、动态分块与 Schema Worker 均随 Go module 内嵌到二进制。页面 CSP 仅允许同源资源、Worker 和调试请求，内联初始化脚本使用每次请求生成的 nonce。

> 项目没有内置账户、JWT 或 RBAC。业务接口正式对外开放前，应接入业务认证与权限控制。Knife4x 会在浏览器保存调试设置及授权信息，共用电脑使用后应在界面清理本地数据。

---

## 📁 项目结构

```text
main.go                     进程入口，调用 cmd.Main
Dockerfile                  多阶段构建，普通用户 + 生产模式启动
.dockerignore               构建上下文排除 .env、bin、logs
.editorconfig               编码、换行与缩进约定（Go 使用制表符）
AGENTS.md                   仓库规范与交付检查约定
api/                        请求 / 响应 DTO 与路由元数据
  example/v1/               示例模块协议，派生服务可替换或删除
internal/
  cmd/                      命令入口与停止信号处理
  config/                   配置加载、默认值和校验
  controller/               HTTP 适配；不直接访问数据库
    example/                示例模块协议适配
  service/                  业务服务与仓储接口，构造函数注入依赖
    example/                示例模块业务规则与事务边界
  repository/               手写持久化实现，组合连接与生成的表 / 列元数据
    example/                示例模块只读持久化
  dao/                      GoFrame CLI 生成的 DAO，不手工修改
  model/                    业务输入、响应投影与分页模型
    do/                     CLI 生成的数据库写入对象
    entity/                 CLI 生成的数据库行结构，不直接返回给接口调用方
  httpclient/               统一的 Resty v2 客户端工厂与 Registry
  infrastructure/           MySQL / Redis 连接、事务入口与实时探测
  loglimit/                 同类告警的时间窗口合并，避免故障期日志洪泛
  middleware/               响应、错误、安全、追踪、限流
  accesslog/                有界队列异步访问日志
  server/                   路由组装与 HTTP 测试
manifest/
  config/config.yaml        GoFrame 日志配置
  sql/                      按版本顺序执行的数据库迁移
  docker/compose.test.yaml  专用本地集成测试依赖
hack/install-tools.ps1      安装固定版本 goimports、Revive、gf 并启用 Git Hooks（Windows）
hack/install-tools.sh       同上（Linux / macOS）
hack/check.ps1              Windows 排版、GoDoc、模板约定、测试和构建检查
hack/check.sh               跨平台检查入口
hack/quality/               Resty / 分层 / 常见 ORM 写入约定检查
.githooks/pre-commit        提交前调用统一检查入口
.githooks/commit-msg        校验单行中文 Conventional Commit 标题
```

### 示例业务纵切

`example` 模块是贯穿四层的可运行范例，只读复用模板自带的 `access_logs` 表，不新增业务表、不写入领域规则、上游地址或凭据。派生服务照它扩展第一个业务模块，或直接删除：

| 层 | 位置 | 职责 |
| --- | --- | --- |
| API | `api/example/v1` | DTO、`g.Meta` 路由元数据、校验与 `dc` 标签 |
| Controller | `internal/controller/example` | 定义最小 Service 接口，做协议适配与响应投影 |
| Service | `internal/service/example` | 定义最小 Repository 接口，拥有事务边界，组合缓存与出站客户端 |
| Repository | `internal/repository/example` | 用 `DBFromContext` 复用事务连接，列名取自生成的列元数据 |

装配发生在 `internal/server/server.go` 的 `Build`，把此前只有测试使用的基础能力接入启动链路：

- `infrastructure.NewTransactor` 注入 Service，`ListLogs` 在事务回调内完成总数与当页记录的一致性读；事务内不发起 Redis 或 HTTP 调用。
- `infrastructure.DBFromContext` 在 Repository 中取得连接，仓储不自行开启事务。
- `infrastructure.NewCache` 注入 Service，缓存示例上游探测结果 5 秒；Redis 未启用或失败时降级为直接请求。
- `app.HTTPClients().New` 通过应用注册表创建 Resty 客户端，Service 每次请求用 `SetContext(ctx)` 传递上下文，由 `App.Close` 统一关闭。

| 接口 | 依赖 | 未启用时 |
| --- | --- | --- |
| `GET {前缀}/v1/example/logs?page=1&pageSize=20` | MySQL 已启用且已执行 V2 迁移 | 503 |
| `GET {前缀}/v1/example/upstream` | `EXAMPLE_UPSTREAM_ORIGIN` 已配置 | 501 |

示例路由暂放模块根目录（`/v1/example/...`）；派生服务应按实际端别迁移到 `/v1/admin/...` 或 `/v1/user/...`，并把 `EXAMPLE_UPSTREAM_ORIGIN` 换成自己的可信上游。删除示例模块时，移除上述四个目录、`Build` 与 `registerBusinessRoutes` 中的示例装配、`EXAMPLE_UPSTREAM_ORIGIN` 配置项及配置表对应行。

### 新增业务模块

以下顺序以上一节 `example` 模块为可运行参照：

1. 在 `api/<module>/v1` 定义请求 / 响应 DTO 与 `g.Meta` 路由信息。
2. 在 `internal/controller` 做协议适配，在 `internal/service` 实现业务规则。
3. 在 `internal/repository` 完成持久化，并在 `internal/server/server.go` 注册路由。

后台接口使用 `/v1/admin/...`，用户端使用 `/v1/user/...`；跨端复用的领域逻辑放 `shared`。全局前缀来自 `API_PREFIX`（默认 `backend`），破坏性变更请新建 `v2`。

文件上传由业务侧按实际需求实现，统一上传到 OSS；模板不提供本地上传目录、路径写入、静态文件托管或 OSS 上传接口。

### 数据访问约定

- 生成文件（`internal/dao`、`internal/model/do`、`internal/model/entity`）由 `gf gen dao` 维护，禁止手工修改。
- 运行时连接通过 `infrastructure.Resources` 注入；Repository 使用生成的表 / 列元数据和 DO，避免走生成 DAO 的默认 `g.DB()` 全局入口。
- 数据写入不使用 map；创建 / 更新时间由 ORM 自动维护，使用 `SoftTimeTypeTimestampMilli` 保持毫秒单位。
- 数据库结构变更只新增 `manifest/sql/V{序号}__{描述}.sql`，按顺序执行一次；已执行的迁移不可修改或删除。

### 跨仓储事务

事务边界由 Service 拥有，而不是仓储。装配层向业务服务注入 `*infrastructure.Transactor`，仓储通过 `infrastructure.DBFromContext` 复用 ctx 中的事务连接，**不自行开启事务**。

```go
// 事务入口由装配层注入；ctx 含事务时直接加入，不含时开启新事务。
err := tx.Transaction(ctx, func(ctx context.Context) error {
    if err := repoA.Update(ctx, itemNo, patch); err != nil {
        return err
    }
    return repoB.Create(ctx, log)
})
```

需要跨多个仓储原子写入时，在 Service 中调用 `tx.Transaction`，把回调内的 ctx 继续传给各仓储方法。不要在仓储内再调用 `Transaction`，也不要在长事务中发起外部 HTTP 或 Redis 调用。

---

## ⚙️ 配置

配置优先级：**操作系统环境变量 > `.env` > 内置默认值**。`.env` 只从**当前工作目录**读取，运行二进制前请进入项目 / 部署目录。文件值只合并为当前配置快照，不写入进程环境；布尔值严格使用 `true` / `false`。启动时会校验服务名、Redis 前缀、端口、代理 CIDR、依赖开关关系和文档凭据。

密码类配置保留原始值，不删除首尾空格；在环境文件中需要空格时请用引号包裹密码。`CORS_ORIGINS` 每项必须是完整 HTTP/HTTPS 来源（协议、主机和可选端口），不能包含路径、查询、凭据或主机通配符。`TRUSTED_PROXY_CIDRS` 只接受具体网段，`0.0.0.0/0` 与 `::/0` 会被拒绝，否则任何客户端都能伪造 `X-Forwarded-For`。

> 已部署旧版本升级时，将原 `.env.local` 中仍需使用的配置合并到 `.env`，重名键保留原本生效的值；应用不再读取 `.env.local`。

### 配置项

`.env.example` 中的键与下表一致（`DB_DATABASE` 以注释形式保留，必须由派生服务自行填写）。默认值以 `internal/config` 代码为准；修改配置必须同步默认值、`internal/config` 校验、`.env.example` 与本表。

| 键名 | 默认值 | 说明 | 生效条件 |
| --- | --- | --- | --- |
| `APP_ENV` | `development` | 运行环境，决定文档、健康诊断、跨域与安全响应头分支 | 仅接受 `development`、`test`、`production` |
| `APP_NAME` | `backend-template-go` | 服务名，同时作为运行时名称、文档标题和默认 Redis 命名空间 | 小写字母开头，1～63 位小写字母、数字、下划线或连字符 |
| `HOST` | `0.0.0.0` | HTTP 监听地址 | 不可为空 |
| `PORT` | `3000` | HTTP 监听端口 | 1～65535 |
| `API_PREFIX` | `backend` | 全局业务路由前缀，自动补前导斜杠并去掉首尾斜杠 | 允许字母、数字、下划线、连字符，可用 `/` 分段；留空表示不加前缀 |
| `CORS_ORIGINS` | 空 | 允许跨域访问的来源，逗号分隔 | 每项为完整 HTTP/HTTPS 来源；生产环境禁止 `*` |
| `TRUSTED_PROXY_CIDRS` | 空 | 可信代理 CIDR，逗号分隔，命中时才解析 `X-Forwarded-For` | 合法 CIDR；拒绝 `0.0.0.0/0` 与 `::/0` |
| `HTTP_LOG_ENABLED` | `false` | 将访问日志写入 `access_logs` | 需 `MYSQL_ENABLED=true` |
| `DOCS_ENABLED` | `true` | 注册 OpenAPI JSON 与在线调试页面 | 生产环境解析配置时强制关闭 |
| `DOCS_AUTH_ENABLED` | `false` | 为文档入口启用 Basic 认证 | 需 `DOCS_ENABLED=true` 且非生产；要求用户名与密码非空 |
| `DOCS_USERNAME` | 空 | 文档认证用户名 | 启用文档认证时必填 |
| `DOCS_PASSWORD` | 空 | 文档认证密码，保留首尾空格 | 启用文档认证时必填 |
| `EXAMPLE_UPSTREAM_ORIGIN` | 空 | 示例模块出站请求的可信来源，仅 http/https origin，不含路径、查询、片段或通配符 | 留空时示例上游探测返回 501；派生服务替换为自己的上游 |
| `MYSQL_ENABLED` | `false` | 启用 MySQL 连接 | 要求 `DB_HOST`、`DB_USERNAME`、`DB_DATABASE` 非空；启动探测失败即退出 |
| `DB_HOST` | `127.0.0.1` | MySQL 地址 | 启用 MySQL 时不可为空 |
| `DB_PORT` | `3306` | MySQL 端口 | 1～65535 |
| `DB_USERNAME` | `app` | MySQL 用户名 | 启用 MySQL 时不可为空 |
| `DB_PASSWORD` | 空 | MySQL 密码，原样读取、不去空格 | 启用 MySQL 时按需填写 |
| `DB_DATABASE` | 空 | 数据库名，模板不提供可直接使用的默认库 | 启用 MySQL 时必须显式设置 |
| `DB_LOGGING` | `false` | ORM 调试日志，可能包含 SQL 与参数 | 仅在启用 MySQL 时生效，生产环境保持关闭 |
| `REDIS_ENABLED` | `false` | 启用 Redis 连接、公共 Cache 与可选限流 | 要求 `REDIS_HOST` 非空；启动探测失败即退出 |
| `REDIS_HOST` | `127.0.0.1` | Redis 地址 | 启用 Redis 时不可为空 |
| `REDIS_PORT` | `6379` | Redis 端口 | 1～65535 |
| `REDIS_USERNAME` | 空 | Redis ACL 用户名 | 使用默认用户时可留空 |
| `REDIS_PASSWORD` | 空 | Redis 密码，原样读取、不去空格 | 无认证时可留空 |
| `REDIS_DB` | `0` | Redis 逻辑数据库编号 | 0～15 |
| `REDIS_KEY_PREFIX` | 继承 `APP_NAME` | 服务命名空间，Cache 自动附加 `:cache:` | 1～128 位字母、数字、冒号、下划线或连字符，以字母或数字开头，禁止花括号 |
| `THROTTLE_ENABLED` | `false` | 启用 API 频率限制与未知路由封禁 | 需 `REDIS_ENABLED=true` 且 `THROTTLE_KEY_PREFIX` 非空 |
| `THROTTLE_KEY_PREFIX` | 继承 `REDIS_KEY_PREFIX` | 限流命名空间 | 同前缀规则；显式设置的旧前缀仍然优先 |

---

## 🧭 请求处理与响应

### 统一响应

业务 JSON 响应统一包含 `code`、`message`、`data`、`timestamp`、`traceId`，`code` 与实际 HTTP 状态一致；写操作成功返回 `data: null`。失败使用实际 HTTP 状态码：请求校验为 400，资源不存在为 404，限流为 429，未知错误为 500。GoFrame 的认证、权限、资源不存在和服务繁忙错误码也会映射到对应 HTTP 状态。

显式状态码不会被改回 200；通用异常日志仅记录追踪号、错误码和错误类型，避免原始 SQL / URL / panic 文本携带凭据或业务数据。下载和健康探针不套业务响应包装；流或下载尚未提交时的异常同样会脱敏。

### 分页约定

页码分页使用 `{ list, pagination: { page, pageSize, total, totalPages } }`，空列表为 `[]`；导出条件使用独立模型，不复用分页 DTO。

### 请求限制

- 请求体上限 2 MiB，超出返回 413。
- 读超时 15 秒，写超时 30 秒，优雅关闭等待 10 秒。
- 标量参数在 GoFrame 转换前检查类型，路径中的资源编号不允许被同名 query / body 参数覆盖。

---

## 🔍 请求追踪与访问日志

### 追踪信任边界

服务端**不接受**入站 `traceparent` / `tracestate`：请求进入时先删除这两个头（同时删除可覆盖路由的框架内部头），再由服务端生成追踪编号。响应体 `traceId`、响应头 `X-Trace-Id`、GoFrame 框架 `Trace-Id` 和 `access_logs.trace_id` 必须同源。

出站请求方向相反：`httpclient` 的 `SetPreRequestHook` 会用当前 Context 重建 `X-Trace-Id` 与 W3C `traceparent` / `tracestate`，因此上游看到的是受控的服务端链路信息。

默认使用 TCP 对端 IP，**不直接信任代理头**。位于反向代理后时，配置 `TRUSTED_PROXY_CIDRS`，并让入口代理覆盖或正确追加 `X-Forwarded-For`。日志与限流使用相同的解析结果。

### 访问日志

运行日志由 `manifest/config/config.yaml` 配置，写入 `logs` 并输出控制台。时间格式为 `YYYY-MM-DD HH:mm:ss`。每个入站 HTTP 请求都会记录 `method` 和转义后的 `url` 路径，省略查询参数。日志按日期命名、100 MB 轮转，闲置超过 24 小时的旧日志也会进入轮转，备份最多 10 份、最长保留 7 天。

`HTTP_LOG_ENABLED=true` 要求 MySQL 已开启，并写入 `access_logs`；仅记录路由模板、方法、状态、耗时、IP 和追踪号。队列容量 256，满时丢弃并告警；停止时最多等待 5 秒处理日志。`access_logs` 的保留期与清理策略由部署侧负责，模板不提供定时清理或分区任务。

---

## 🗄️ MySQL 与访问日志

新库需要访问日志时，执行一次 [V2__create_access_logs.sql](manifest/sql/V2__create_access_logs.sql)，创建的表中追踪编号为 `CHAR(32)`。脚本不带 `IF NOT EXISTS`；应用不自动建表或执行迁移。

该 SQL 是新的初始化基线，不适用于已经执行旧 V2 的数据库：不要在已有表上重跑，也不要覆盖已有迁移记录。旧版数据库如尚未执行旧 V3，应由部署方保留原迁移并执行一次，先归一化历史 36 位 UUID，再将列收窄为 `CHAR(32)`；执行前核对历史值和迁移状态。

在 `.env` 设置 `MYSQL_ENABLED=true` 并填写 `DB_HOST`、`DB_PORT`、`DB_USERNAME`、`DB_PASSWORD`、`DB_DATABASE`。启用访问日志还需设置 `HTTP_LOG_ENABLED=true`。连接池最多 10 条连接；启用的依赖启动检查失败会终止启动。

---

## ♻️ Redis 与限流

设置 `REDIS_ENABLED=true` 并配置 `REDIS_*` 后启用 Redis。`THROTTLE_ENABLED=true` 要求同时启用 Redis：

- 用户业务接口共用 IP 计数，10 秒允许 30 次，第 31 次封禁 10 分钟。
- API 前缀下的未知业务路由在 30 秒内出现 5 次，后续请求封禁 24 小时；已有接口返回的业务 404 不计入。
- 后台、Webhook、健康接口跳过用户频率计数；文档和探针不参与限流。未知路由封禁期间，API 健康接口也会被拦截，独立存活 / 就绪探针仍可用。
- Lua 原子操作、带 TTL 的独立封禁标记，429 返回 `Retry-After`。运行中 Redis 失败时告警并放行；启动连接失败时拒绝启动。

### 公共 Cache

```go
cache, err := infrastructure.NewCache(resources.Redis, c.RedisKeyPrefix)
```

校验前缀并提供可注入的公共缓存方法：`Set(ctx, key, value, ttl)`、`Get(ctx, key)`、`Delete(ctx, key)`、`SetIfAbsent(ctx, key, value, ttl)`。实际键统一为 `<前缀>:cache:<业务键>`，不同服务操作同名业务键不会互相覆盖。

- TTL 使用 `time.Duration`，普通写入允许 0 表示永久，NX 写入要求正 TTL。
- 获取结果区分不存在和空字符串。
- Redis 禁用或故障时显式返回错误，NX 不会把连接失败当成获取成功。
- 将 NX 用作业务互斥时，应由业务侧补充唯一持有者令牌、续期和比较删除逻辑。

### 升级提示

已有派生服务升级时，先确定 `APP_NAME` 与 `REDIS_KEY_PREFIX`，需要保留当前封禁 / 计数状态时继续显式设置原 `THROTTLE_KEY_PREFIX`，再部署并验证隔离。Cache 新增前缀后旧的裸键不会再命中，需按业务需要预热；模板不会迁移或删除旧键。单节点 Redis 配置开箱可用；集群连接拓扑需要额外配置。

---

## 🌐 外部 HTTP 请求

统一通过 `internal/httpclient` 创建 Resty **v2.17.2** 客户端：`httpclient.New(options)` 是工厂，`httpclient.Registry` 持有本进程创建的全部客户端。默认超时 10 秒、响应体上限 2 MiB，复用连接池；不自动重试、不跟随重定向、不持久化 Cookie、不输出报文调试，TLS 证书校验保持开启。

```go
response, err := client.R().
    SetContext(ctx).
    SetHeader("Authorization", "Bearer "+token).
    Get("/v1/example")
if err != nil {
    return gerror.New("上游请求失败")
}
if !response.IsSuccess() {
    return gerror.New("上游响应状态异常")
}
```

客户端统一由应用持有的 `httpclient.Registry` 创建与关闭，业务代码不自己创建，也不自己关闭：

```go
// 装配阶段：Registry 由 App 持有，Close 之后再创建会返回错误。
client, err := app.HTTPClients().New(httpclient.Options{BaseURL: endpoint, Timeout: 3 * time.Second})
if err != nil {
    return err
}
upstream := NewUpstream(client)
```

`App.Close` 按 `Server.Shutdown` → 访问日志消费协程 → 客户端空闲连接 → 外部连接（MySQL / Redis）的顺序释放资源。每个上游独立创建并复用客户端，认证头、结果对象和请求参数放在单次 Request 上。

禁止在业务包或实际请求测试中直接使用标准库 HTTP 客户端、GoFrame gclient 或自行 new Resty。`net/http` 仍用于服务端、状态码和客户端底层 Transport，入站单测使用 `httptest.NewRequest`。

---

## 🚢 部署

生产设置 `APP_ENV=production`；文档强制关闭，健康业务接口只返回存活状态。非生产健康接口额外提供运行时长和 Go 堆内存诊断。`/readyz` 并行探测启用的依赖，不可用时返回 503；`/livez` 独立反映进程存活。

生产跨域来源通过 `CORS_ORIGINS` 明确配置，留空表示不提供跨域许可。模板不处理 HTTPS 证书，生产通常由入口代理终止 TLS。

首次部署顺序：

1. 按需准备数据库和 Redis → 需要访问日志时在新库执行 V2。
2. 注入配置 → 构建启动。
3. 检查 `livez`、`readyz` 和健康接口。
4. 先验证新版本再切流；回退时恢复上一版二进制和配置，保留已写入的数据库表。

```powershell
./hack/check.ps1
./bin/backend-template-go.exe
```

构建 Linux 二进制：

```powershell
$env:GOOS="linux"
$env:GOARCH="amd64"
$env:CGO_ENABLED="0"
go build -trimpath -o bin/backend-template-go .
Remove-Item Env:GOOS,Env:GOARCH,Env:CGO_ENABLED
```

部署时同时提供 `manifest/config/config.yaml`，给运行用户 `logs` 写权限；通过运行环境或未纳入版本管理的 `.env` 提供凭据。也可 `docker build -t backend-template-go .`，运行镜像安装 CA 证书与时区数据、以普通用户 `app` 启动，并默认 `APP_ENV=production`。

反向代理需同时转发 API 前缀下的 `/assets/`、`/knife4j-next-*.svg`、`/oauth2-redirect.html` 与 `/vite.svg`，这些路径保留给内嵌文档。

---

## ✅ 验证

先安装固定版本工具：Windows 使用 `./hack/install-tools.ps1`，Linux / macOS 使用 `sh ./hack/install-tools.sh`。

```powershell
# Windows
./hack/install-tools.ps1
./hack/check.ps1
```

```sh
# Linux / macOS
sh ./hack/install-tools.sh
sh ./hack/check.sh
```

安装脚本把 goimports、Revive 和 gf 安装到 `bin/tools`，并把当前仓库的 `core.hooksPath` 指向 `.githooks`。脚本自身输出固定为英文 ASCII：仓库源码是 UTF-8 无 BOM，而非 ASCII 文案在 Windows PowerShell 5.1 下会按 ANSI 解码，导致脚本语法被破坏。

检查入口依次执行 goimports、Revive、模板约定检查、`go vet ./...`、`go test ./...`、`go build ./...`。默认测试无需外部服务，覆盖配置、可信代理边界、真实 HTTP 路由、追踪头剥离、参数校验、异常脱敏、Knife4x 前缀与内嵌资源、文档认证、限流排除，以及示例四层纵切的事务一致性读、分页契约、出站追踪头、缓存命中与依赖降级。

提交前会自动调用统一检查入口，不自动改写或暂存源码；提交标题遵循 Conventional Commits，建议单行中文，例如 `fix(http): 修复错误响应状态码`。

需要 Docker 的专用集成测试：

```powershell
docker compose -f manifest/docker/compose.test.yaml up -d --wait
$env:INTEGRATION_TEST="true"
go test ./internal/server -run TestMySQLRedisIntegration -count=1 -v
Remove-Item Env:INTEGRATION_TEST
docker compose -f manifest/docker/compose.test.yaml down
```

专用测试只使用本机 13306 / 16379 端口、`backend_template_test` 数据库和 Redis DB 15，不读取业务连接配置。

### 生成数据库模型

`hack/config.yaml` 维护生成规则，不含数据库凭据。先在独立开发库执行迁移，将生成配置复制到未提交的 `hack/config.local.yaml`，设置 `gfcli.gen.dao[0].link`，再执行：

```powershell
./bin/tools/gf.exe gen dao '--gf.gcfg.file=hack/config.local.yaml'
```

> 不要手工编辑生成文件，也不要把业务生产库用于生成。

---

## 🔗 与 Nest 模板的能力对应

| Nest 模板能力 | GoFrame 实现 |
| --- | --- |
| Controller、DTO、验证管道 | 标准路由 DTO、标量类型检查和 GoFrame 校验标签 |
| TypeORM / MySQL | GoFrame ORM、生成的 DAO / DO / Entity、注入式 Repository 与顺序 SQL 迁移 |
| TypeORM 事务 | 注入 `Transactor` 拥有事务边界，Repository 复用 ctx 中的事务 |
| Redis 服务与限流存储 | gredis、可注入 Cache、Lua 原子计数与封禁 |
| Knife4j 在线文档 | GoFrame OpenAPI + Knife4x Go 内嵌在线调试 |
| 异常过滤器、统一响应 | HTTP / GoFrame 错误码映射、响应包装、异常脱敏 |
| 请求追踪与访问日志 | 服务端生成追踪编号、拒绝入站 trace 头、有界异步日志队列 |
| Helmet / CORS | CSP 等安全响应头、生产来源白名单、可信代理 CIDR |
| 健康诊断 | 非生产运行 / 内存信息、独立存活及就绪探针 |
| ESLint / 构建检查 / Git Hooks | goimports、Revive、模板约定、go vet、go test、构建及 Git Hooks |

---

## 📚 参考

- [GoFrame 官方文档](https://goframe.org/docs)
- [项目目录设计](https://goframe.org/docs/design/project-structure)
- [规范路由](https://goframe.org/docs/web/router-registering-strict-router-usage)
- [Resty v2 文档](https://pkg.go.dev/github.com/go-resty/resty/v2)
- [Knife4x Go 接入说明](https://github.com/songxychn/knife4j-next/blob/knife4x/go/v0.9.2/knife4x/go/README.md)
