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
- `Config`：读取插件的站点级配置。
- `PersonalConfig`：按用户 ID 读取插件个人配置；未被个人值覆盖的字段会回落到站点级配置。

插件实现 `PersonalConfigProvider` 并通过 `PersonalConfigSchema() []FieldSchema` 声明个人字段后，已登录用户可以在“个人设置”中维护自己的配置。该入口对 `visitor`、`subscriber` 等不具备内容管理权限的用户同样开放，但只能读写当前用户自己的配置。

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

## 附件存储

附件默认写入本地 `uploads` 目录。存储插件可以在处理钩子中设置 `Handled=true`，完全接管对象存储操作；未接管时继续执行内置文件系统实现。文件大小、危险扩展名和 MIME 一致性仍由核心校验，存储插件不能绕过这些安全边界。

| 常量 | Payload | 用途 |
|---|---|---|
| `HookUploadBeforeSave` | `UploadPayload` | 上传校验和处理前修改名称或父内容 ID |
| `HookUploadHandle` | `UploadHandlePayload` | 接管实际写入，返回完整 `models.AttachmentMeta` |
| `HookUploadAfterSave` | `UploadPayload` | 文件写入完成、附件记录创建前过滤元数据 |
| `HookAttachmentBeforeReplace` | `AttachmentReplacePayload` | 替换开始前检查或修改输入 |
| `HookAttachmentReplaceHandle` | `AttachmentReplacePayload` | 接管实际替换，返回新的附件元数据 |
| `HookAttachmentAfterReplace` | `AttachmentReplacePayload` | 替换完成、附件记录更新前过滤结果 |
| `HookAttachmentBeforeDelete` | `AttachmentPayload` | 附件记录删除前，可返回错误阻止删除 |
| `HookAttachmentDeleteHandle` | `AttachmentDeleteHandlePayload` | 接管实际文件删除 |
| `HookAttachmentAfterDelete` | `AttachmentPayload` | 附件记录和文件删除完成后 |
| `HookAttachmentURL` | `AttachmentURLPayload` | 动态生成公开 URL，适合签名 URL 或 CDN 域名 |
| `HookAttachmentData` | `AttachmentDataPayload` | 提供文件内容，供远程图片生成后台缩略图 |

`UploadHandlePayload.Open` 和 `AttachmentReplacePayload.Open` 返回本次上传暂存文件的新读取流。该函数及读取流只允许在当前同步钩子调用期间使用，插件不能保存后异步读取。处理钩子应至少返回 `Name`、`URL`、`Size`、`Type` 和 `MIME`；远程存储可将 `Path` 用作对象键。

## 附件信息

| 常量 | Payload | 用途 |
|---|---|---|
| `HookAttachmentBeforeEdit` | `AttachmentEditPayload` | 修改标题和描述前，可过滤或阻止保存 |
| `HookAttachmentAfterEdit` | `AttachmentEditPayload` | 附件信息保存后 |

附件描述保存在 `AttachmentMeta.Description` 中，不增加数据库专用列，现有附件 JSON、备份和导入流程保持兼容。本地同扩展名替换会保留原始 `Path` 和 URL，避免正文中已经插入的链接失效。
