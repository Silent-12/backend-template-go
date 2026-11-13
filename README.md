# GoFrame 后端模板

基础能力对齐同系列的 Nest 基础模板，使用 GoFrame **v2.10.4** 实现。最低 Go 1.23，建议使用受支持的较新 Go 版本；模板在 Go 1.27.3 / Windows 上验证。

提供可选 MySQL / Redis、Resty 外部 HTTP 客户端、统一响应和错误处理、参数校验、可在线调试的 OpenAPI 文档、健康探针、日志轮转、异步数据库访问日志、可信代理 IP 解析、CORS 和 Redis 原子限流。默认关闭 MySQL、Redis 和限流，可直接启动。

本仓库用于派生多个独立后端服务，代码规范以根目录 `AGENTS.md` 为准。新增业务前先替换 `go.mod` 模块名和内部导入，设置各自的 `APP_NAME`、API 前缀和数据库。`APP_NAME` 同时用于运行时服务名、文档标题及默认 Redis 命名空间；同一服务的多副本使用相同名称，不同服务必须使用不同名称。派生时不复制 `.git`、`bin`、`logs`、`.codegraph`、`.env` 或本地生成配置，更不要复制其他服务的凭据与测试数据。

## 快速启动

在项目根目录使用 PowerShell：

```powershell
Copy-Item .env.example .env
go mod download
go run .
```

- 健康接口：`http://localhost:3000/backend/v1/health`
- 接口文档：`http://localhost:3000/backend/doc.html`
- OpenAPI JSON：`http://localhost:3000/backend/api-json`
- 存活 / 就绪探针：`/backend/livez`、`/backend/readyz`

文档使用 Swagger UI，可查看模型和在线调试；界面脚本需要访问 CDN，JSON 可下载后导入离线 API 工具。页面关闭外部校验服务和授权信息持久化。文档只包含当前启用的标准业务接口。项目没有内置账户、JWT 或 RBAC；业务接口正式对外开放前应接入业务认证与权限控制。

## 配置

配置优先级为：操作系统环境变量 > `.env` > 内置默认值。`.env` 就是当前服务的本地配置文件，只从**当前工作目录**读取，运行二进制前请进入项目 / 部署目录。文件值只合并为当前配置快照，不写入进程环境，重复加载会重新读取文件；变量插值沿用 godotenv 的单文件解析规则。布尔值严格使用 `true` / `false`。启动时会校验服务名、Redis 前缀、端口、代理 CIDR、依赖开关关系和文档凭据。

已有部署升级时，将原 `.env.local` 中仍需使用的配置合并到 `.env`，重名键保留原本生效的值，再启动新版本；应用不再读取 `.env.local`。不修改已注入的进程环境，不自动搬迁或删除凭据文件；回退时使用原版本及原配置文件。默认配置测试覆盖 `.env` 优先级、重复读取、文件缺失及旧文件不参与加载。

密码类配置保留原始值，不删除首尾空格；在环境文件中需要空格时请用引号包裹密码。`CORS_ORIGINS` 每项必须是完整 HTTP/HTTPS 来源（协议、主机和可选端口），不能包含路径、查询、凭据或主机通配符。`TRUSTED_PROXY_CIDRS` 只接受具体网段，`0.0.0.0/0` 与 `::/0` 会被拒绝，否则任何客户端都能伪造 `X-Forwarded-For`。

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

## 目录与扩展方式

