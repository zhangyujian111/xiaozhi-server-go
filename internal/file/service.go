package file

import (
	"context"
	"io"
	"time"
)

// Service 文件管理服务接口。
//
// 职责：
//   - 文件上传（支持本地/S3/MinIO）
//   - 文件下载（含预签名 URL）
//   - 文件元数据管理
type Service interface {
	// Upload 上传文件。
	Upload(ctx context.Context, req UploadReq, reader io.Reader) (*File, error)

	// Get 获取文件元数据。
	Get(ctx context.Context, fileID string) (*File, error)

	// Download 下载文件内容。
	Download(ctx context.Context, fileID string) (io.ReadCloser, error)

	// GetURL 获取文件访问 URL（预签名 URL）。
	GetURL(ctx context.Context, fileID string, expire time.Duration) (string, error)

	// Delete 删除文件。
	Delete(ctx context.Context, fileID string) error

	// List 按条件查询文件列表。
	List(ctx context.Context, filter FileFilter) (*FilePage, error)

	// BatchDelete 批量删除文件。
	BatchDelete(ctx context.Context, fileIDs []string) error
}