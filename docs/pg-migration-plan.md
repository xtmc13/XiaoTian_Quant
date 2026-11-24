# 主库迁移 PostgreSQL 方案（SQLite → PG）

> 对标项目：QuantDinger（主库 PostgreSQL 18 + psycopg2，Redis 缓存/任务，Kafka 事件总线）。
> 状态：**B0 已落地并通过 `go build` / `go vet`；B1 迁移机制层已适配 PG；B1 基线 DDL、B2 repo SQL、B3 测试待 PG 实例环境验证**。本文是后续分批落地的依据；实施时每批以 `go build` / `go vet` / 目标包测试为闸门。
> 基线：当前工作区已有未提交的 A/P 档改动，本计划不回滚、不纳入本计划 diff。

## 实施进度（2026-11-24）

### 已完成（B0 + B1 机制层）
- 依赖：`go.mod` 引入 `github.com/jackc/pgx/v5 v5.13.0`（stdlib），移除 `modernc.org/sqlite` 及其传递依赖；`go mod tidy` 通过。
- 连接层：新增 `internal/store/db.go`——`resolveDSN()`（`DATABASE_URL` 优先，否则 `POSTGRES_HOST/PORT/USER/PASSWORD/DB` 拼装）、`openPostgres()`（pgx stdlib、连接池 `DB_POOL_MIN/MAX`、生命周期、启动 ping）、`RuntimeDir()/EnsureRuntimeDir()`（`DATA_DIR` 优先，兼容旧 `DB_PATH` 目录回退）。
- `store.go` `InitDB`：去掉 SQLite DSN/`_pragma`/WAL/文件目录创建；初始建表 DDL 移出（归 B1）；运行时目录与配置路径改用 `RuntimeDir()`。
- 目录推导：`agentfiles/plugin.go`、`metrics/metrics.go` 改读 `DATA_DIR`（`DB_PATH` 仅作兼容回退）；`cmd/download` 报错文案更新。
- 配置/部署：`.env.example` 改 PG 口径（`DATABASE_URL`/`POSTGRES_*`/连接池/`DATA_DIR`）；`docker-compose.yml`、`docker-compose.prod.yml`、`scripts/setup/docker-compose.server.yml` 均新增 `postgres:18-alpine` 服务（健康检查 `pg_isready`、`pg_data` 卷）、网关 `depends_on: postgres(healthy)` 与 `DATABASE_URL` 注入。三份 compose YAML 解析校验通过。
- 迁移机制 PG 方言：`schema.go` 台账表 `BIGINT PRIMARY KEY`、版本记录 `INSERT … ON CONFLICT DO UPDATE`；`sql_migrations.go` 台账表 `BIGINT`、占位符 `?`→`$1/$2`。
- 闸门：`go build ./...`、`go vet ./...` 退出码 0。

### B1 已完成（静态转换，待 PG 建库核对）
- 类型方言：`schema.go` V2–V15 与 58 个 `.sql` 全部 `INTEGER PRIMARY KEY AUTOINCREMENT`→`BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY`、`REAL`→`DOUBLE PRECISION`、其余 `INTEGER`→`BIGINT`；`TEXT` 保留。
- 核心表基线：`migrateV1` 补 `xt_users`（19 列，含 MFA/套餐/积分/推荐人）与 `xt_orders`（22 列，含 exchange_order_id）PG 终态建表，先于后续 ALTER。
- 迁移容错：`isDuplicateColumnErr()` 兼容 SQLite `duplicate column` 与 PG `already exists`/`42703`；V14/V15 与 `migrateXTOrders` 接入。
- 台账：`schema_version`/`sql_migrations` 表 `BIGINT`、`ON CONFLICT` upsert、占位符 `$n`。
- `.sql` 已扫：无 `datetime('now')`/`julianday`/`strftime`/`GROUP_CONCAT`/`json_extract`/`INSERT OR REPLACE` 残留。

### 待办（需 PG 实例环境验证）
- B2：repo 层运行时 SQL 方言，35 个非测试文件、51 处特殊方言 + 数百处占位符：
  - `?`→`$1,$2,…`（按单条语句内顺序编号）；
  - 26 处 `LastInsertId()` → `INSERT … RETURNING id` + `QueryRow().Scan(&id)`；
  - `INSERT OR REPLACE/IGNORE`→`ON CONFLICT … DO UPDATE/NOTHING`；
  - `rowid`→`ctid`/业务主键；`sqlite_master`→`information_schema.tables`；
  - 唯一冲突错误识别 `UNIQUE constraint failed`→PG SQLSTATE 23505；类型扫描对齐。