```text
main.go                     进程入口，调用 cmd.Main
Dockerfile                  多阶段构建，运行镜像以普通用户、生产模式启动
.dockerignore               构建上下文排除项，避免带入 .env、bin、logs
.editorconfig               编码、换行与缩进约定（Go 使用制表符）
AGENTS.md                   仓库规范与交付检查约定
api/                        请求 / 响应 DTO 与路由元数据
internal/
  cmd/                      命令入口与停止信号处理
  config/                   配置加载、默认值和校验
  controller/               HTTP 适配；不直接访问数据库
  service/                  业务服务与仓储接口，通过构造函数注入依赖
  repository/               手写持久化实现，组合连接和生成的表 / 列元数据
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
hack/install-tools.ps1      安装固定版本的 goimports、Revive 和 gf，并启用 Git Hooks（Windows）
hack/install-tools.sh       同上（Linux / macOS）
hack/check.ps1              Windows 排版、GoDoc、模板约定、测试和构建检查
hack/check.sh               跨平台检查入口
hack/quality/               Resty / 分层 / 常见 ORM 写入约定检查
.githooks/pre-commit        提交前调用统一检查入口
.githooks/commit-msg        校验单行中文 Conventional Commit 标题
```

新增业务模块按 `api → internal/controller → internal/service → internal/repository` 分层，并在 `internal/server/server.go` 注册。通过构造函数显式注入依赖，不强制生成接口 / 注册器。新增后台接口按 `/backend/v1/admin/...` 和 `admin` 目录划分，用户端按 `/backend/v1/user/...` 划分，复用的领域逻辑放 `shared`。

生成 DAO / DO / Entity 与手写 Repository 分开管理。运行时连接通过 `infrastructure.Resources` 注入，Repository 使用生成的表 / 列元数据和 DO，避免通过生成 DAO 的默认 `g.DB()` 入口引入第二套全局配置。数据写入不使用 map；创建 / 更新时间由 ORM 自动维护，并通过 `SoftTimeTypeTimestampMilli` 保持既有毫秒单位。

### 跨仓储事务

事务边界由 Service 拥有，而不是仓储。由装配层向业务服务注入 `*infrastructure.Transactor`，仓储通过 `infrastructure.DBFromContext` 复用 ctx 中的事务连接，**不自行开启事务**。

```go
// 事务入口由装配层注入；ctx 含事务时直接加入，不含时开启新事务。
err := tx.Transaction(ctx, func(ctx context.Context) error {
    if err := repoA.Update(ctx, itemNo, patch); err != nil {
        return err
    }
    return repoB.Create(ctx, log)
})
```

需要跨多个仓储原子写入时，在 Service 中调用 `tx.Transaction`，把回调内的 ctx 继续传给各仓储方法；不要在仓储内再调用 `Transaction`，也不要在长事务中发起外部 HTTP 或 Redis 调用。回调返回错误或 panic 时事务回滚，panic 恢复后继续向上抛出。

开发工具首次安装（Windows）：

```powershell
./hack/install-tools.ps1
```

`hack/config.yaml` 维护生成规则，不含数据库凭据。生成新业务模型时先在独立开发库执行迁移，将生成配置复制到未提交的 `hack/config.local.yaml`，在本地文件中设置 `gfcli.gen.dao[0].link`，再执行：

```powershell
./bin/tools/gf.exe gen dao '--gf.gcfg.file=hack/config.local.yaml'
```

不要手工编辑生成文件。当前访问日志表的模型在无 MySQL 实例的环境下，使用按迁移字段构造的临时 SQLite 元数据经 GoFrame CLI v2.10.4 生成；后续以独立 MySQL 开发库重新生成并验证。运行时仍使用 MySQL；SQLite 只用于离线生成及 ORM 时间精度回归测试。

## 外部 HTTP 请求

统一通过 `internal/httpclient` 创建 Resty **v2.17.2** 客户端：`httpclient.New(options)` 是工厂，`httpclient.Registry` 持有本进程创建的全部客户端。默认超时 10 秒、响应体上限 2 MiB，复用连接池；不自动重试、不跟随重定向、不持久化 Cookie、不输出报文调试，TLS 证书校验保持开启。每个上游可通过 `httpclient.Options` 设置 BaseURL、Timeout 和 ResponseBodyLimit。

业务方法使用已注入的客户端，每次新建 Request 并绑定调用方 Context：

