package handlers

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"image"
	"image/color"
	"image/jpeg"
	"image/png"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"testing"

	"goblog/core/models"
	"goblog/core/plugin"
	"goblog/pkg/imageproc"
)

func handlerTestPNG(t *testing.T, width, height int) []byte {
	t.Helper()
	img := image.NewNRGBA(image.Rect(0, 0, width, height))
	for y := 0; y < height; y++ {
		for x := 0; x < width; x++ {
			img.SetNRGBA(x, y, color.NRGBA{R: uint8(x), G: uint8(y), B: 180, A: 255})
		}
	}
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

func TestImageUploadProcessingConvertsAttachmentToWebP(t *testing.T) {
	app, secret, adminID := newSecurityTestApp(t)
	app.UploadDir = t.TempDir()
	ctx := context.Background()
	if err := app.Options.Set(ctx, "upload_image_processing", imageproc.UploadWebPQuality); err != nil {
		t.Fatal(err)
	}
	if err := app.Options.Set(ctx, "upload_webp_quality", "74"); err != nil {
		t.Fatal(err)
	}
	postID := createPublishedPost(t, app, adminID, "processed-image")
	_, meta := uploadMedia(t, app, secret, adminID, postID, "cover.png", handlerTestPNG(t, 80, 48))

	if meta.Name != "cover.webp" || meta.Type != "webp" || meta.MIME != "image/webp" {
		t.Fatalf("converted attachment metadata = %#v", meta)
	}
	if meta.Width != 80 || meta.Height != 48 || !strings.HasSuffix(meta.Path, "cover.webp") {
		t.Fatalf("converted attachment dimensions/path = %#v", meta)
	}
	data, err := os.ReadFile(filepath.Join(app.UploadDir, filepath.FromSlash(meta.Path)))
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.HasPrefix(data, []byte("RIFF")) {
		t.Fatalf("converted attachment is not WebP")
	}
}

func TestAdminThumbnailUsesConfiguredFormatAndCachesResult(t *testing.T) {
	app, secret, adminID := newSecurityTestApp(t)
	app.UploadDir = t.TempDir()
	ctx := context.Background()
	if err := app.Options.Set(ctx, "thumbnail_format", imageproc.ThumbnailWebP); err != nil {
		t.Fatal(err)
	}
	if err := app.Options.Set(ctx, "thumbnail_quality", "78"); err != nil {
		t.Fatal(err)
	}
	postID := createPublishedPost(t, app, adminID, "thumbnail-image")
	_, meta := uploadMedia(t, app, secret, adminID, postID, "large.png", handlerTestPNG(t, 640, 360))

	req := httptest.NewRequest(http.MethodGet, adminThumbnailURL(meta.URL), nil)
	setSession(t, req, secret, adminID)
	rec := httptest.NewRecorder()
	app.Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("thumbnail status = %d, want 200: %s", rec.Code, rec.Body.String())
	}
	if got := rec.Header().Get("Content-Type"); got != "image/webp" {
		t.Fatalf("thumbnail content type = %q", got)
	}
	cfg, _, err := image.DecodeConfig(bytes.NewReader(rec.Body.Bytes()))
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Width != 320 || cfg.Height != 180 {
		t.Fatalf("thumbnail size = %dx%d, want 320x180", cfg.Width, cfg.Height)
	}
	sourcePath := filepath.Join(app.UploadDir, filepath.FromSlash(meta.Path))
	cachePath := thumbnailCachePath(sourcePath, imageproc.ThumbnailWebP, 78)
	if _, err := os.Stat(cachePath); err != nil {
		t.Fatalf("thumbnail cache missing: %v", err)
	}
	if err := app.Options.Set(ctx, "thumbnail_format", imageproc.ThumbnailDisabled); err != nil {
		t.Fatal(err)
	}
	req = httptest.NewRequest(http.MethodGet, adminThumbnailURL(meta.URL), nil)
	setSession(t, req, secret, adminID)
	rec = httptest.NewRecorder()
	app.Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusTemporaryRedirect || rec.Header().Get("Location") != meta.URL {
		t.Fatalf("disabled thumbnail response = %d %q", rec.Code, rec.Header().Get("Location"))
	}
}

