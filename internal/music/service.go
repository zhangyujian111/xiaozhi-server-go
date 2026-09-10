package music

import "context"

// Service 音乐播放服务接口。
//
// 职责：
//   - 音乐搜索（曲库查询）
//   - 播放控制（播放/暂停/恢复/停止）
//   - 播放列表管理
type Service interface {
	// Search 搜索音乐。
	Search(ctx context.Context, query string, filter SearchFilter) (*SearchResult, error)

	// Get 获取曲目详情。
	Get(ctx context.Context, trackID string) (*Track, error)

	// Play 在指定设备上播放曲目。
	Play(ctx context.Context, deviceID, trackID string) error

	// Pause 暂停当前播放。
	Pause(ctx context.Context, deviceID string) error

	// Resume 恢复播放。
	Resume(ctx context.Context, deviceID string) error

	// Stop 停止播放。
	Stop(ctx context.Context, deviceID string) error

	// GetPlaybackState 获取当前播放状态。
	GetPlaybackState(ctx context.Context, deviceID string) (*PlaybackState, error)

	// GetPlaylist 获取设备的播放列表。
	GetPlaylist(ctx context.Context, deviceID string) (*Playlist, error)

	// AddToPlaylist 添加曲目到播放列表。
	AddToPlaylist(ctx context.Context, deviceID, trackID string) error

	// RemoveFromPlaylist 从播放列表移除曲目。
	RemoveFromPlaylist(ctx context.Context, deviceID, trackID string) error

	// ClearPlaylist 清空播放列表。
	ClearPlaylist(ctx context.Context, deviceID string) error
}