```go
response, err := client.R().
    SetContext(ctx).
    SetHeader("Authorization", "Bearer "+token).
    Get("/v1/example")
if err != nil {
    // 由上游适配组件转换为安全的领域错误；不要直接记录含查询参数的 URL 错误。
    return gerror.New("上游请求失败")
}
if !response.IsSuccess() {
    return gerror.New("上游响应状态异常")
}
```

工厂在最终请求头合并后清理手动设置或残留的追踪头，再从当前 Context 注入追踪编号和 W3C Trace Context；不要覆盖工厂的 `SetPreRequestHook`，扩展请求处理使用 `OnBeforeRequest`。认证、参数及结果对象必须在单次 Request 上设置；非 2xx 不代表 Resty 一定返回 err，需要同时检查状态。仅在明确幂等、可重放时为特定上游开启有限重试。

禁止在业务包或实际请求测试中直接使用标准库 HTTP 客户端、GoFrame gclient 或自行 new Resty。`net/http` 仍用于服务端、状态码和客户端底层 Transport，入站单测使用 `httptest.NewRequest`。

### 上游 HTTP 客户端生命周期

客户端统一由应用持有的 `httpclient.Registry` 创建与关闭，业务代码不自己创建，也不自己关闭：

```go
// 装配阶段：Registry 由 App 持有，Close 之后再创建会返回错误。
client, err := app.HTTPClients().New(httpclient.Options{BaseURL: endpoint, Timeout: 3 * time.Second})
if err != nil {
    return err
}
upstream := NewUpstream(client)
```

`App.Close` 按 `Server.Shutdown` → 访问日志消费协程 → 客户端空闲连接 → 外部连接（MySQL / Redis）的顺序释放资源，因此不需要在业务代码里调用 `CloseIdleConnections`。流式读取仍由业务侧限制大小并关闭响应 Body；派生服务如需自定义客户端，应复用同一 Registry，避免绕过统一超时与证书策略。

## 请求追踪与访问日志

### 追踪信任边界

服务端**不接受**入站 `traceparent` / `tracestate`：请求进入时先删除这两个头（同时删除可覆盖路由的框架内部头），再由服务端生成追踪编号。响应体 `traceId`、响应头 `X-Trace-Id`、GoFrame 框架 `Trace-Id` 和 `access_logs.trace_id` 必须同源，日志中的 `traceId` 也取自同一上下文。

出站请求方向相反：`httpclient` 的 `SetPreRequestHook` 会用当前 Context 重建 `X-Trace-Id` 与 W3C `traceparent` / `tracestate`，因此上游看到的是受控的服务端链路信息，入站客户端无法伪造本服务的追踪编号或跨链路传播任意 trace 上下文。

默认使用 TCP 对端 IP，**不直接信任代理头**。位于反向代理后时，配置 `TRUSTED_PROXY_CIDRS`（逗号分隔），并让入口代理覆盖或正确追加 `X-Forwarded-For`。日志与限流使用相同的解析结果。

### 访问日志与保留期

运行日志由 `manifest/config/config.yaml` 配置，写入 `logs` 并输出控制台，按日期命名、100 MB 轮转；闲置超过 24 小时的旧日志也会进入轮转，备份最多 10 份、最长保留 7 天，按框架定时检查清理。`HTTP_LOG_ENABLED=true` 要求 MySQL 已开启，并写入 `access_logs`；仅记录路由模板、方法、状态、耗时、IP 和追踪号。队列容量 256，满时丢弃并告警（同类告警按时间窗口合并），停止时最多等待 5 秒处理日志。

`access_logs` 的保留期与清理策略由部署侧负责，模板不提供定时清理或分区任务；高频服务应在库级别制定归档 / 删除计划，并关注 `trace_id` 列的索引体积。

## MySQL 和访问日志

新库需要访问日志时，执行一次 [V2__create_access_logs.sql](manifest/sql/V2__create_access_logs.sql)，创建的表中追踪编号为 CHAR(32)。脚本不带 IF NOT EXISTS；应用不自动建表或执行迁移。

