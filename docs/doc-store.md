# 个人文档库（multica store）

类似 Redis 的 key/value 用法：把每周的运营文档（Excel、Markdown、CSV 等）按 key 存进来，之后按 key 取回、按前缀列举、删除。值是原始文件字节，服务端不做解析或改写。

**访问范围：仅白名单邮箱账号可用。** 默认只允许 `shuaijin@tencent.com`；部署时可用 `DOC_STORE_ALLOWED_EMAILS`（逗号分隔）覆盖，环境变量有值时**替换**默认值而不是追加。其他账号调用返回 `403`。

## 命令

    # 存入（覆盖同名 key）
    multica store set weekly/2026-11-13/report.md ./report.md

    # 从 stdin 存入
    cat report.md | multica store set weekly/2026-11-13/report.md -

    # 读取到 stdout
    multica store get weekly/2026-11-13/report.md

    # 读取到文件
    multica store get weekly/2026-11-13/report.md -o /tmp/report.md

    # 按前缀列出
    multica store list --prefix weekly/2026-11-13/

    # JSON 输出（便于脚本处理）
    multica store list --prefix weekly/ --json

    # 删除
    multica store del weekly/2026-11-13/report.md

`set` 会根据扩展名推断 Content-Type，也可用 `--content-type` 指定。

## 规则

- key 长度 1–512，可含 `/` 组织层级；不能以 `/` 开头，不能有空段、`.` 或 `..` 段，不能有首尾空白或控制字符。
- 值为单个文件，上限 **25 MiB**。
- `set` 是覆盖写；`get` 返回最新值。
- 删除不可恢复。
- 没有 TTL、没有内容全文检索；列举只支持前缀匹配。
- 数据按账号隔离：不同用户的同名 key 互不可见。

## HTTP API

所有接口都需要登录（`Authorization: Bearer <token>`），且账号在白名单内。

| 方法 | 路径 | 说明 |
|---|---|---|
| PUT | `/api/doc-store/{key}` | 请求体为原始字节；Content-Type 即存储的类型 |
| GET | `/api/doc-store/{key}` | 返回原始字节；响应头含 `X-Doc-Key`、`X-Doc-Updated-At` |
| DELETE | `/api/doc-store/{key}` | 删除，成功 204；不存在 404 |
| GET | `/api/doc-store?prefix=&limit=&offset=` | 列出元信息，返回 `{keys:[...], total}` |

错误码：未登录 401；非白名单 403；key 非法或内容类型非法 400；超过 25 MiB 413；不存在 404。

## 部署

后端需要应用迁移 `104_doc_store`，并重新构建 `multica` CLI 以获得 `store` 子命令。环境变量：

    DOC_STORE_ALLOWED_EMAILS=shuaijin@tencent.com

不设置时使用代码内默认值（同上）。