- B3：测试 setup 连 PG（`TEST_DATABASE_URL`），无库 `t.Skip`。
- B5：`deploy-linux.sh` 去 sqlite3 安装/`.db` 初始化，加装 postgres、建库建用户，systemd 改 `EnvironmentFile` + `After=postgresql.service`；文档/CI 同步。

### 待 PG 建库核对的风险点
- 核心表终态列以仓储 SELECT/UPDATE 反推，可能漏个别仅在写入路径出现的列；建库后对照 `information_schema.columns` 与 repo SQL 补齐。
- 布尔列保留 `BIGINT 0/1`，扫描层按 int 收；若某列改 `BOOLEAN` 需同步扫描目标。
- `xt_users.created_at` 保留 `TEXT`（扫描为字符串），与其余表 `BIGINT` 时间戳口径不同，勿统一改类型。

---

## 一、背景与目标

当前网关唯一业务库是嵌入式 **SQLite**（纯 Go 驱动 `modernc.org/sqlite`）。随着多用户、多 worker 方向演进，SQLite 单写者模型成为瓶颈，需要把主库整体切到 **PostgreSQL**，与对标项目一致。

目标：
1. 主库由 SQLite 替换为 PostgreSQL，**代码层不再保留 SQLite 驱动与连接路径**。
2. 全部业务表、迁移、仓储 SQL、测试、部署配置、运维文档对齐 PG 方言。
3. 连接池、迁移台账、健康检查、备份口径按服务化标准补齐。

## 二、现状基线（已核实）

| 项 | 现状 |
|---|---|
| 连接入口 | `gateway/internal/store/store.go` `InitDB()`：`sql.Open("sqlite", DB_PATH)`，全局 `*sql.DB` |
| 连接环境变量 | 代码只读取 `DB_PATH`（SQLite 文件路径）；`.env.example` 里的 `DB_TYPE` / `DATABASE_URL` **当前未接线**（仅注释预留） |
| SQL 驱动依赖 | `modernc.org/sqlite v1.51.0`（go.mod） |
| 仓储层 | `internal/store/` 60+ repo；`internal/data/`（OHLCV/tick）复用 `store.GetDB()`；`internal/agent*`、`social`、`marketplace`、`ml`、`reconcile`、`community`、`indicator` 等直接或间接访问同一库 |
| Go 版本迁移 | `schema.go` V1–V15 函数迁移 + `schema_version` 台账表 |
| 纯 SQL 迁移 | `internal/store/migrations/sql/` 共 **58** 个 `.sql`，`sql_migrations` 台账表，按文件名序、分号切分、独立事务 exactly-once |
| Redis | go-redis v9，可选缓存；prod compose 已有 redis 容器（维持现状，不强制） |
| Rust 撮合引擎 | 进程内状态，不落库（不在本次主库范围；重启恢复见遗留项 C6） |
| 测试 | **58** 个测试文件用临时 SQLite 文件 + `InitDB()` 起库（`DB_PATH` 指向 `t.TempDir()` 下 `.db`） |
| 部署 | `docker-compose.prod.yml` 仅有 redis，无 DB 服务；gateway 挂 `gateway_data` 卷存 SQLite 文件；`deploy-linux.sh` / `docs/DEPLOYMENT.md` 多处 `sqlite3` 运维命令 |

### 方言渗透规模（已统计）

- SQLite 特有写法（`AUTOINCREMENT` / `LastInsertId` / `INSERT OR REPLACE` / `datetime('now')` / `rowid` / `sqlite_master` 等）：**98 处，跨 50 个 Go 文件**。
- `store` 包内 SQL 调用（`Exec/Query/QueryRow`）数百处；占位符全部为 `?`。
- 金额/时间多以整数戳或 REAL 列存储；布尔多用 `INTEGER 0/1`。

## 三、范围与非目标

**范围内**
- 主库 SQLite → PostgreSQL；移除 `modernc.org/sqlite` 依赖与 SQLite 连接/DSN 路径。
- 仓储 SQL、DDL、迁移机制、测试 setup、compose/env、systemd/部署脚本、运维文档对齐 PG。
- 连接池参数化（min/max、生命周期、健康检查）。