func TestImageProcessingFailureFallsBackToOriginal(t *testing.T) {
	app, _, _ := newSecurityTestApp(t)
	app.UploadDir = t.TempDir()
	ctx := context.Background()
	if err := app.Options.Set(ctx, "upload_image_processing", imageproc.UploadWebPQuality); err != nil {
		t.Fatal(err)
	}
	if err := app.Options.Set(ctx, "image_processing_memory_mb", "64"); err != nil {
		t.Fatal(err)
	}
	img := image.NewNRGBA(image.Rect(0, 0, 4000, 3000))
	var source bytes.Buffer
	if err := jpeg.Encode(&source, img, &jpeg.Options{Quality: 80}); err != nil {
		t.Fatal(err)
	}
	saved, err := app.saveUpload(ctx, bytes.NewReader(source.Bytes()), "over-budget.jpg", 0)
	if err != nil {
		t.Fatal(err)
	}
	if saved.Warning != imageProcessingFallbackWarning || saved.Meta.Type != "jpg" || !strings.HasSuffix(saved.Meta.Path, "over-budget.jpg") {
		t.Fatalf("fallback upload = %#v", saved)
	}
	data, err := os.ReadFile(filepath.Join(app.UploadDir, filepath.FromSlash(saved.Meta.Path)))
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(data, source.Bytes()) {
		t.Fatal("fallback upload did not preserve the original bytes")
	}
}

func TestImageProcessingSettingsRejectInvalidQuality(t *testing.T) {
	app, secret, adminID := newSecurityTestApp(t)
	form := url.Values{
		"_csrf":                   {adminToken(secret, adminID)},
		"upload_image_processing": {imageproc.UploadWebPQuality},
		"upload_webp_quality":     {"101"},
		"thumbnail_format":        {imageproc.ThumbnailJPEG},
	}
	req := httptest.NewRequest(http.MethodPost, "/admin/options/general", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	setSession(t, req, secret, adminID)
	rec := httptest.NewRecorder()
	app.Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), "WebP 质量必须是 1 到 100 的整数") {
		t.Fatalf("invalid quality response = %d: %s", rec.Code, rec.Body.String())
	}
}

func TestUploadExtensionComparisonUsesStoredFormat(t *testing.T) {
	if !sameUploadExtension("jpeg", "jpg") || !sameUploadExtension("webp", "webp") {
		t.Fatal("equivalent stored extensions should match")
	}
	if sameUploadExtension("png", "webp") {
		t.Fatal("different stored extensions should not match")
	}
}

func TestMediaPageUsesDirectUploadAndCompactTable(t *testing.T) {
	app, secret, adminID := newSecurityTestApp(t)
	app.UploadDir = t.TempDir()
	uploadMedia(t, app, secret, adminID, 0, "compact.png", tinyPNG(t))
	req := httptest.NewRequest(http.MethodGet, "/admin/medias?kind=all&author=all", nil)
	setSession(t, req, secret, adminID)
	rec := httptest.NewRecorder()
	app.Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("media page status = %d", rec.Code)
	}
	body := rec.Body.String()
	for _, want := range []string{"media-upload-form", "data-auto-submit", "media-table", "media-copy-button", "media-batch-form", "清理孤立附件", "上传附件", `name="kind" label="类型" value="all"`, `name="author" label="作者" value="all"`, `<mdui-menu-item value="all">全部</mdui-menu-item>`} {
		if !strings.Contains(body, want) {
			t.Fatalf("media page missing %q", want)
		}
	}
	if strings.Contains(body, "<th>地址</th>") || strings.Contains(body, "未选择文件") {
		t.Fatalf("media page still contains the old upload or address layout")
	}
}

