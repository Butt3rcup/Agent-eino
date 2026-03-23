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

type UploadAcceptedResponse struct {
	Message   string `json:"message"`
	Filename  string `json:"filename"`
	TaskID    string `json:"task_id"`
	State     string `json:"state"`
	StatusURL string `json:"status_url"`
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
	if ext == ".pdf" && !h.cfg.Upload.PDFEnabled {
		c.JSON(http.StatusBadRequest, gin.H{"error": "PDF 上传当前已禁用，请先启用 PDF_UPLOAD_ENABLED"})
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
	task, err := h.uploadTasks.Enqueue(filename, savePath, metadata)
	if err != nil {
		_ = os.Remove(savePath)
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "上传任务队列繁忙，请稍后重试"})
		return
	}
	task.StatusURL = fmt.Sprintf("/api/upload/%s", task.TaskID)

	c.JSON(http.StatusAccepted, UploadAcceptedResponse{
		Message:   "文件已接收，正在后台入库",
		Filename:  filename,
		TaskID:    task.TaskID,
		State:     string(task.State),
		StatusURL: task.StatusURL,
	})
}

func (h *Handler) HandleUploadStatus(c *gin.Context) {
	taskID := strings.TrimSpace(c.Param("taskID"))
	if taskID == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "task_id 不能为空"})
		return
	}

	task, ok := h.uploadTasks.Get(taskID)
	if !ok {
		c.JSON(http.StatusNotFound, gin.H{"error": "上传任务不存在"})
		return
	}
	task.StatusURL = fmt.Sprintf("/api/upload/%s", task.TaskID)
	c.JSON(http.StatusOK, task)
}
