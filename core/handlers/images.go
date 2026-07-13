package handlers

import (
	"bytes"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"path"
	"path/filepath"
	"strconv"
	"strings"

	"goblog/pkg/imageproc"
)

const (
	adminThumbnailWidth  = 320
	adminThumbnailHeight = 200
)

func adminThumbnailURL(rawURL string) string {
	if !strings.HasPrefix(rawURL, "/uploads/") {
		return rawURL
	}
	ext := strings.ToLower(strings.TrimPrefix(path.Ext(rawURL), "."))
	if ext == "svg" {
		return rawURL
	}
	return "/admin/thumbnail?src=" + url.QueryEscape(rawURL)
}

func (a *App) adminThumbnail(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		methodNotAllowed(w, http.MethodGet)
		return
	}
	if !a.requireRole(w, r, "contributor") {
		return
	}

	sourcePath, err := a.secureUploadPath(r.URL.Query().Get("src"))
	if err != nil {
		http.Error(w, "invalid thumbnail source", http.StatusBadRequest)
		return
	}
	sourceInfo, err := os.Stat(sourcePath)
	if err != nil || !sourceInfo.Mode().IsRegular() {
		http.NotFound(w, r)
		return
	}

	rawFormat := a.option(r.Context(), "thumbnail_format", imageproc.ThumbnailJPEG)
	if rawFormat == imageproc.ThumbnailDisabled {
		http.Redirect(w, r, r.URL.Query().Get("src"), http.StatusTemporaryRedirect)
		return
	}
	format := imageproc.NormalizeThumbnailFormat(rawFormat)
	quality := imageproc.ClampQuality(optionInt(a.option(r.Context(), "thumbnail_quality", ""), 0), imageproc.DefaultThumbnailQuality)
	cachePath := thumbnailCachePath(sourcePath, format, quality)
	data, mimeType, err := cachedThumbnail(sourcePath, sourceInfo, cachePath, format, quality, a.imageProcessingMemoryLimit(r.Context()))
	if err != nil {
		http.Redirect(w, r, r.URL.Query().Get("src"), http.StatusTemporaryRedirect)
		return
	}

	w.Header().Set("Content-Type", mimeType)
	w.Header().Set("Cache-Control", "private, max-age=86400")
	http.ServeContent(w, r, filepath.Base(cachePath), sourceInfo.ModTime(), bytes.NewReader(data))
}

func (a *App) secureUploadPath(rawURL string) (string, error) {
	if !strings.HasPrefix(rawURL, "/uploads/") {
		return "", fmt.Errorf("not an upload URL")
	}
	rel := strings.TrimPrefix(path.Clean(strings.TrimPrefix(rawURL, "/uploads/")), "/")
	if rel == "" || rel == "." || rel == ".." || strings.HasPrefix(rel, "../") {
		return "", fmt.Errorf("invalid upload path")
	}
	root, err := filepath.Abs(a.UploadDir)
	if err != nil {
		return "", err
	}
	fullPath, err := filepath.Abs(filepath.Join(root, filepath.FromSlash(rel)))
	if err != nil {
		return "", err
	}
	resolved, err := filepath.EvalSymlinks(fullPath)
	if err != nil {
		return "", err
	}
	within, err := filepath.Rel(root, resolved)
	if err != nil || within == ".." || strings.HasPrefix(within, ".."+string(filepath.Separator)) {
		return "", fmt.Errorf("upload path escapes root")
	}
	return resolved, nil
}

func cachedThumbnail(sourcePath string, sourceInfo os.FileInfo, cachePath, format string, quality int, memoryLimitBytes int64) ([]byte, string, error) {
	mimeType := "image/jpeg"
	if format == imageproc.ThumbnailWebP {
		mimeType = "image/webp"
	}
	if cacheInfo, err := os.Stat(cachePath); err == nil && cacheInfo.Mode().IsRegular() && cacheInfo.ModTime().UnixNano() >= sourceInfo.ModTime().UnixNano() {
		data, err := os.ReadFile(cachePath)
		return data, mimeType, err
	}

	source, err := os.Open(sourcePath)
	if err != nil {
		return nil, "", err
	}
	defer source.Close()
	data, mimeType, err := imageproc.Thumbnail(source, format, quality, adminThumbnailWidth, adminThumbnailHeight, memoryLimitBytes)
	if err != nil {
		return nil, "", err
	}
	if err := writeThumbnailCache(cachePath, data); err != nil {
		return nil, "", err
	}
	removeOtherThumbnailCaches(sourcePath, cachePath)
	return data, mimeType, nil
}

func thumbnailCachePath(sourcePath, format string, quality int) string {
	ext := ".jpg"
	if format == imageproc.ThumbnailWebP {
		ext = ".webp"
	}
	name := filepath.Base(sourcePath) + ".thumb-" + strconv.Itoa(adminThumbnailWidth) + "x" + strconv.Itoa(adminThumbnailHeight) + "-q" + strconv.Itoa(quality) + ext
	return filepath.Join(filepath.Dir(sourcePath), ".thumbnails", name)
}

func writeThumbnailCache(target string, data []byte) error {
	if err := os.MkdirAll(filepath.Dir(target), 0755); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(filepath.Dir(target), ".thumbnail-*")
	if err != nil {
		return err
	}
	tmpName := tmp.Name()
	defer os.Remove(tmpName)
	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	if err := os.Rename(tmpName, target); err != nil {
		if _, statErr := os.Stat(target); statErr == nil {
			return nil
		}
		return err
	}
	return nil
}

func (a *App) removeImageThumbnails(sourcePath string) {
	dir := filepath.Join(filepath.Dir(sourcePath), ".thumbnails")
	entries, err := os.ReadDir(dir)
	if err != nil {
		return
	}
	prefix := filepath.Base(sourcePath) + ".thumb-"
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasPrefix(entry.Name(), prefix) {
			continue
		}
		_ = os.Remove(filepath.Join(dir, entry.Name()))
	}
	if entries, err = os.ReadDir(dir); err == nil && len(entries) == 0 {
		_ = os.Remove(dir)
	}
}

func removeOtherThumbnailCaches(sourcePath, keepPath string) {
	dir := filepath.Dir(keepPath)
	entries, err := os.ReadDir(dir)
	if err != nil {
		return
	}
	prefix := filepath.Base(sourcePath) + ".thumb-"
	for _, entry := range entries {
		candidate := filepath.Join(dir, entry.Name())
		if entry.IsDir() || candidate == keepPath || !strings.HasPrefix(entry.Name(), prefix) {
			continue
		}
		_ = os.Remove(candidate)
	}
}
