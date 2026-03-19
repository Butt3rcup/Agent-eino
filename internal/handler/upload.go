package handler

import (
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
)

type UploadResponse struct {
	Message  string `json:"message"`
	Filename string `json:"filename"`
}

func (h *Handler) HandleUpload(c *gin.Context) {
	file, err := c.FormFile("file")
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "文件上传失败"})
		return
	}

	if file.Size > h.cfg.Upload.MaxSize {
		c.JSON(http.StatusBadRequest, gin.H{"error": "文件大小超过限制"})
		return
	}

	ext := strings.ToLower(filepath.Ext(file.Filename))
	if ext != ".md" && ext != ".markdown" && ext != ".pdf" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "仅支持 .md / .markdown / .pdf"})
		return
	}
	contentType, err := detectUploadedContentType(file)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "无法识别文件类型"})
		return
	}
	if !isAllowedUploadType(ext, contentType) {
		c.JSON(http.StatusBadRequest, gin.H{"error": "文件内容类型与扩展名不匹配"})
		return
	}

	if err := os.MkdirAll(h.cfg.Upload.Dir, 0o755); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "创建上传目录失败"})
		return
	}

	now := time.Now()
	safeName := sanitizeUploadFilename(file.Filename)
	filename := fmt.Sprintf("%d_%s", now.Unix(), safeName)
	savePath := filepath.Join(h.cfg.Upload.Dir, filename)
	if err := c.SaveUploadedFile(file, savePath); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "保存文件失败"})
		return
	}

	metadata := fmt.Sprintf("filename:%s,upload_time:%s", safeName, now.Format(time.RFC3339))
	if err := h.ragService.IndexFile(c.Request.Context(), savePath, metadata); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": fmt.Sprintf("索引文件失败: %v", err)})
		return
	}

	c.JSON(http.StatusOK, UploadResponse{
		Message:  "文件上传并入库成功",
		Filename: filename,
	})
}
