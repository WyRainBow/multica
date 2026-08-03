# 本地启动（不用 Docker）

仓库默认的 `make dev` 会用 Docker 起 PostgreSQL。这份文档记录**不装 Docker**、直接用本机 PostgreSQL 跑起前后端的完整步骤。

适用场景：装不了 Docker、拉不动镜像（国内 DNS 污染 `registry-1.docker.io` 很常见）、或者本机已经有 PostgreSQL 不想再多一层。

## 一、为什么不能用 make

`scripts/ensure-postgres.sh` 在判定数据库地址是 `localhost` / `127.0.0.1` / `::1` 时，会强制执行 `docker compose up -d postgres`：

```bash
# scripts/ensure-postgres.sh
if is_local; then
  echo "==> Ensuring shared PostgreSQL container is running on localhost:5432..."
  docker compose up -d postgres
```

`make dev`、`make server`、`make setup` 都会调它，所以本机装了 PostgreSQL 也绕不开。本文的做法是**跳过这些 make 目标，手动跑三条命令**。

## 二、依赖

| 依赖 | 版本要求 | 来源 |
| --- | --- | --- |
| Go | 1.26.1+ | `server/go.mod` 写死了 `go 1.26.1` |
| Node.js | 20+ | CI 用 22 |
| pnpm | 10.28+ | |
| PostgreSQL | 17 | CI 用 `pgvector/pgvector:pg17` |

macOS 安装：

```bash
brew install go postgresql@17
```

**不需要装的**（虽然 compose 用的是 pgvector 镜像）：

- **pgvector** —— 273 个迁移文件里没有任何 `vector` 类型，镜像选型是历史遗留。
- **pg_bigm** —— 迁移 032 / 033 / 039 用 `DO $$ ... EXCEPTION` 包着，装不上会跳过；迁移 138 / 139 有 `pg_trgm` 的 fallback 索引兜底。影响：中文子串搜索退化，不影响启动。
- **pg_cron** —— 迁移 076 同样是 EXCEPTION 兜底。影响：token 用量日汇总不会自动跑。

`pgcrypto` 和 `pg_trgm` 是 PostgreSQL contrib 自带的，brew 的 `postgresql@17` 里就有。

## 三、准备 PostgreSQL

### 如果本机已经有别的 PostgreSQL

用不同端口共存，别动老的。下面以 **5433** 为例：

```bash
# 改端口
sed -i '' 's/^#port = 5432.*/port = 5433/' /opt/homebrew/var/postgresql@17/postgresql.conf

# 启动（开机自启）
brew services start postgresql@17

# 等就绪
/opt/homebrew/opt/postgresql@17/bin/pg_isready -h 127.0.0.1 -p 5433
```

### 建角色和库

```bash
export PATH="/opt/homebrew/opt/postgresql@17/bin:$PATH"

psql -h 127.0.0.1 -p 5433 -d postgres -v ON_ERROR_STOP=1 <<'SQL'
CREATE ROLE multica LOGIN SUPERUSER PASSWORD 'multica';
SQL

createdb -h 127.0.0.1 -p 5433 -O multica multica

psql -h 127.0.0.1 -p 5433 -d multica -v ON_ERROR_STOP=1 \
  -c 'CREATE EXTENSION IF NOT EXISTS pgcrypto; CREATE EXTENSION IF NOT EXISTS pg_trgm;'
```

角色需要 `SUPERUSER` 是因为部分迁移会尝试 `CREATE EXTENSION`。

验证：

```bash
psql "postgres://multica:multica@127.0.0.1:5433/multica?sslmode=disable" -Atc "select current_user, current_database()"
# multica|multica
```

## 四、配置 .env

```bash
cp .env.example .env
```

改这两行指向本机数据库（端口按上一步实际用的填）：

```bash
POSTGRES_PORT=5433
DATABASE_URL=postgres://multica:multica@127.0.0.1:5433/multica?sslmode=disable
```

其余保持默认即可：后端 `PORT=8080`，前端 `FRONTEND_PORT=3000`。

## 五、跑迁移

```bash
pnpm install

cd server && set -a && . ../.env && set +a && go run ./cmd/migrate up
```

`set -a` 让 `.env` 里的变量导出成环境变量，`go run` 才读得到。看到 `Done.` 就成功了。

## 六、启动

两个终端，或者各自后台跑。

**后端**：

```bash
cd server && set -a && . ../.env && set +a && go run ./cmd/server
```

**前端**（仓库根目录）：

```bash
set -a && . ./.env && set +a && pnpm dev:web
```

访问：

- 前端 http://localhost:3000
- 后端健康检查 http://localhost:8080/healthz

## 七、注册账号：验证码在哪

本地默认没有配发信通道（`.env` 里 `SMTP_HOST=` 是空的，也没有 `RESEND_API_KEY`），**验证码邮件不会真的发出去**，只写进数据库。

取最新验证码：

```bash
psql "postgres://multica:multica@127.0.0.1:5433/multica?sslmode=disable" -Atc \
  "SELECT code FROM verification_code
   WHERE email='你的邮箱' AND used=false AND expires_at>now()
   ORDER BY created_at DESC LIMIT 1"
```

后端日志里也会打印：`[DEV] Verification code for <email>: 123456`。

## 八、CLI 连本地实例

CLI 默认 profile 可能连着云端。用独立 profile 隔离，不要覆盖默认配置：

```bash
multica --profile local setup self-host \
  --server-url http://localhost:8080 \
  --app-url http://localhost:3000
```

会打开浏览器做 OAuth 登录，配置写到 `~/.multica/profiles/local/config.json`，跟默认 profile 完全隔离。

之后所有本地操作都要带 `--profile local`：

```bash
multica --profile local issue list
multica --profile local daemon status
```

`make daemon` 已经内置了 `--profile local`。

## 九、跑测试

Go 测试连的是 `DATABASE_URL`，**不带环境变量会静默跳过**（输出仍然是 `ok`，很容易误以为通过了）：

```bash
# 错误示范：会跳过，但显示 ok
cd server && go test ./internal/handler/

# 正确
cd server && set -a && . ../.env && set +a && go test ./internal/handler/ -count=1
```

跳过时会打印这行，注意看：

```
Skipping tests: database not reachable: ... role "multica" does not exist
```

TS 测试不依赖数据库：

```bash
pnpm typecheck
pnpm lint
pnpm test
```

## 十、常见问题

**`docker compose up -d postgres` 报 TLS 证书错误**

DNS 污染，`registry-1.docker.io` 被解析到了 Facebook 的 IP 段（`31.13.x` / `69.171.x`）。验证：

```bash
dig +short registry-1.docker.io
```

这份文档的做法本来就不需要 Docker，遇到这个报错说明你还在走 `make dev`，改用第五、六节的手动命令。

**`psql` 连的是旧版本**

brew 的 `postgresql@15` 会 shadow `postgresql@17` 的命令。要么用全路径 `/opt/homebrew/opt/postgresql@17/bin/psql`，要么 `brew link --overwrite postgresql@17`。

**迁移报 `role "multica" does not exist`**

`.env` 里的 `DATABASE_URL` 还指着 5432（旧实例），改成第四节的值。

**端口被占**

```bash
lsof -nP -iTCP:8080 -iTCP:3000 -iTCP:5433 -sTCP:LISTEN
```
