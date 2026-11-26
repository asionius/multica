# 研发运营报告发布接口（运营 agent 交接）

面向：后续负责生成运营报告并通过 API 发布的 agent / 运营同学。

站点只展示**已批准成稿**，不在服务端或浏览器重算任何指标。发布一次即生成一个不可变修订；更正必须再发一个新修订。

## 1. 鉴权与作用域

- 使用已获授权的用户凭证：`Authorization: Bearer <JWT>`（站点 Cookie 会话或 PAT 均可，沿用现有鉴权）。
- 工作区必须通过现有请求头指定：`X-Workspace-Slug: <workspace-slug>`（或 `X-Workspace-ID`）。**workspace 不接受请求体声明。**
- 只有该工作区的 `owner` / `admin` 可以发布；普通成员可读、发布返回 403。
- 不提供共享万能密钥；凭证通过现有部署配置注入，不写入仓库、URL 或报告正文。

## 2. 发布

`POST /api/dev-reports`

请求体（`Content-Type: application/json`，上限 1 MiB）：

| 字段 | 必填 | 说明 |
|---|---|---|
| `schema_version` | 是 | 当前只接受 `1` |
| `version` | 是 | 版本键，例如 `3.34.0`；按精确键匹配，不做字典序排序 |
| `comparison_version` | 否 | 对照版本，例如 `3.33.0`；不要求该版本也有站点报告 |
| `project_id` | 否 | 必须是**同一工作区**的 Project UUID；跨工作区会被 400 拒绝 |
| `title` | 是 | 非空，≤200 字符 |
| `metric_version` | 是 | 报告注明的指标/历史口径，例如 `historical-comment-inference/v1` |
| `observation_as_of` | 是 | RFC3339 **必须带时区**（`Z` 或 `±hh:mm`），例如 `2026-10-28T02:11:14Z` |
| `body_markdown` | 是 | 经批准的原始 Markdown，UTF-8，原样入库，不做任何改写 |
| `publish_key` | 是 | 幂等键，≤128 字符。同一键 + 相同内容只回放原记录；相同键 + 不同内容返回 409 |

响应：

- `201`：新建修订，返回 `id`、`revision`、`body_sha256`、`version`、`observation_as_of`、`published_at` 等元信息（不返回正文）。
- `200`：幂等重试命中，返回原记录（同一个 `id`/`revision`）。
- `400`：缺字段、空正文、无时区时间、非法 UUID、未知 `schema_version`、跨工作区 project。
- `401`：未登录 / 凭证失效。
- `403`：非 owner/admin。
- `409`：同一 `publish_key` 提交了不同内容。
- `413`：请求体超过 1 MiB。

并发：同一 `workspace + version` 的并发发布会串行分配 `revision`，不会出现重复修订或丢失最后一份。

## 3. 读取与核验

| 方法与路径 | 用途 |
|---|---|
| `GET /api/dev-reports` | 工作区内每个版本的最新修订列表；支持 `?version=`、`?limit=`（默认 20，最大 100）、`?offset=`。不含正文 |
| `GET /api/dev-reports/{id}` | 指定修订的完整元信息 + `body_markdown` |
| `GET /api/dev-reports/{id}/revisions` | 同一版本的全部修订（新→旧），用于历史修订阅读 |
| `GET /api/dev-reports/{id}/markdown` | 下载存储的 Markdown 原文（`text/markdown`），正文只来自数据库 |

非法 UUID → 400；不存在或其他工作区的报告 → 404（不泄漏内容）。

### 公开只读接口

报告页面无需登录、无需加入工作区即可阅读，读路径因此有公开版本（仅 GET，发布仍必须鉴权）：

| 方法与路径 | 用途 |
|---|---|
| `GET /api/public/dev-reports?workspace_slug={slug}` | 公开列表 |
| `GET /api/public/dev-reports/{id}?workspace_slug={slug}` | 公开详情 |
| `GET /api/public/dev-reports/{id}/revisions?workspace_slug={slug}` | 公开修订历史 |
| `GET /api/public/dev-reports/{id}/markdown?workspace_slug={slug}` | 公开下载 |

公开接口不带工作区标识时返回 400，不会退化成"跨工作区全量列表"。

发布后建议：用 `GET /api/dev-reports/{id}` 取回正文，与本地文件逐字节比对 `body_sha256`，再构造阅读地址。

## 4. 站点地址

- 列表 + 默认最新阅读：`https://tmultica.woa.com/{workspaceSlug}/dev-report`（**公开，无需登录**）
- 指定修订稳定阅读：`https://tmultica.woa.com/{workspaceSlug}/dev-report/{reportId}`（**公开，无需登录**）

详情地址只读指定 `reportId`，新发布不会改变旧链接的内容。公开页使用独立版式（无工作区侧栏）；已登录成员会看到"返回工作区"入口。

## 5. 发布纪律

- 只发布已审批成稿；不从早期预览、原始 ledger 或模型结果重新生成。
- 不修改正文中的措辞/数值来"修正"报告；有更正请发布新修订并保留旧修订。
- 发布不会改动 issue、comment、Gate、质量结果或 `.codebuddy` 数据。
- 站点不提供删除/撤回 API；误发布通过新修订更正。
