// Package file 文件上传管理服务。
//
// 职责：文件上传、下载、预签名 URL 生成、存储后端抽象。
// 实现：本地文件系统存储（./tmp/uploads/），0600 权限。
package file

import (
	"errors"
	"time"
)

// File 文件记录。
type File struct {
	ID          string    `json:"id"`          // 文件唯一标识
	Filename    string    `json:"filename"`    // 原始文件名
	Size        int64     `json:"size"`        // 文件大小（字节）
	MimeType    string    `json:"mimeType"`    // MIME 类型
	StorageKey  string    `json:"storageKey"`  // 存储 key（本地路径 / S3 object key）
	StorageType string    `json:"storageType"` // 存储类型：local / s3 / minio
	MD5         string    `json:"md5"`         // MD5 校验和
	SHA256      string    `json:"sha256"`      // SHA-256 校验和
	URL         string    `json:"url"`         // 访问 URL（预签名或本地路径）
	UploadedBy  string    `json:"uploadedBy"`  // 上传者（用户 ID / 设备 ID）
	TenantID    string    `json:"tenantId"`    // 租户 ID
	UploadedAt  time.Time `json:"uploadedAt"`  // 上传时间
}

// UploadReq 文件上传请求。
type UploadReq struct {
	Filename   string `json:"filename"`   // 原始文件名（必填）
	MimeType   string `json:"mimeType"`   // MIME 类型
	UploadedBy string `json:"uploadedBy"` // 上传者
	TenantID   string `json:"tenantId"`   // 租户 ID
	Category   string `json:"category"`   // 分类：avatar / firmware / document / audio / image
	PublicRead bool   `json:"publicRead"` // 是否公开可读
}

// FileFilter 文件查询过滤条件。
type FileFilter struct {
	Category   string     `json:"category"`   // 分类过滤
	UploadedBy string     `json:"uploadedBy"` // 上传者过滤
	TenantID   string     `json:"tenantId"`   // 租户过滤
	Keyword    string     `json:"keyword"`    // 文件名搜索
	DateFrom   *time.Time `json:"dateFrom"`   // 上传时间起始
	DateTo     *time.Time `json:"dateTo"`     // 上传时间截止
	Page       int        `json:"page"`       // 页码
	PageSize   int        `json:"pageSize"`   // 每页条数
}

// FilePage 文件分页结果。
type FilePage struct {
	Items    []*File `json:"items"`
	Total    int64   `json:"total"`
	Page     int     `json:"page"`
	PageSize int     `json:"pageSize"`
	HasMore  bool    `json:"hasMore"`
}

// 哨兵错误。
var (
	ErrFileNotFound    = errors.New("file: file not found")
	ErrFileTooLarge    = errors.New("file: file exceeds size limit")
	ErrInvalidMimeType = errors.New("file: unsupported mime type")
)