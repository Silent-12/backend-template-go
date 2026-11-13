# 定位、规则加载与优先级

- 本仓库是供多个独立后端服务复制使用的 GoFrame v2 模板。只沉淀已证明可复用的基础能力；业务接口、上游地址、凭据、租户策略和领域规则由派生服务自行实现，禁止写入特定项目的默认值。
- 修改前检查根目录 `.agents`，仅加载与任务相关的 rules / skills。GoFrame 工作使用 `goframe-v2` 技能；用户明确要求优先于技能建议。已获授权的常规修改直接完成，不因一般实现选择重复请求确认。
- 本文件使用 Go 规范，不照搬 Nest 的 TypeScript、装饰器和 JSDoc 写法。嵌套规则只在其作用范围内生效；无法安全判断的真实冲突需说明来源。
- 先理解调用链并复用已有实现，不创建未经需求证明的抽象、适配层或空模块。保留用户已有改动，不自动提交、推送或部署。

## 代码定位

- 存在 `.codegraph/` 时，理解或定位业务代码先使用 `codegraph explore`，再阅读实际源码；按需使用 query、callers、callees、impact 等命令。
- `rg` 用于精确 / 局部匹配，或 CodeGraph 不可用时兜底；无索引时不自行执行 `codegraph init`。

# 技术栈与分层

- 基础栈为 GoFrame v2、MySQL、Redis、Resty v2；使用 Go Modules。依赖固定在 `go.mod` / `go.sum`，开发工具固定在 `hack/install-tools.ps1` 和 `hack/check.sh`。不得为了工具随意提高服务最低 Go 版本。
- 分层为 `api → internal/controller → internal/service → internal/repository`。业务逻辑直接实现于 `service`，不新建 `logic` 或通过 init 注册的全局业务单例。
- `api/<module>/v1` 定义传输协议和 OpenAPI 元数据；Controller 只做协议适配；Service 维护业务规则和事务边界；Repository 使用注入的数据库连接完成持久化。
- 事务边界由 Service 通过注入的 `Transactor` 拥有，Repository 只复用 ctx 中的事务，不自行开启事务。
- `internal/dao`、`internal/model/do`、`internal/model/entity` 由 `gf gen dao` 维护。禁止手工伪造生成文件或修改 `DO NOT EDIT` 内容；需要扩展时在 Repository 中组合生成的表 / 列元数据和注入连接。
- 数据库连接由 `infrastructure.Resources` 统一创建、注入和关闭。当前生成 DAO 的默认 `g.DB()` 单例入口未作为运行时连接来源，业务应使用 Repository，避免引入另一份数据库配置。
- 接口定义放在使用方所在包，保持最小方法集合；依赖通过构造函数注入。不为每个 struct 机械生成接口，也不引入服务定位器。
- 后台与用户端分别使用模块下的 `admin` / `user` 目录和路由前缀，跨端领域复用放 `shared`；尚无明确使用方的基础示例可保持模块根目录。
- 公共类型放所属能力包或 `internal/model`，常量优先放使用它的包；禁止堆积无明确边界的 common / util 大包。

# 排版、命名与注释

- 源码使用 UTF-8 无 BOM；修改前检查编码，发现乱码或异常编码先报告并转换。Go 文件使用制表符缩进，由 gofmt / goimports 决定排版，不手工对齐空格。
- 导入按标准库、第三方库、当前 module 三组，使用 goimports 的 `-local <当前 module>` 固定分组；包路径使用小写，Go 文件使用 `snake_case.go`。包名简短、不加下划线或连字符。
- 导出标识符使用 MixedCaps；缩略词使用 `HTTP`、`ID`、`URL`、`JSON`、`SQL`、`TLS`、`IP` 等标准写法。生成文件遵循 GoFrame 生成器命名，不手工更改 `Id` 等字段。
- 每个手写包至少有一个 `// Package <包名> ...` 注释；导出类型、函数、方法、变量和常量必须有 GoDoc，并以标识符名称开头。使用完整简洁的中文说明目的、约束、错误和资源责任，不写 JSDoc 或机械的 `@param` / `@returns`。
- 测试函数和测试替身无需机械添加导出注释，以清晰用例名为主；隔离条件、并发前提和不明显的测试目的仍需说明。生成代码遵循生成器注释，排除手写 GoDoc 检查。
- API 方法注释中的业务动作与 `g.Meta` 的 `summary` 保持一致。DTO 的 `dc`、`v`、`d` 标签分别说明接口语义、校验和默认值，不把持久化 Entity 当作响应 DTO；响应 DTO 与请求 DTO 一样必须写 `dc`。
- 字段需要额外业务说明时，在字段上方使用单行 `//`；不为显而易见的赋值堆注释，不使用三行以上块注释描述单个字段。非导出复杂函数应说明关键算法、并发条件或协议限制。
- 函数之间留空行，按职责组织代码；避免把构造、声明、分支和多步逻辑压成一行。三个及以上相关变量声明用 `var (...)` 分组；不遮蔽 min、max、len、error 等内置标识符。
- 函数保持单一职责，非法输入和失败状态提前返回。拆分过长编排函数时按安全头、跨域、认证、路由等真实职责拆分，避免无意义的薄封装。