**非目标（本轮不做）**
- 不引入 Kafka 事件总线（单体进程内事件总线维持现状；对标项目的 Kafka 留待多副本服务化阶段再评估）。
- Redis 维持可选缓存，不升级为双实例/强制依赖。
- 不把现存 JSON 文件持久化（策略配置/日志/模板/agent tokens 的文件兜底）并入数据库——保留双写兜底，后续单列任务。
- Rust 引擎进程内状态持久化不在本次范围。

## 四、环境前置条件

| 条件 | 现状 | 说明 |
|---|---|---|
| PG 驱动 | 待引入 `github.com/jackc/pgx/v5 v5.13.0`（纯 Go，无需 C 工具链；stdlib 兼容 `database/sql`） | 本机无 gcc，必须选纯 Go 驱动 |
| Go 模块代理 | `proxy.golang.org` 直连超时；`goproxy.cn` 可达，pgx 可经国内镜像拉取 | 实施时 `GOPROXY=https://goproxy.cn,direct` |
| PG 实例 | **本机无 Docker、无 PostgreSQL 服务** | 编译/静态自查可在本机完成；测试绿灯与运行验证需要有 PG 的环境（容器或远端实例） |
| 网络 | 本机断网，交易所 API / WS 不可达（环境噪声，不算代码回归） | — |

> 备选驱动 `github.com/lib/pq`：纯 Go 但维护放缓；优先 pgx v5。两者占位符都用 `$1`，不翻译 `?`。

## 五、总体设计

### 5.1 连接层（B0）

- 新增统一数据库初始化：按 `DATABASE_URL`（`postgresql://user:pass@host:port/db?sslmode=...`）解析连接；也支持拆分环境变量 `POSTGRES_HOST/PORT/USER/PASSWORD/DB_NAME`（对标 QuantDinger）。
- 用 pgx stdlib 注册驱动后 `sql.Open("pgx", dsn)`；设置连接池：`SetMaxOpenConns / SetMaxIdleConns / SetConnMaxLifetime / SetConnMaxIdleTime`，参数走 `DB_POOL_MIN/MAX` 等环境变量（缺省安全值）。
- 启动期做一次连通性 ping + 关键表权限探测（对标 QuantDinger 启动探针，缺权限高可见报错而不是跑起来后静默失败）。
- 全局 `GetDB() *sql.DB` 签名保持不变，仓储层无需改取连接方式。

### 5.2 方言策略（最小改动面）

为避免类型重构放大风险，**保留现有存储语义，只替换 PG 不兼容的语法**：

- 时间：继续用 INTEGER 毫秒/秒级 Unix 戳，**不引入 `TIMESTAMPTZ`**，扫描层不改时间类型。
- 数值：价格/数量列 `REAL` → `DOUBLE PRECISION`；金额整数微单位列保持 `INTEGER/BIGINT`。
- 布尔：先评估扫描适配；默认保留 `INTEGER 0/1` 语义（Go 侧扫 int），避免全链路布尔重构；若某列改 `BOOLEAN`，扫描目标需同步对齐。
- 主键：TEXT 主键保持不变；自增整数主键改 PG identity/serial。

### 5.3 迁移策略（B1）

- PG **全新库**直接跑一份**全量基线 DDL**（合并现有 V1–V15 与 58 个 `.sql` 的全部表/列/索引），不再逐条回放 SQLite 增量迁移。
- 保留 `schema_version` / `sql_migrations` 台账表；后续 schema 变更继续走 `.sql` 增量文件，但方言为 PG。
- 历史 SQLite 增量迁移文件仅服务于旧库升级 / 数据导入路径（见 B4）。

## 六、SQLite → PostgreSQL 方言对照表