func TestAttachmentEditPaginationAndClearUnattached(t *testing.T) {
	app, secret, adminID := newSecurityTestApp(t)
	app.UploadDir = t.TempDir()
	ctx := context.Background()
	attachment, _ := uploadMedia(t, app, secret, adminID, 0, "editable.png", tinyPNG(t))
	manager := plugin.NewManager()
	app.Plugins = manager
	var editEvents []string
	manager.RegisterHook(plugin.HookAttachmentBeforeEdit, func(ctx context.Context, value any) (any, error) {
		payload := value.(plugin.AttachmentEditPayload)
		payload.Title += " [plugin]"
		editEvents = append(editEvents, "before-edit")
		return payload, nil
	})
	manager.RegisterHook(plugin.HookAttachmentAfterEdit, func(ctx context.Context, value any) (any, error) {
		editEvents = append(editEvents, "after-edit")
		return value, nil
	})
	form := url.Values{
		"_csrf":       {adminToken(secret, adminID)},
		"title":       {"Edited cover"},
		"description": {"A reusable cover image"},
	}
	req := httptest.NewRequest(http.MethodPost, "/admin/medias/"+itoa(attachment.CID)+"/edit", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	setSession(t, req, secret, adminID)
	rec := httptest.NewRecorder()
	app.Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusSeeOther {
		t.Fatalf("attachment edit status = %d: %s", rec.Code, rec.Body.String())
	}
	updated, err := app.Contents.ByID(ctx, attachment.CID)
	if err != nil {
		t.Fatal(err)
	}
	meta := parseAttachmentMeta(updated)
	if updated.Title != "Edited cover [plugin]" || meta.Description != "A reusable cover image" || strings.Join(editEvents, ",") != "before-edit,after-edit" {
		t.Fatalf("edited attachment = %#v meta=%#v", updated, meta)
	}
	postID := createPublishedPost(t, app, adminID, "media-clear-parent")
	attachedMeta := models.AttachmentMeta{Name: "attached.txt", URL: "https://cdn.example/attached.txt", Size: 10, Type: "txt", MIME: "text/plain"}
	attachedText, _ := json.Marshal(attachedMeta)
	attachedID, err := app.Contents.CreateAttachmentMeta(ctx, "Attached", "attached", string(attachedText), adminID, postID)
	if err != nil {
		t.Fatal(err)
	}

	for i := 0; i < 20; i++ {
		name := "page-item-" + strconv.Itoa(i) + ".txt"
		meta := models.AttachmentMeta{Name: name, URL: "https://cdn.example/" + name, Size: 10, Type: "txt", MIME: "text/plain"}
		text, _ := json.Marshal(meta)
		if _, err := app.Contents.CreateAttachmentMeta(ctx, name, strings.TrimSuffix(name, ".txt"), string(text), adminID, 0); err != nil {
			t.Fatal(err)
		}
	}
	req = httptest.NewRequest(http.MethodGet, "/admin/medias?page=2&kind=all&author=all", nil)
	setSession(t, req, secret, adminID)
	rec = httptest.NewRecorder()
	app.Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), "第 2 / 2 页，共 22 个") {
		t.Fatalf("media pagination status=%d body=%s", rec.Code, rec.Body.String())
	}

	form = url.Values{"_csrf": {adminToken(secret, adminID)}}
	req = httptest.NewRequest(http.MethodPost, "/admin/medias/clear-unattached", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	setSession(t, req, secret, adminID)
	rec = httptest.NewRecorder()
	app.Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusSeeOther {
		t.Fatalf("clear unattached status = %d: %s", rec.Code, rec.Body.String())
	}
	if _, err := app.Contents.ByID(ctx, attachment.CID); !errors.Is(err, sql.ErrNoRows) {
		t.Fatalf("unattached record was not deleted: %v", err)
	}
	if _, err := app.Contents.ByID(ctx, attachedID); err != nil {
		t.Fatalf("attached record was incorrectly cleared: %v", err)
	}
	form = url.Values{"_csrf": {adminToken(secret, adminID)}, "action": {"delete"}, "id": {itoa(attachedID)}}
	req = httptest.NewRequest(http.MethodPost, "/admin/medias/batch", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	setSession(t, req, secret, adminID)
	rec = httptest.NewRecorder()
	app.Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusSeeOther {
		t.Fatalf("batch media delete status = %d: %s", rec.Code, rec.Body.String())
	}
	if _, err := app.Contents.ByID(ctx, attachedID); !errors.Is(err, sql.ErrNoRows) {
		t.Fatalf("batch attachment record remains: %v", err)
	}
}

func TestAttachmentStorageHooksCanTakeOverLifecycle(t *testing.T) {
	app, secret, adminID := newSecurityTestApp(t)
	app.UploadDir = t.TempDir()
	manager := plugin.NewManager()
	app.Plugins = manager
	ctx := context.Background()
	var events []string
	manager.RegisterHook(plugin.HookUploadBeforeSave, func(ctx context.Context, value any) (any, error) {
		events = append(events, "before-upload")
		return value, nil
	})
	manager.RegisterHook(plugin.HookUploadHandle, func(ctx context.Context, value any) (any, error) {
		payload := value.(plugin.UploadHandlePayload)
		reader, err := payload.Open()
		if err != nil {
			return value, err
		}
		data, err := io.ReadAll(reader)
		_ = reader.Close()
		if err != nil || !bytes.Equal(data, tinyPNG(t)) {
			t.Fatalf("upload handle data mismatch: %v", err)
		}
		payload.Handled = true
		payload.Meta = models.AttachmentMeta{Name: payload.Name, Path: "remote/object.png", URL: "https://storage.example/object.png", Size: int64(len(data)), Type: "png", MIME: "image/png", IsImage: true, Width: 1, Height: 1}
		events = append(events, "upload-handle")
		return payload, nil
	})
	manager.RegisterHook(plugin.HookUploadAfterSave, func(ctx context.Context, value any) (any, error) {
		events = append(events, "after-upload")
		return value, nil
	})
	manager.RegisterHook(plugin.HookAttachmentURL, func(ctx context.Context, value any) (any, error) {
		payload := value.(plugin.AttachmentURLPayload)
		payload.URL = "https://cdn.example/signed.png"
		return payload, nil
	})
	manager.RegisterHook(plugin.HookAttachmentData, func(ctx context.Context, value any) (any, error) {
		payload := value.(plugin.AttachmentDataPayload)
		payload.Data = tinyPNG(t)
		payload.Handled = true
		events = append(events, "data-handle")
		return payload, nil
	})
	manager.RegisterHook(plugin.HookAttachmentReplaceHandle, func(ctx context.Context, value any) (any, error) {
		payload := value.(plugin.AttachmentReplacePayload)
		payload.Handled = true
		payload.Meta = models.AttachmentMeta{Name: "replacement.png", Path: "remote/replacement.png", URL: "https://storage.example/replacement.png", Size: payload.Size, Type: "png", MIME: "image/png", IsImage: true}
		events = append(events, "replace-handle")
		return payload, nil
	})
	manager.RegisterHook(plugin.HookAttachmentBeforeReplace, func(ctx context.Context, value any) (any, error) {
		events = append(events, "before-replace")
		return value, nil
	})
	manager.RegisterHook(plugin.HookAttachmentAfterReplace, func(ctx context.Context, value any) (any, error) {
		events = append(events, "after-replace")
		return value, nil
	})
	manager.RegisterHook(plugin.HookAttachmentBeforeDelete, func(ctx context.Context, value any) (any, error) {
		events = append(events, "before-delete")
		return value, nil
	})
	manager.RegisterHook(plugin.HookAttachmentDeleteHandle, func(ctx context.Context, value any) (any, error) {
		payload := value.(plugin.AttachmentDeleteHandlePayload)
		payload.Handled = true
		events = append(events, "delete-handle")
		return payload, nil
	})
	manager.RegisterHook(plugin.HookAttachmentAfterDelete, func(ctx context.Context, value any) (any, error) {
		events = append(events, "after-delete")
		return value, nil
	})

	saved, err := app.saveUpload(ctx, bytes.NewReader(tinyPNG(t)), "remote.png", 0)
	if err != nil || saved.Meta.URL != "https://storage.example/object.png" {
		t.Fatalf("custom upload = %#v, err=%v", saved, err)
	}
	text, _ := json.Marshal(saved.Meta)
	id, err := app.Contents.CreateAttachmentMeta(ctx, "Remote", "remote", string(text), adminID, 0)
	if err != nil {
		t.Fatal(err)
	}
	item, _ := app.Contents.ByID(ctx, id)
	if got := app.attachmentMeta(ctx, item).URL; got != "https://cdn.example/signed.png" {
		t.Fatalf("attachment URL hook = %q", got)
	}
	req := httptest.NewRequest(http.MethodGet, adminThumbnailURLForAttachment(id, saved.Meta.URL), nil)
	setSession(t, req, secret, adminID)
	rec := httptest.NewRecorder()
	app.Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusOK || !strings.HasPrefix(rec.Header().Get("Content-Type"), "image/") {
		t.Fatalf("hooked thumbnail status=%d type=%q body=%s", rec.Code, rec.Header().Get("Content-Type"), rec.Body.String())
	}

	req = multipartUploadRequestBytes(t, "/admin/medias/"+itoa(id)+"/replace", map[string]string{"_csrf": adminToken(secret, adminID)}, "file", "replacement.png", tinyPNG(t))
	setSession(t, req, secret, adminID)
	rec = httptest.NewRecorder()
	app.Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusSeeOther {
		t.Fatalf("hooked replace status=%d body=%s", rec.Code, rec.Body.String())
	}
	item, _ = app.Contents.ByID(ctx, id)
	if got := parseAttachmentMeta(item).Path; got != "remote/replacement.png" {
		t.Fatalf("hooked replacement path = %q", got)
	}

	form := url.Values{"_csrf": {adminToken(secret, adminID)}}
	req = httptest.NewRequest(http.MethodPost, "/admin/medias/"+itoa(id)+"/delete", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	setSession(t, req, secret, adminID)
	rec = httptest.NewRecorder()
	app.Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusSeeOther {
		t.Fatalf("hooked delete status=%d body=%s", rec.Code, rec.Body.String())
	}
	if _, err := app.Contents.ByID(ctx, id); !errors.Is(err, sql.ErrNoRows) {
		t.Fatalf("hooked attachment record remains: %v", err)
	}
	for _, want := range []string{"before-upload", "upload-handle", "after-upload", "data-handle", "before-replace", "replace-handle", "after-replace", "before-delete", "delete-handle", "after-delete"} {
		if !slices.Contains(events, want) {
			t.Fatalf("missing lifecycle event %q in %#v", want, events)
		}
	}
}

func TestSettingsAssetCardsCopyRelativeURL(t *testing.T) {
	app, secret, adminID := newSecurityTestApp(t)
	app.UploadDir = t.TempDir()
	ctx := context.Background()
	adminAsset, err := app.saveAdminSettingUpload(ctx, bytes.NewReader(tinyPNG(t)), "admin-copy.png")
	if err != nil {
		t.Fatal(err)
	}
	themeAsset, err := app.saveThemeSettingUpload(ctx, bytes.NewReader(tinyPNG(t)), "theme-copy.png")
	if err != nil {
		t.Fatal(err)
	}
	for _, check := range []struct {
		path string
		url  string
	}{
		{path: "/admin/management", url: adminAsset.URL},
		{path: "/admin/themes/default/config", url: themeAsset.URL},
	} {
		req := httptest.NewRequest(http.MethodGet, check.path, nil)
		setSession(t, req, secret, adminID)
		rec := httptest.NewRecorder()
		app.Handler().ServeHTTP(rec, req)
		if rec.Code != http.StatusOK {
			t.Fatalf("GET %s status = %d", check.path, rec.Code)
		}
		body := rec.Body.String()
		if !strings.Contains(body, "copy-notice-button") || !strings.Contains(body, `data-copy="`+check.url+`"`) || !strings.Contains(body, "复制相对 URL") {
			t.Fatalf("GET %s missing relative URL copy action for %s", check.path, check.url)
		}
	}
}
