# 内容与评论插件钩子

插件通过 `Manager.RegisterHook` 注册钩子。钩子只在所属插件启用时执行；返回错误会中止前置操作，完成类钩子的错误会返回给调用方。

## 保存生命周期

| 常量 | Payload | 调用时机 |
|---|---|---|
| `HookContentBeforeSave` | `ContentSavePayload` | 创建、更新、手动保存草稿和自动保存写入数据库前，可修改 `Input` |
| `HookContentAfterSave` | `ContentSavePayload` | 任意内容保存完成后 |
| `HookContentAfterDraftSave` | `ContentSavePayload` | 草稿或自动保存完成后 |
| `HookContentAfterPublish` | `ContentSavePayload` | 发布操作完成后，包括贡献者提交待审核内容 |

`ContentSavePayload.Operation` 的稳定值为 `draft`、`autosave` 或 `publish`。编辑已发布内容时，`PublishedID` 是已发布记录 ID，`ID` 在完成钩子中是实际写入或发布后的记录 ID。

## 删除与状态

| 常量 | Payload | 调用时机 |
|---|---|---|
| `HookContentBeforeDelete` | `ContentDeletePayload` | 删除内容及处理附件之前，可返回错误阻止删除 |
| `HookContentAfterDelete` | `ContentDeletePayload` | 内容删除完成后 |
| `HookContentBeforeStatus` | `ContentStatusPayload` | 隐藏、公开、待审核等状态变更前，可修改 `Status` |
| `HookContentAfterStatus` | `ContentStatusPayload` | 状态变更完成后 |

## 查询与渲染

| 常量 | Payload | 用途 |
|---|---|---|
| `HookContentSearch` | `ContentSearchPayload` | `Stage=before` 时修改查询或设置 `Handled` 并提供结果；`Stage=after` 时过滤结果 |
| `HookContentFilter` | `ContentFilterPayload` | 内容对象进入主题前的通用过滤 |
| `HookContentTitle` | `ContentTitlePayload` | 标题输出过滤 |
| `HookContentBeforeRender` | `ContentRenderPayload` | 正文解析前过滤内容对象和原文 |
| `HookContentMarkdown` | `ContentParserPayload` | 设置 `Handled=true` 和 `HTML` 可替换 Markdown 解析器 |
| `HookContentAutoParagraph` | `ContentParserPayload` | 替换纯文本/AutoP 解析器 |
| `HookContentAfterRender` | `ContentRenderPayload` | 正文 HTML 生成后的过滤 |
| `HookExcerpt` | `ExcerptPayload` | 摘要生成后的过滤 |

搜索钩子的 `Query` 当前为 `services.ContentQuery`，`Results` 为 `[]models.Content`。未设置 `Handled` 时，CMS 继续执行默认 SQL LIKE 查询。

## 文章字段

插件可实现 `ContentFieldsProvider`，通过 `ContentFieldSchema() []FieldSchema` 声明文章或页面字段；也可使用 `HookContentFields` 动态增减字段。`FieldSchema.ForTypes` 可限制为 `post` 或 `page`。

设置 `FieldSchema.ReadOnly`，或通过 `HookContentFieldReadOnly` 返回 `ReadOnly=true`，可以保护字段。只读检查在服务端执行，伪造表单也无法覆盖或删除原值。

插件路由的 `Runtime` 还提供：

- `ContentByID`：读取单项公开内容结构。
- `IncrementIntField`：原子增加整数自定义字段，适合阅读计数等场景。

## 评论保存生命周期

所有评论入口，包括前台评论、后台回复/编辑、Pingback 和 Trackback，都会经过通用保存钩子。`CommentSavePayload.Operation` 的稳定值为 `comment`、`reply`、`edit`、`pingback` 或 `trackback`。

| 常量 | Payload | 调用时机 |
|---|---|---|
| `HookCommentBeforeSave` | `CommentSavePayload` | 任意评论写入前，可修改 `Input` 或返回错误阻止写入 |
| `HookCommentAfterSave` | `CommentSavePayload` | 写入完成后，`ID` 和 `Comment` 已填充 |
| `HookCommentBeforeReply` / `HookCommentAfterReply` | `CommentSavePayload` | 后台回复写入前/后 |
| `HookCommentBeforeEdit` / `HookCommentAfterEdit` | `CommentSavePayload` | 后台编辑写入前/后 |

`Input` 当前为 `services.SaveCommentInput`，`Comment` 当前为 `models.Comment`，`Content` 为评论所属的 `models.Content`。

## 评论管理生命周期

| 常量 | Payload | 调用时机 |
|---|---|---|
| `HookCommentBeforeMark` | `CommentActionPayload` | 审核状态变更前，可修改 `Status` 或返回错误阻止操作 |
| `HookCommentAfterMark` | `CommentActionPayload` | 审核状态和评论数同步完成后 |
| `HookCommentBeforeDelete` | `CommentActionPayload` | 删除前，可返回错误阻止操作 |
| `HookCommentAfterDelete` | `CommentActionPayload` | 删除及子评论重新挂接完成后 |

单项、批量操作和清空垃圾评论使用相同钩子，不存在绕过插件的后台入口。

## 评论查询与渲染

| 常量 | Payload | 用途 |
|---|---|---|
| `HookCommentFilter` | `CommentFilterPayload` | 评论进入后台列表或主题前过滤 `models.Comment` |
| `HookCommentBeforeRender` | `CommentRenderPayload` | 评论正文解析前修改 `Text` |
| `HookCommentMarkdown` | `CommentParserPayload` | 设置 `Handled=true` 和 `HTML` 接管 Markdown 解析 |
| `HookCommentAutoParagraph` | `CommentParserPayload` | 设置 `Handled=true` 和 `HTML` 接管普通文本解析 |
| `HookCommentAfterRender` | `CommentRenderPayload` | 评论 HTML 生成后过滤 |
| `HookCommentAvatar` | `CommentAvatarPayload` | 修改或清空头像 URL |

渲染钩子返回的 `template.HTML` 被视为受信任的插件输出。插件如果处理访客输入，必须自行完成转义或净化。