该 SQL 是新的初始化基线，不适用于已经执行旧 V2 的数据库：不要在已有表上重跑，也不要覆盖已有迁移记录。旧版数据库如尚未执行旧 V3，应由部署方保留原迁移并执行一次，先归一化历史 36 位 UUID，再将列收窄为 CHAR(32)；执行前核对历史值和迁移状态。已执行旧 V3 的数据库无需再次变更。已有 template_items 表不会被本次变更删除，是否清理由所属服务确认。

在 .env 设置 MYSQL_ENABLED=true 并填写 DB_HOST、DB_PORT、DB_USERNAME、DB_PASSWORD、DB_DATABASE。启用访问日志还需设置 HTTP_LOG_ENABLED=true。启用 MySQL 时必须显式提供 DB_DATABASE；应用使用 GoFrame ORM，连接池最多 10 条连接；启用的依赖启动检查失败会终止启动。

标量参数在 GoFrame 转换前检查类型，路径中的资源编号不允许被同名 query / body 参数覆盖；超出 2 MiB 的请求体返回 413。

失败使用实际 HTTP 状态码，`code` 同步；请求校验为 400，资源不存在为 404，限流为 429，未知错误为 500。GoFrame 的认证、权限、资源不存在和服务繁忙错误码也会映射到对应 HTTP 状态。显式状态码不会被改回 200；通用异常日志仅记录追踪号、错误码和错误类型，避免原始 SQL / URL / panic 文本携带凭据或业务数据。详细诊断由所属组件记录经过脱敏、字段受控的上下文。下载和健康探针不套业务响应包装；流或下载尚未提交时的异常同样会脱敏。

## Redis 与限流

设置 `REDIS_ENABLED=true` 并配置 `REDIS_*` 后启用 Redis。`THROTTLE_ENABLED=true` 要求同时启用 Redis：

- 用户业务接口共用 IP 计数，10 秒允许 30 次，第 31 次封禁 10 分钟。
- API 前缀下的未知业务路由在 30 秒内出现 5 次，后续请求封禁 24 小时；已有接口返回的业务 404 不计入。
- 后台、Webhook、健康接口跳过用户频率计数；文档和探针不参与限流。未知路由封禁期间，API 健康接口也会被拦截，独立存活 / 就绪探针仍可用。
- Lua 原子操作、带 TTL 的独立封禁标记，429 返回 `Retry-After`。运行中 Redis 失败时告警（按时间窗口合并）并放行；启动连接失败时拒绝启动。
- `REDIS_KEY_PREFIX` 默认继承 `APP_NAME`，限流的 `THROTTLE_KEY_PREFIX` 默认继承 `REDIS_KEY_PREFIX`；显式设置旧限流前缀仍然优先。前缀为 1～128 位字母、数字、冒号、下划线或连字符，以字母或数字开头，禁止花括号干扰 Cluster hash tag。Redis key 的 IP 部分使用摘要。单节点 Redis 配置开箱可用；集群连接拓扑需要额外配置。

`cache, err := infrastructure.NewCache(resources.Redis, c.RedisKeyPrefix)` 校验前缀并提供可注入的公共缓存方法：`Set(ctx, key, value, ttl)`、`Get(ctx, key)`、`Delete(ctx, key)`、`SetIfAbsent(ctx, key, value, ttl)`。实际键统一为 `<前缀>:cache:<业务键>`，不同服务操作同名业务键不会互相覆盖。调用方处理构造错误后注入业务服务。TTL 使用 `time.Duration`，普通写入允许 0 表示永久，NX 写入要求正 TTL；获取结果区分不存在和空字符串。Redis 禁用或故障时显式返回错误，NX 不会把连接失败当成获取成功。将 NX 用作业务互斥时，应由业务侧补充唯一持有者令牌、续期和比较删除逻辑。

