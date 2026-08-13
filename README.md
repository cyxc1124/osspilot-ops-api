# osspilot-ops-api

OssPilot 运营 API（Go）。对照 [OssPilot](https://github.com/cyxc1124/OssPilot) `v0.6.0` 按切片重写。

## 本地

```bash
export DATABASE_URL=postgres://osspilot:osspilot@127.0.0.1:5432/osspilot_ops?sslmode=disable
go run ./cmd/migrate up
go test ./...
go run ./cmd/api
```

- `GET /healthz`
- `POST /api/login`、`POST /api/logout`、`GET /api/me`、`POST /api/password/change`
- `GET|POST /api/users`、`GET|PUT|DELETE /api/users/{user_id}`、`POST /api/users/{user_id}/password/reset`（仅 platform_admin）
- `GET /api/regions`（登录即可）、`POST|PUT|DELETE /api/regions`（仅 platform_admin）
- `GET /api/settings`（登录即可）、`PUT /api/settings`（仅 platform_admin）
- `GET|POST /api/tenant-users`、`GET|PUT|DELETE /api/tenant-users/{user_id}`、`POST .../password/reset`（仅 platform_admin）
- `GET|POST /api/buckets`、`POST /api/buckets/import-batch`（仅 platform_admin；不访问 RGW）
- `GET|PUT /api/tenant-users/{user_id}/buckets`、`DELETE .../buckets/{bucket_id}`（授权并投影到租户 API）
- `GET /api/lifecycle-rules`、`POST /api/buckets/{bucket_id}/lifecycle-rules`、`PUT|DELETE /api/lifecycle-rules/{rule_id}`（仅 platform_admin；只存规则，不跑清理任务）

迁移后内置 `admin` / `admin`，`must_change_password=true`。首次登录后除改密、`/api/me`、登出外一律 403。新密码至少 8 位且不能与旧密码相同。

运营角色在 `ops_roles` / `ops_user_roles`，一个用户可同时有 `platform_admin` 和 `ops_operator`。写路径仍仅 `platform_admin`。不能停用或删除自己。无审计日志。

租户账号存在运营库 `tenant_accounts`。配置 `TENANT_API_URL` 与 `PROJECTION_SECRET` 后，账号与桶授权会投影到租户 API（用户名对齐，无跨库外键）。未配置时只写运营库。桶登记只记名字，不创建 RGW 桶。`GET /api/s3/buckets` 暂返回空列表。

区域 `tenant_count` 按绑定账号计数；有绑定时不能删区域。RGW 密钥明文存库、接口返回打码；`********` 表示不改密钥。

契约见 `openapi.yaml`。无 `DATABASE_URL` 时 healthz 仍可用，鉴权接口返回 503。

## 许可

AGPL-3.0-only