# Context、错误和日志

- 请求链首参使用 `ctx context.Context`，向数据库、Redis、Resty 和下游方法传递同一请求上下文；不在业务调用中用 Background / TODO 切断取消或追踪，不将 Context 长期存入业务 struct。
- Background 仅用于进程初始化、显式脱离请求的后台任务或有独立超时的关闭阶段。后台任务必须有启动、取消和等待退出的责任方，不使用无界 goroutine 或队列。
- 应用新建 / 包装错误使用 `gerror` 和明确 `gcode`，通过 `errors.Is` / `errors.As` 判断错误；不得使用字符串匹配识别错误。处理错误后不要继续执行依赖该结果的流程。
- 详细异常由产生异常的组件或集中异常中间件记录，避免层层重复打印。日志使用 GoFrame Logger；业务代码禁止 fmt.Print / log.Print，CLI 和一次性工具可按用途输出。
- 通用异常中间件只记录追踪号、错误码和错误类型，不格式化原始错误、Code Detail 或错误堆栈中的文本。需要详细诊断时由所属组件记录经过脱敏、字段受控的业务上下文，禁止直接将 Resty / SQL / panic 错误交给日志格式化。
- API 不得透传 SQL、堆栈、原始上游响应或底层错误文本；返回安全的状态码和提示。包装错误时保留原因，存在凭据泄露风险时先脱敏，禁止直接打印整个 Config。
- 禁止记录 Authorization、Cookie、密码、令牌、签名、完整 URL 查询以及请求 / 响应体。访问日志仅记录路由模板、状态、耗时、客户端 IP 和追踪号；日志大小、保留期及队列必须有界。
- 使用 GoFrame 上下文中的追踪编号，保持业务响应、`X-Trace-Id`、框架 `Trace-Id` 和日志一致；不直接信任客户端自填的追踪头或代理 IP 头。
- 不接受入站 `traceparent` / `tracestate`，追踪编号由服务端生成，响应体 `traceId`、`X-Trace-Id`、框架 `Trace-Id` 和 `access_logs.trace_id` 必须同源。

# 外部 HTTP 请求：统一 Resty

- 所有本项目代码主动发送的 HTTP 请求，包括测试中的实际请求，统一通过 `internal/httpclient.New` 创建 Resty 客户端。不得直接使用 `http.Client`、`http.DefaultClient`、`http.Get/Post`、GoFrame `g.Client/gclient` 或在业务包调用 `resty.New*`。
- `net/http` 可用于服务端、状态码、Handler、Header 和统一工厂中的 Transport；纯入站测试使用 `httptest.NewRequest`。第三方 SDK 内部实现不在此静态检查范围内，新增 SDK 时需核实其 HTTP 配置方式。
- 上游客户端由 `httpclient.Registry` 统一创建与关闭，业务不自行创建客户端，也不自行调用 `CloseIdleConnections`。
- 每个上游独立创建并复用客户端，启动后不并发修改其配置；每次通过 `R().SetContext(ctx)` 创建请求。认证头、结果对象、请求参数放在 Request 上，禁止用共享客户端保存用户态数据。
- 工厂的 `SetPreRequestHook` 负责在最终请求头合并后按当前 Context 重建追踪头，业务不得覆盖它；扩展请求处理使用 `OnBeforeRequest`。
- 默认开启证书校验，配置有限超时、响应体上限和连接池；关闭自动重试、重定向、Cookie 持久化和报文调试。不得用 `InsecureSkipVerify` 解决证书问题。
- 调用方必须同时处理 `err` 和 HTTP 状态；Resty 的非 2xx 响应不等于网络错误。需要特定状态语义时由上游适配组件转换为领域结果或安全错误。
- 仅在证明幂等且请求体可重放时按上游单独配置有限重试；支付、创建、发货等写请求不得默认重试。跨主机重定向和自动转发凭据必须有明确设计。
- 上游地址来自可信配置，不直接接收用户传入的完整 URL；防范 SSRF。流式读取由业务侧限定大小并关闭响应 Body，不把普通请求改成无界读取。

# API 契约与安全

- 全局前缀来自 `API_PREFIX`，默认为 `backend`；业务路由显式注册 `v1`，破坏性变更新建 `v2`。后台使用 `/v1/admin/...`，用户端使用 `/v1/user/...`。
- 标准业务接口声明 `g.Meta` 的 path、method、tags、summary，并使用独立 Req / Res。路径资源编号不能被同名 query / body 参数覆盖；校验原始类型、范围、长度和枚举。
- 业务 JSON 响应统一包含 `code`、`message`、`data`、`timestamp`、`traceId`，code 与实际 HTTP 状态一致；写操作成功返回 `data: null`。
- 页码分页使用 `{ list, pagination: { page, pageSize, total, totalPages } }`，空列表为 `[]`。限制页容量和查询范围；导出条件使用独立模型，不复用分页 DTO。
- 响应字段最小化；对外使用 UUID / 稳定业务编号，不暴露自增主键、内部状态、审计字段或敏感信息。删除对外接口 / 字段属于兼容性变更，须保留版本或明确处理调用方。
- 探针、文件、SSE、Webhook 和 204 / 304 遵循各自协议。尚未提交的流或下载发生错误时也必须脱敏，已提交响应不得再次写 JSON。
- 生产关闭接口文档，CORS 使用明确来源白名单；代理地址只信任配置的 CIDR。管理端接口仍需业务鉴权，路径前缀不能代替权限校验。
- 文件上传由业务侧自行实现，统一上传 OSS；模板不提供本地上传目录、相对 / 绝对路径写入或静态文件托管。

