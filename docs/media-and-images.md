# 附件与图片处理

## 附件记录

附件是 `gb_contents` 中 `type=attachment` 的记录。其正文保存 `models.AttachmentMeta` JSON：

```go
type AttachmentMeta struct {
    Name        string `json:"name"`
    Path        string `json:"path"`
    URL         string `json:"url"`
    Size        int64  `json:"size"`
    Type        string `json:"type"`
    MIME        string `json:"mime"`
    Description string `json:"description,omitempty"`
    IsImage     bool   `json:"isImage"`
    Width       int    `json:"width"`
    Height      int    `json:"height"`
}
```

`Parent` 是所属文章或页面的 `cid`。附件标题、Slug 和描述可在后台编辑；描述位于元数据 JSON，不额外占用数据库列。

## 本地目录策略

默认附件文件系统根目录为 `data/uploads`，即 `<GOPHERINK_DATA_DIR>/uploads`；可用 `GOPHERINK_UPLOAD_DIR` 单独覆盖。核心把这个目录挂载为浏览器可访问的 `/uploads/` URL 前缀。

| 附件用途 | 默认文件系统目录 | 公开 URL 前缀 |
|---|---|---|
| 未绑定附件 | `data/uploads/unattached/` | `/uploads/unattached/` |
| 文章 `<cid>` 的附件 | `data/uploads/posts/<cid>/` | `/uploads/posts/<cid>/` |
| 页面 `<cid>` 的附件 | `data/uploads/pages/<cid>/` | `/uploads/pages/<cid>/` |
| 后台个性化素材 | `data/uploads/admin-settings/` | `/uploads/admin-settings/` |
| 默认主题设置素材 | `data/uploads/theme-settings/` | `/uploads/theme-settings/` |

新建内容编辑页上传的附件可先作为当前草稿附件管理。把未绑定附件绑定到指定文章/页面时，处理器会将本地文件移动到对应目录，并同步更新 `Path`、`URL` 和 `Parent`。

主题和插件不应通过字符串猜测绝对磁盘路径，应优先读取 `AttachmentMeta`，并使用附件 URL/数据钩子兼容远程存储。

## 内容删除后的附件

`attachment_delete_policy` 决定删除文章或页面时如何处理其附件：

- `keep`：保留附件记录和文件，将附件解绑并迁移整个目录。
- `record`：删除附件数据库记录，保留物理文件。
- `file`：删除附件记录和物理文件。

在 `keep` 模式下：

```text
data/uploads/posts/6/  -> data/uploads/unattached/6-post/
data/uploads/pages/6/  -> data/uploads/unattached/6-pages/
```

迁出后附件 `Parent` 变为 `0`。目标目录冲突时，实际处理逻辑会选择可用路径，调用方不应假设目录名永远未经调整。

## 上传流程

附件上传的主要步骤是：

1. 解析 multipart 文件并检查配置的最大字节数。
2. 校验文件名、危险扩展名、允许扩展名和 MIME 一致性。
3. 保存到临时文件，构造可重复打开的同步读取函数。
4. 触发 `upload.before_save`。
5. 图片按配置尝试转换；失败时保留原始文件并返回警告。
6. 触发可接管存储的 `upload.handle`；未接管时写入本地目录。
7. 触发 `upload.after_save`，创建附件记录。

上传默认允许：

```text
jpg,jpeg,png,gif,webp,svg,pdf,txt,md,zip
```

实际允许列表和最大大小由后台配置。默认上限 16 MB。即使存储插件接管写入，核心仍会先执行大小、扩展名及 MIME 安全校验。

## 图片上传处理

`pkg/imageproc` 提供三种模式：

| 模式值 | 行为 |
|---|---|
| `original` | 不转换，保存原文件 |
| `webp_lossless` | 转换为无损 WebP |
| `webp_quality` | 按 1 到 100 的质量转换 WebP |

默认 WebP 质量为 85。SVG 保持原文件；动画 GIF 使用动画 WebP 转换路径。图片处理成功后会更新扩展名、MIME、宽高和大小。

### 超大图片保护

图片处理前会检查：

- WebP 编码允许的尺寸边界，当前按最大边 16383 像素保护。
- 估算解码/处理内存，当前按约 8 字节/像素估算。
- 后台配置的处理预算，默认 256 MB，最小 64 MB。

预算是预检查阈值，不是 Go Runtime 的严格内存配额。图片超出尺寸或预算、解码失败或编码失败时，上传不会整体失败：核心回退保存原始文件，并通过后台通知说明转换未完成。

## 缩略图

后台附件列表和素材管理使用缩略图减少传输：

- 格式可选 JPG、WebP 或禁用。
- 默认 JPG，默认质量 82。
- 缩放使用 Catmull-Rom。
- 输出 JPG 时透明区域铺白色背景。
- 禁用或缩略图处理失败时返回原图。
- 远程存储附件可通过 `attachment.data` 提供源文件字节。

禁用缩略图会减少服务器处理，但后台预览可能直接传输大图，应结合带宽和附件下载 WAF 策略评估。

## 替换附件

附件替换会重新执行大小、扩展名、MIME 和图片转换校验。启用“仅允许相同扩展名替换”时，处理后的最终扩展名也必须与旧附件兼容。

本地存储且扩展名相同时会尽量保留原 `Path` 和 URL，使正文里已经插入的链接继续有效。远程存储插件可通过 `attachment.replace_handle` 接管实际替换并返回完整新元数据。

## 删除附件

删除流程先触发 `attachment.before_delete`，随后调用 `attachment.delete_handle`：

- 插件设置 `Handled=true` 时负责删除远程对象。
- 未接管时核心删除本地文件。
- 数据库记录删除后触发 `attachment.after_delete`。

插件不能只处理上传而忽略删除，否则远程对象会长期残留。

## 后台和主题素材

后台个性化与默认主题设置使用彼此独立目录，并提供素材列表、预览、相对 URL 复制、删除和“一键清理未使用素材”。

清理操作根据当前保存的设置值判断引用关系；在插件或自定义模板中手工引用这些目录的文件不会自动进入核心引用集合。此类额外引用应避免使用一键清理，或由扩展自行维护。

## 编辑器附件选择

文章和页面编辑器采用分层浏览：

- 当前内容上传的附件直接显示。
- 已发布内容附件可按文章/页面 ID 和标题选择。
- 未绑定附件独立筛选。
- 默认“不选择”，避免一次加载全部历史文件。

图片点击后插入 Markdown 图片语法，其他文件插入普通链接；插入位置使用编辑框当前选区，而不是固定追加到正文末尾。附件卡片还可以复制绝对 URL 或相对 URL。

默认主题通过 URL 规范化函数兼容站内相对 URL。第三方主题输出用户配置的媒体地址时也应接受 `/uploads/...`，不要强制要求 `http://` 或 `https://`。

## 存储插件约束

对象存储插件接管 `upload.handle` 时至少返回：

- `Name`
- `URL`
- `Size`
- `Type`
- `MIME`

远程对象键可保存在 `Path`。`UploadHandlePayload.Open` 和替换 payload 的 `Open` 只允许在当前同步钩子调用期间使用，不得保存到异步任务后再打开。

远程存储通常还应实现：

- `attachment.replace_handle`
- `attachment.delete_handle`
- `attachment.url`（签名 URL/CDN 重写）
- `attachment.data`（后台缩略图需要读取原始数据时）

完整 payload 和代码示例见 [插件与钩子开发](plugins-and-hooks.md)。