| 主题 | SQLite 现状 | PostgreSQL 改造 |
|---|---|---|
| 占位符 | `?` | `$1, $2, …`（pgx/lib/pq 不翻译 `?`，需逐条改写） |
| 自增主键 | `INTEGER PRIMARY KEY AUTOINCREMENT` | `BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY`（或 `BIGSERIAL`） |
| 取自增 ID | `res.LastInsertId()` | `INSERT … RETURNING id`，`QueryRow(...).Scan(&id)` |
| upsert | `INSERT OR REPLACE INTO …` | `INSERT … ON CONFLICT (pk) DO UPDATE SET … = excluded.…` |
| 当前时间 | `datetime('now')` | `NOW()`，或由应用层传 Unix 戳（现有多为应用层传戳，优先保留） |
| 行号排序 | `ORDER BY rowid` | 改用业务主键 `id`；无主键顺序需求时用 `ctid` |
| 探表 | `sqlite_master WHERE type='table'` | `information_schema.tables` / `to_regclass(...)` |
| 列类型 | `TEXT` | `TEXT`（兼容，保留） |
| 列类型 | `REAL` | `DOUBLE PRECISION`（价格/数量） |
| 列类型 | `INTEGER` | `BIGINT` |
| 列类型 | 布尔 `INTEGER 0/1` | 保留 `INTEGER` 或改 `BOOLEAN`（随扫描适配决定，见 5.2） |
| 加列 | `ALTER TABLE t ADD COLUMN c …`（重复列容错靠忽略错误） | PG 无 `ADD COLUMN IF NOT EXISTS`；迁移前查 `information_schema.columns` 判断，或用 `DO $$ … IF NOT EXISTS … $$` 块；重复列不再靠静默忽略 |
| 索引 | `CREATE INDEX IF NOT EXISTS` | PG 支持 `IF NOT EXISTS`（保留） |
| 唯一约束 | `UNIQUE(a,b)` | 兼容（保留） |
| 字符串模糊 | `LIKE ?` | 占位符随 `?`→`$n`；`LIKE` 语义保留 |
| 大小写不敏感比较 | `UPPER(x)=UPPER(?)` | 兼容 PG（保留） |
| 聚合/空值 | `COALESCE / SUM / GROUP BY` | 兼容（保留） |
| 分页 | `LIMIT ?` | 占位符改 `$n`；`LIMIT` 兼容 |
| 事务 | `db.Begin()/Commit()/Rollback()` | `database/sql` 接口兼容（保留） |
| 布尔写入 | 传 `bool` 到 INTEGER 列 | 按最终列类型对齐扫描/写入（见 5.2） |

### 易漏点（实施时逐项排查）

- `rowid` 隐式行号（如 `order_repo.go` 两条按 `rowid` 排序的查询）。
- `sqlite_master` 探表（测试与启动探针）。
- 动态拼接 WHERE 的 `buildFilter`（`repository.go`）：占位符生成集中在此，优先改为 `$n`。
- `IN (?)` 多值展开、`LIMIT ?` 边界占位符。
- 浮点时间戳列（`xt_orders.created_at/updated_at` 历史 REAL）扫描先收 `float64` 再转 `int64` 的既有兼容逻辑，PG 下复核是否仍需要。
- 金额/计数列的 NULL 与零值扫描目标类型对齐。

## 七、批次计划

> 每批闸门：`CGO_ENABLED=0 go build ./...` + `go vet ./internal/<pkg>/`；有 PG 环境再跑目标包 `go test -count=1`。本机无 PG 时测试允许跳过（B3），但不允许编译/vet 报错。

### B0 连接层与配置部署（任务 #28）
- go.mod 引入 `pgx/v5`；移除 `modernc.org/sqlite`。
- 重写 `InitDB`：解析 `DATABASE_URL` / `POSTGRES_*`，建连接池，启动 ping + 关键表探测。
- `.env.example`：`DB_TYPE=postgresql`，`DATABASE_URL=postgresql://…`，补 `POSTGRES_*` 与 `DB_POOL_MIN/MAX`；删除 `DB_PATH` SQLite 口径。
- `docker-compose.prod.yml`：新增 `postgres` 服务（镜像、卷、健康检查、`max_connections`/`shared_buffers`），gateway `depends_on: postgres(healthy)`，挂库卷替换文件库卷。
- 闸门：`go build ./...`、`go vet` 通过；无 PG 时启动期报错信息清晰。

### B1 PG 全量基线 DDL 与迁移台账（任务 #29）
- 汇总现有全部表/列/索引为一份 PG 基线 `init.sql`（或 Go 建表），方言按第六节转换。
- 保留 `schema_version` / `sql_migrations` 台账；新增 PG 版增量迁移目录约定。
- 核对每张表：自增列、唯一约束、索引、NOT NULL/默认值与现状一致。
- 闸门：在有 PG 的环境跑基线建库成功，台账表可写。

### B2 分批改写 repo 层 SQL 方言（任务 #30）
按包分批，每批过编译 + 目标包测试：
1. `store` 核心：用户/订单/成交/持仓/信号/风控/快照。
2. `store` 业务：AI bot、市场/订阅/分成、计费/推荐、ML、通知、 ladder/grid/dca/lmartin。
3. `data`（OHLCV/tick）、`agent*` 各连接器仓储。
4. `social` / `marketplace` / `community` / `indicator` / `reconcile` / `ml`。
- 每处按第六节对照表改占位符、upsert、RETURNING、rowid/ctid、类型扫描。