已有派生服务升级时，先确定 `APP_NAME` 与 `REDIS_KEY_PREFIX`，需要保留当前封禁 / 计数状态时继续显式设置原 `THROTTLE_KEY_PREFIX`，再部署并验证隔离。Cache 新增前缀后旧的裸键不会再命中，需按业务需要预热，旧键仅按原 TTL 到期或由业务核对后清理，模板不会迁移或删除旧键；回退使用原二进制和配置。已通过配置继承、缓存跨服务读写 / NX / 删除隔离及限流单元测试验证，实际共享 Redis 部署仍需实连验证。

文件上传由业务侧按实际需求自行实现，统一上传到 OSS。基础模板不提供本地文件存储目录、相对或绝对路径上传、静态文件托管或 OSS 上传接口。

## 部署与配置变更

生产设置 `APP_ENV=production`；文档强制关闭，健康业务接口只返回存活状态。非生产健康接口额外提供运行时长和 Go 堆内存诊断。`/readyz` 并行探测启用的依赖，不可用时返回 503，某个依赖超时不会连带误报其他依赖；`/livez` 独立反映进程存活。

生产跨域来源通过 `CORS_ORIGINS` 明确配置，逗号分隔，留空表示不提供跨域许可；开发环境留空时允许来源。模板不处理 HTTPS 证书，生产通常由入口代理终止 TLS。开发 / 测试文档可设置 `DOCS_AUTH_ENABLED=true` 和非空的 `DOCS_USERNAME` / `DOCS_PASSWORD`，同时保护 HTML 与 JSON。

首次部署顺序为：按需准备数据库和 Redis → 需要访问日志时在新库执行 V2 → 注入配置 → 构建启动 → 检查 `livez`、`readyz` 和健康接口。数据库 / 日志开关默认关闭，只有显式启用后才会产生连接和数据写入。先验证新版本再切流；回退时恢复上一版二进制和配置，保留已写入的数据库表。应用不会自动执行破坏性回滚。

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

部署时同时提供 `manifest/config/config.yaml`，给运行用户 `logs` 写权限；通过运行环境或未纳入版本管理的 `.env` 提供凭据。也可 `docker build -t backend-template-go .`，`Dockerfile` 使用多阶段构建，运行镜像安装 CA 证书与时区数据、以普通用户 `app` 启动，并默认 `APP_ENV=production`，因此部署时仍需显式提供 `APP_NAME`、`DB_DATABASE` 等生产配置。

## 验证

先安装固定版本工具：Windows 使用 `hack/install-tools.ps1`，Linux / macOS 使用 `hack/install-tools.sh`。

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

安装脚本把 goimports、Revive 和 gf 安装到 `bin/tools`，并把当前仓库的 `core.hooksPath` 指向 `.githooks`。脚本自身的输出固定为英文 ASCII：仓库源码是 UTF-8 无 BOM，而非 ASCII 文案在 Windows PowerShell 5.1 下会按 ANSI 解码，导致引号与括号配对错乱、脚本语法被破坏；因此脚本注释、提示与错误信息只用 ASCII，仓库内的 Go 与 Markdown 文件仍使用中文。

检查入口固定版本工具，校验 goimports 导入分组、Revive 的命名 / GoDoc / Context / 错误规范，以及常见 Resty 绕过、旧 logic 分层和 map ORM 写入。Resty 约定检查覆盖根入口、业务包、工具及测试，识别默认导入和别名，禁止点导入。ORM 检查仅针对当前文件可识别来源的调用，不把 gvalid 或 SDK 的 `Data(map)` 当成数据库写入；跨文件封装仍需审查 DO 使用情况。随后执行：

```powershell
go vet ./...
go test ./...
go build ./...
```

默认测试无需外部服务，覆盖配置、可信代理边界、真实 HTTP 路由、入站追踪头剥离、参数校验、异常脱敏、文档认证和生产文档关闭。SQLite 测试验证访问日志写入和 ORM 毫秒时间，不代表 MySQL 实连验证。

Resty 测试使用本地 HTTP / TLS 模拟上游，覆盖取消与超时、响应大小、写操作不重试、重定向限制、追踪透传、证书校验和 Cookie 隔离，不请求真实第三方服务。