# 数据库、Redis 与配置

- 数据库结构变更只新增 `manifest/sql/V{序号}__{描述}.sql`，按顺序执行一次；已执行迁移不可修改或删除，纠正通过新迁移完成。禁止启动时自动同步表结构或执行破坏性回滚。
- 数据写入使用生成的 DO，禁止用 g.Map、gdb.Map 或临时 map 替代 DO。可选字段保持 nil，由 ORM 忽略；显式写零值时通过非 nil 的 DO 字段传入。
- 创建和更新时间由 ORM 维护。当前表保留 Unix 毫秒，使用 `SoftTimeTypeTimestampMilli`，禁止手工写入时间字段或关闭自动维护；引入 deleted_at 时先评估迁移和业务语义，再使用原生软删除。
- SQL 使用参数绑定和生成的列元数据；动态排序字段使用白名单。事务由 Service 决定业务边界、Repository 执行必要原子操作；外部 HTTP / Redis 调用不在数据库长事务内执行。
- 派生服务显式设置独立 `APP_NAME`，运行时名称和文档标题由它提供；启用 MySQL 必须显式设置 `DB_DATABASE`。不复制其他服务的数据库名、Redis key 前缀或幂等键。
- `REDIS_KEY_PREFIX` 默认继承 `APP_NAME`，`THROTTLE_KEY_PREFIX` 默认继承 Redis 前缀，旧的显式限流前缀继续优先。公共 Cache 必须通过 `NewCache(client, prefix)` 创建并处理构造错误，自动附加 `<前缀>:cache:`；业务只传场景内的键。命名空间禁止花括号，以免覆盖 Cluster hash tag；设置合理 TTL，锁 / 幂等写入失败不得当作获取成功。
- MySQL / Redis 开关必须与实际资源一致，启用后启动检查失败应中止监听。关闭顺序为停止请求和后台任务、处理队列、关闭外部连接。
- 新增配置同步默认值、`internal/config` 校验、`.env.example` 和 README；若本地 `.env` 存在，同步其键名但保留用户值。凭据原样读取，不 trim；不提交真实 `.env` 或含凭据的生成配置。
- 部署、Redis key 或配置变化的目的、范围、顺序、回退方式及验证结果写在已有 README 或用户指定记录中，不主动新增独立实现 / 变更 / 回滚说明文档。

# 模板派生与交付检查

- 复制为新服务时，先替换 go.mod 模块名和所有内部导入，再调整应用名称、API 前缀、数据库名、Redis key 命名空间及镜像名；检查示例模块是否保留。不复制 `.git`、`bin`、`logs`、`.codegraph`、`.env` 和本地生成配置，更不能带入其他服务的密钥或测试数据。
- `.env.example` 不得提供可直接使用的 `DB_DATABASE` 值，派生服务必须显式填写自己的库名。
- 生成工具只连接明确授权的独立开发 / 测试库；不为生成代码连接业务生产库。首次建库按全部迁移初始化后执行 `gf gen dao`，生成文件与手写代码分开检查。
- 先安装固定版本工具：Windows 使用 `./hack/install-tools.ps1`，安装成功后自动启用当前仓库 Git Hooks。完成 Go 修改后执行 `./hack/check.ps1` 或 `sh ./hack/check.sh`，检查 goimports、Revive、模板约定、go vet、go test 和构建。
- 生成文件排除手写 GoDoc / 导入风格检查，但必须参与编译和必要测试。检查脚本只报告问题，不自动修改用户代码或暂存区。
- 为外部调用、输入校验、事务、时间精度和并发行为保留有意义的回归测试，不为纯排版或可逆简单改动机械新增测试。并发改动按需运行竞态检查。
- Windows 如不能执行临时目录中的竞态程序，可使用 `go test -c -race -o bin/<包名>-race.test.exe ./internal/<包名>` 后运行该测试程序；不得绕过系统安全设置。
- 实连集成测试只操作专用测试实例；默认测试不得依赖生产数据库、Redis 或真实第三方服务。明确区分模拟验证、实际进程验证、实连验证和未验证项目。
- 安装脚本通过 `git config --local core.hooksPath .githooks` 启用 Git Hooks，只修改当前仓库配置，不修改全局 Git 配置。需要提交时使用单行中文 Conventional Commit 标题，例如 `refactor(template): 统一服务分层与外部请求规范`。