### B3 测试改造（任务 #31）
- 公共 `setupTestDB` 改读 `TEST_DATABASE_URL`（PG）；未设置或连不上时 `t.Skip("postgres unavailable")`，**跳过而非失败**，保证本机 `go test` 不红。
- 测试内 `sqlite_master` 探表改 `information_schema`；去掉 `.db-wal/.db-shm` 清理。
- 有 PG 的 CI/容器环境跑全量测试绿灯。

### B4 可选：旧库数据迁移工具（任务 #32）
- 仅当决定保留本地旧 SQLite 历史数据时实施。
- 一次性工具（`cmd/migrate-sqlite-to-pg`）：按表读旧 SQLite → 类型映射 → 灌入 PG；处理自增 ID 映射、唯一键冲突、金额/时间戳换算。
- **默认口径：全新 PG 空库起步，不迁旧数据**（本地为开发脏数据，与 P0 清库口径一致）。

### B5 运维与部署文档固化（任务 #33）
- `docs/DEPLOYMENT.md`：备份 `sqlite3 .backup` → `pg_dump`；排障 `sqlite3 .schema` → `psql \dt`；`PRAGMA busy_timeout` → PG 连接池/锁参数；VACUUM/REINDEX 对应 PG 命令。
- `deploy-linux.sh` / systemd 单元：去 `DB_PATH`/sqlite 文件，注入 `DATABASE_URL`；`After=postgres.service`。
- CI（`.github/workflows/ci.yml`）：加 PG service 容器、迁移步骤、测试环境 `TEST_DATABASE_URL`。
- 回滚口径：保留 SQLite 只读快照一段过渡期（代码层已不连 SQLite，但旧 `.db` 文件备份保留）。

## 八、关键风险与回滚

| 风险 | 缓解 |
|---|---|
| 数百处 SQL 占位符/方言漏改，运行期才报错 | 按包分批，每批编译 + PG 环境测试；方言对照表 + 易漏点清单逐项排查 |
| PG 强类型与 SQLite 动态类型不符导致扫描失败 | 5.2 保留既有类型语义；扫描目标随列类型对齐；金额/时间/布尔单列复核 |
| 本机无 PG，测试无法在本机绿灯 | B3 无库跳过；CI/容器 PG 环境补全量绿灯；不把跳过误判为通过 |
| 切库期间旧库数据丢失 | 默认空库起步；保留旧 `.db` 备份；B4 工具按需导出导入 |
| 连接池/并发参数不当压垮 PG | 连接池默认值对标单机 ~50 并发；监控 `connection pool exhausted` 再调 |

**回滚**：批次原子提交；B0 之前的提交保持 SQLite 可用。若 PG 路径出阻断问题，回退到 B0 之前镜像；旧库文件在过渡期内只读保留。

## 九、待决策项（默认建议，待确认）

1. **旧数据**：默认全新空库、不迁（B4 仅按需）。若要保留历史用户/订单/持仓/策略，启用 B4。
2. **范围边界**：默认只换主库；Redis 维持可选；不引 Kafka；JSON 文件兜底保留。如需把 JSON 持久化并入库或引入 Kafka，单独立项。
3. **实施节奏**：本机可完成编译/静态自查；测试绿灯与运行验证需有 PG 实例。建议先补环境（Docker Desktop + PG 容器 + 联网拉 pgx）再进入 B2 批量改写，避免在无复核条件下堆积不可验证的改动。

## 十、附录：相关文件索引

- 连接初始化：`gateway/internal/store/store.go`（`InitDB` / `CloseDB` / `GetDB`）
- Go 版本迁移：`gateway/internal/store/schema.go`
- SQL 迁移机制：`gateway/internal/store/sql_migrations.go`
- SQL 迁移目录：`gateway/internal/store/migrations/sql/`（58 个）
- 仓储基类/过滤拼接：`gateway/internal/store/repository.go`（`buildFilter`）
- 行情存储：`gateway/internal/data/storage.go`、`gateway/internal/data/tick_storage.go`
- 部署 compose：`docker-compose.prod.yml`、`docker-compose.yml`
- 环境模板：`.env.example`
- 部署脚本：`deploy-linux.sh`
- 运维文档：`docs/DEPLOYMENT.md`
- CI：`.github/workflows/ci.yml`
