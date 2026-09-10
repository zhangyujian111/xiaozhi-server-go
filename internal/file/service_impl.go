package file

import (
	"context"
	"crypto/md5"
	"crypto/sha256"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
)

const (
	defaultUploadDir = "./tmp/uploads"
	defaultMaxSize   = 100 * 1024 * 1024 // 100MB
	filePerm         = 0600
	dirPerm          = 0700
)

// localFileService 基于本地文件系统的文件服务实现。
type localFileService struct {
	mu       sync.RWMutex
	files    map[string]*File // fileID → File
	uploadDir string
	maxSize  int64
}

// NewService 创建基于本地文件系统的文件服务。
func NewService() Service {
	svc := &localFileService{
		files:     make(map[string]*File),
		uploadDir: defaultUploadDir,
		maxSize:   defaultMaxSize,
	}
	// 确保上传目录存在
	_ = os.MkdirAll(svc.uploadDir, dirPerm)
	return svc
}

// NewServiceWithDir 创建指定目录的文件服务。
func NewServiceWithDir(uploadDir string) Service {
	svc := &localFileService{
		files:     make(map[string]*File),
		uploadDir: uploadDir,
		maxSize:   defaultMaxSize,
	}
	_ = os.MkdirAll(svc.uploadDir, dirPerm)
	return svc
}

// Upload 上传文件。
func (s *localFileService) Upload(ctx context.Context, req UploadReq, reader io.Reader) (*File, error) {
	if ctx.Err() != nil {
		return nil, ctx.Err()
	}
	if req.Filename == "" {
		return nil, fmt.Errorf("file: filename is required")
	}
	if reader == nil {
		return nil, fmt.Errorf("file: reader is nil")
	}

	// 生成文件 ID 和存储路径
	fileID := uuid.New().String()
	ext := filepath.Ext(req.Filename)
	storageKey := filepath.Join(s.uploadDir, fileID+ext)

	// 创建文件并写入（0600 权限）
	f, err := os.OpenFile(storageKey, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, filePerm)
	if err != nil {
		return nil, fmt.Errorf("file: create file: %w", err)
	}
	defer f.Close()

	// 计算校验和并写入
	md5h := md5.New()
	sha256h := sha256.New()
	writer := io.MultiWriter(f, md5h, sha256h)

	// 限制大小
	limitedReader := io.LimitReader(reader, s.maxSize+1)
	written, err := io.Copy(writer, limitedReader)
	if err != nil {
		os.Remove(storageKey)
		return nil, fmt.Errorf("file: write file: %w", err)
	}
	if written > s.maxSize {
		os.Remove(storageKey)
		return nil, ErrFileTooLarge
	}

	mimeType := req.MimeType
	if mimeType == "" {
		mimeType = "application/octet-stream"
	}

	file := &File{
		ID:          fileID,
		Filename:    req.Filename,
		Size:        written,
		MimeType:    mimeType,
		StorageKey:  storageKey,
		StorageType: "local",
		MD5:         fmt.Sprintf("%x", md5h.Sum(nil)),
		SHA256:      fmt.Sprintf("%x", sha256h.Sum(nil)),
		URL:         storageKey,
		UploadedBy:  req.UploadedBy,
		TenantID:    req.TenantID,
		UploadedAt:  time.Now(),
	}

	s.mu.Lock()
	s.files[fileID] = file
	s.mu.Unlock()

	return file, nil
}

// Get 获取文件元数据。
func (s *localFileService) Get(ctx context.Context, fileID string) (*File, error) {
	if ctx.Err() != nil {
		return nil, ctx.Err()
	}

	s.mu.RLock()
	defer s.mu.RUnlock()

	f, ok := s.files[fileID]
	if !ok {
		return nil, fmt.Errorf("%w: %s", ErrFileNotFound, fileID)
	}
	return f, nil
}

// Download 下载文件内容。
func (s *localFileService) Download(ctx context.Context, fileID string) (io.ReadCloser, error) {
	if ctx.Err() != nil {
		return nil, ctx.Err()
	}

	s.mu.RLock()
	f, ok := s.files[fileID]
	s.mu.RUnlock()
	if !ok {
		return nil, fmt.Errorf("%w: %s", ErrFileNotFound, fileID)
	}

	reader, err := os.Open(f.StorageKey)
	if err != nil {
		return nil, fmt.Errorf("file: open file: %w", err)
	}
	return reader, nil
}

// GetURL 获取文件访问 URL。
func (s *localFileService) GetURL(ctx context.Context, fileID string, expire time.Duration) (string, error) {
	if ctx.Err() != nil {
		return "", ctx.Err()
	}

	s.mu.RLock()
	f, ok := s.files[fileID]
	s.mu.RUnlock()
	if !ok {
		return "", fmt.Errorf("%w: %s", ErrFileNotFound, fileID)
	}

	// 本地存储直接返回文件路径
	absPath, err := filepath.Abs(f.StorageKey)
	if err != nil {
		return f.StorageKey, nil
	}
	return "file://" + absPath, nil
}

// Delete 删除文件。
func (s *localFileService) Delete(ctx context.Context, fileID string) error {
	if ctx.Err() != nil {
		return ctx.Err()
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	f, ok := s.files[fileID]
	if !ok {
		return fmt.Errorf("%w: %s", ErrFileNotFound, fileID)
	}

	if err := os.Remove(f.StorageKey); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("file: remove file: %w", err)
	}

	delete(s.files, fileID)
	return nil
}

// List 按条件查询文件列表。
func (s *localFileService) List(ctx context.Context, filter FileFilter) (*FilePage, error) {
	if ctx.Err() != nil {
		return nil, ctx.Err()
	}

	s.mu.RLock()
	defer s.mu.RUnlock()

	var matched []*File
	for _, f := range s.files {
		if filter.Category != "" {
			// category 存储在 UploadReq 中但未持久化到 File，跳过此过滤
		}
		if filter.UploadedBy != "" && f.UploadedBy != filter.UploadedBy {
			continue
		}
		if filter.TenantID != "" && f.TenantID != filter.TenantID {
			continue
		}
		if filter.Keyword != "" && !strings.Contains(strings.ToLower(f.Filename), strings.ToLower(filter.Keyword)) {
			continue
		}
		if filter.DateFrom != nil && f.UploadedAt.Before(*filter.DateFrom) {
			continue
		}
		if filter.DateTo != nil && f.UploadedAt.After(*filter.DateTo) {
			continue
		}
		matched = append(matched, f)
	}

	total := int64(len(matched))
	page := filter.Page
	if page < 1 {
		page = 1
	}
	pageSize := filter.PageSize
	if pageSize <= 0 || pageSize > 100 {
		pageSize = 20
	}

	start := (page - 1) * pageSize
	if start >= len(matched) {
		return &FilePage{
			Items:    []*File{},
			Total:    total,
			Page:     page,
			PageSize: pageSize,
			HasMore:  false,
		}, nil
	}

	end := start + pageSize
	if end > len(matched) {
		end = len(matched)
	}

	return &FilePage{
		Items:    matched[start:end],
		Total:    total,
		Page:     page,
		PageSize: pageSize,
		HasMore:  end < len(matched),
	}, nil
}

// BatchDelete 批量删除文件。
func (s *localFileService) BatchDelete(ctx context.Context, fileIDs []string) error {
	if ctx.Err() != nil {
		return ctx.Err()
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	for _, fileID := range fileIDs {
		f, ok := s.files[fileID]
		if !ok {
			continue
		}
		os.Remove(f.StorageKey)
		delete(s.files, fileID)
	}
	return nil
}