# 安装与配置

## 编译要求

- Go 版本以根目录 `go.mod` 的 `go` 指令为准。
- 默认 SQLite 驱动需要 CGO 和可用的 C 编译器。
- 新增或修改主题、插件后需要重新编译。

```bash
go build -o gopherink ./cmd/gopherink
./gopherink
```

默认监听 `:8080`，SQLite 数据库位于 `data/gopherink.db`。首次空库会创建 Schema，并根据环境配置创建管理员或进入 Web 安装流程。

## 环境变量

| 变量 | 作用 | 默认/回退行为 |
|---|---|---|
| `GOPHERINK_ADDR` | HTTP 监听地址 | `:8080` |
| `GOPHERINK_DB_DRIVER` | `sqlite3`、`mysql`、`mariadb` 或 `postgres` | 启动流程选择/SQLite |
| `GOPHERINK_DB_DSN` | 主数据库 DSN | `data/gopherink.db` |
| `GOPHERINK_DB_WRITE_DSN` | 独立写库 DSN | 回退到 `GOPHERINK_DB_DSN` |
| `GOPHERINK_DB_READ_DSN` | 读库 DSN | 未设置时使用写库 |
| `GOPHERINK_ADMIN_USER` | 初始管理员用户名 | `admin` |
| `GOPHERINK_ADMIN_PASSWORD` | 初始管理员密码 | `admin123` |
| `GOPHERINK_ADMIN_MAIL` | 初始管理员邮箱 | `admin@example.com` |
| `GOPHERINK_WEB_INSTALL` | 空库时启用浏览器安装向导 | `true` |
| `GOPHERINK_DATA_DIR` | 数据目录 | `data` |
| `GOPHERINK_UPLOAD_DIR` | 附件文件系统根目录 | `<GOPHERINK_DATA_DIR>/uploads` |

生产环境必须显式设置初始管理员密码，或通过安装向导立即改为强密码。不要继续使用示例默认凭据。

## DSN 示例

SQLite：

```text
data/gopherink.db
```

MySQL/MariaDB：

```text
user:password@tcp(127.0.0.1:3306)/gopherink?charset=utf8mb4&parseTime=true
```

PostgreSQL：

```text
postgres://user:password@127.0.0.1:5432/gopherink?sslmode=disable
```

读写分离时，读写 DSN 必须使用同一数据库方言。复制、故障切换和只读权限由数据库基础设施负责。

## 持久化目录

部署时至少持久化以下位置：

| 路径 | 内容 |
|---|---|
| `data/gopherink.db` | 默认 SQLite 数据库 |
| `data/waf.log` | WAF 独立事件日志 |
| `data/uploads/posts/` | 默认文章附件目录 |
| `data/uploads/pages/` | 默认独立页面附件目录 |
| `data/uploads/unattached/` | 默认未绑定附件及保留策略迁出目录 |
| `data/uploads/admin-settings/` | 默认后台个性化素材目录 |
| `data/uploads/theme-settings/` | 默认主题设置素材目录 |

上表使用默认 `GOPHERINK_DATA_DIR=data`。设置 `GOPHERINK_UPLOAD_DIR` 后，五个子目录都位于该自定义根目录；浏览器公开 URL仍以 `/uploads/` 开头。使用外部数据库时仍需持久化 `data/waf.log` 和本地上传目录。使用插件接管附件存储时，应按插件实现备份远程对象及其元数据。

## 配置存储

后台设置最终保存到 `gb_options`。`core/services/options.go` 负责补全缺失默认值，其中认证密钥 `auth_secret` 在不存在时随机生成。

配置分为几类：

- 站点、阅读、评论、附件、API、HTTP 和 WAF 等核心选项。
- `theme:<theme-name>`：主题配置 JSON。
- `plugin:<plugin-name>`：插件站点配置 JSON。
- `plugin:<plugin-name>:personal`：插件按用户保存的个人配置 JSON。
- 后台个性化选项：背景、颜色和各组件透明度等。

不要在多实例间使用不同的 `auth_secret` 数据库副本，否则会话 Cookie 和 CSRF 令牌无法互认。

## 基础 URL 和反向代理

`base_url` 用于固定链接、订阅、Sitemap、Pingback 等绝对 URL。生产环境应填写浏览器实际访问的 HTTPS 地址，避免生成 `localhost` 或错误协议链接。

反向代理环境中的客户端 IP 信任是独立安全设置：

- 未启用信任时，以直接连接地址为准。
- 启用后可选择白名单或黑名单模式，并逐行填写 IP/CIDR。
- 只有满足信任规则的代理来源才能影响 `X-Forwarded-For` / `X-Real-IP` 解析。

不要无条件信任来自公网的转发头，否则攻击者可以伪造 IP 绕过限流和登录封禁。详细规则见 [安全与 WAF](security-and-waf.md)。

## 上传和图片内存

上传大小在后台以 MB 配置，默认 16 MB。图片处理内存预算默认 256 MB，最低允许 64 MB。预算用于在处理超大图片前做估算保护，不是 Go 进程总内存硬限制。

图片转换失败时，上传流程会回退保存原始文件并向后台返回提示；缩略图可选择 JPG、WebP 或禁用。详细行为见 [附件与图片处理](media-and-images.md)。

## 数据库备份

后台备份能力与数据库驱动和当前处理器实现相关。无论是否使用后台入口，建议生产部署执行一致性备份：

- SQLite：在停写或使用 SQLite 在线备份机制时复制数据库文件。
- MySQL/MariaDB：使用 `mysqldump` 或物理备份。
- PostgreSQL：使用 `pg_dump` 或物理备份。
- 同时备份本地 `uploads/` 和必要的 `data/` 文件。

附件数据库记录只保存路径/URL及元数据，不包含本地文件本体，只备份数据库无法恢复媒体资源。

## 部署检查

1. 设置真实 `base_url` 和强管理员密码。
2. 持久化数据库、上传目录和 WAF 日志目录。
3. HTTPS 终止后再按需启用 HSTS；证书和 HTTPS 链路未稳定前保持关闭。
4. 仅信任明确的反向代理 IP/CIDR。
5. 根据服务器资源调整上传大小、图片处理预算和缩略图策略。
6. 根据访问量设置公开缓存 TTL 和各类 WAF 限流。
7. 变更插件或主题后重新编译，并在上线前验证其路由和钩子。
