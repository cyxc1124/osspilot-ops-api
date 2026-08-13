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

迁移后内置 `admin` / `admin`，`must_change_password=true`。首次登录后除改密、`/api/me`、登出外一律 403。新密码至少 8 位且不能与旧密码相同。

运营角色暂存在 `ops_users.role` 一列（前端仍收 `ops_roles` 数组；同时给了两个角色时保留 `platform_admin`）。不能停用或删除自己。无审计日志。

`tenant_count` 暂为 0（等 O5 租户账号）。不能删除当前默认区域。

契约见 `openapi.yaml`。无 `DATABASE_URL` 时 healthz 仍可用，鉴权接口返回 503。

## 许可

AGPL-3.0-only