回归测试还覆盖显式错误状态、流 / 下载初始化异常、请求体大小、CORS 和追踪一致性。Redis 公共缓存、Lua 并发限流与封禁 TTL 使用进程内 Redis 协议模拟服务验证，无需 Docker；它不能替代实际 Redis 服务的兼容性验证。

需要 Docker 的专用集成测试：

```powershell
docker compose -f manifest/docker/compose.test.yaml up -d --wait
$env:INTEGRATION_TEST="true"
go test ./internal/server -run TestMySQLRedisIntegration -count=1 -v
Remove-Item Env:INTEGRATION_TEST
docker compose -f manifest/docker/compose.test.yaml down
```

专用测试只使用本机 13306 / 16379 端口、`backend_template_test` 数据库和 Redis DB 15，不读取业务连接配置；SQL 在新容器首次初始化时执行。测试容器使用公开的本地示例密码，端口仅绑定环回地址，不用于生产。

交付检查统一由 `./hack/check.ps1`（Windows）或 `sh ./hack/check.sh`（Linux / macOS）执行 goimports、Revive、模板约定、`go vet`、默认测试和构建。修复批次内已确认导入分组、`api` 包编译与 `go vet` 通过；全仓默认测试与构建结果以维护者执行该入口的输出为准。当前 Docker 引擎未运行，MySQL / Redis 实连集成测试及 Docker 镜像构建尚未执行。

## 与 Nest 模板的能力对应

| Nest 模板能力 | GoFrame 实现 |
| --- | --- |
| Controller、DTO、验证管道 | 标准路由 DTO、标量类型检查和 GoFrame 校验标签 |
| TypeORM / MySQL | GoFrame ORM、生成的 DAO / DO / Entity、注入式 Repository 与顺序 SQL 迁移 |
| TypeORM 事务 | 注入 `Transactor` 拥有事务边界，Repository 复用 ctx 中的事务 |
| Redis 服务与限流存储 | gredis、可注入 Cache、Lua 原子计数与封禁 |
| Knife4j 在线文档 | GoFrame OpenAPI + Swagger UI 在线调试 |
| 异常过滤器、统一响应 | HTTP / GoFrame 错误码映射、响应包装、异常脱敏 |
| 请求追踪与访问日志 | 服务端生成追踪编号、拒绝入站 trace 头、有界异步日志队列 |
| Helmet / CORS | CSP 等安全响应头、生产来源白名单、可信代理 CIDR |
| 健康诊断 | 非生产运行 / 内存信息、独立存活及就绪探针 |
| ESLint / 构建检查 / Git Hooks | goimports、Revive、模板约定、go vet、go test、构建及 Git Hooks |

新克隆项目后，Windows 执行一次安装脚本即可安装开发工具并启用 Git Hooks：

```powershell
./hack/install-tools.ps1
```

脚本仅设置当前仓库的 `core.hooksPath=.githooks`，不修改全局 Git 配置；可重复执行。新克隆或派生的仓库需各自执行一次。工具安装失败时脚本中止，不继续配置 Hooks。

Linux/macOS 使用 `sh ./hack/install-tools.sh`；已手动安装工具时也可只执行 `git config --local core.hooksPath .githooks`，首次启用前还需运行 `chmod +x .githooks/pre-commit .githooks/commit-msg`。

提交前调用统一检查入口，不自动改写或暂存源码；提交标题遵循 Conventional Commits，建议单行中文，例如 `fix(http): 修复错误响应状态码`。本地存储按项目约定移除，文件统一由业务侧接入 OSS；不会照搬 Nest 的本地静态文件路径。

## 参考

- [GoFrame 官方文档](https://goframe.org/docs)
- [项目目录设计](https://goframe.org/docs/design/project-structure)
- [规范路由](https://goframe.org/docs/web/router-registering-strict-router-usage)
- [Resty v2 文档](https://pkg.go.dev/github.com/go-resty/resty/v2)
