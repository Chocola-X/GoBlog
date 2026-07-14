# 内容插件钩子

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

