// Package music 音乐播放控制服务。
//
// 职责：音乐搜索、播放控制（播放/暂停/恢复/停止）、播放列表管理。
// 实现：内存 mock 5 首测试曲目，播放状态按设备维度管理。
package music

import (
	"errors"
	"time"
)

// Track 曲目信息。
type Track struct {
	ID       string        `json:"id"`       // 曲目 ID
	Title    string        `json:"title"`    // 歌曲名
	Artist   string        `json:"artist"`   // 艺术家
	Album    string        `json:"album"`    // 专辑名
	CoverURL string        `json:"coverUrl"` // 封面图片 URL
	URL      string        `json:"url"`      // 音频流 URL
	Duration time.Duration `json:"duration"` // 时长
	Format   string        `json:"format"`   // 音频格式（mp3/opus/aac）
	Source   string        `json:"source"`   // 来源（local/neteasemusic/qqmusic）
	LyricURL string        `json:"lyricUrl"` // 歌词 URL
}

// SearchFilter 搜索过滤条件。
type SearchFilter struct {
	Type   string `json:"type"`   // 类型：song / album / artist / playlist
	Source string `json:"source"` // 来源：all / local / neteasemusic / qqmusic
	Page   int    `json:"page"`   // 页码
	Limit  int    `json:"limit"`  // 每页条数（最大 50）
}

// SearchResult 搜索结果。
type SearchResult struct {
	Items   []*Track `json:"items"`   // 曲目列表
	Total   int64    `json:"total"`   // 总结果数
	Page    int      `json:"page"`    // 当前页码
	Limit   int      `json:"limit"`   // 每页条数
	HasMore bool     `json:"hasMore"` // 是否有更多
	Source  string   `json:"source"`  // 实际搜索来源
}

// PlaybackState 播放状态。
type PlaybackState struct {
	DeviceID  string        `json:"deviceId"`  // 设备 ID
	Track     *Track        `json:"track"`     // 当前曲目（null 表示无播放）
	Status    string        `json:"status"`    // 播放状态：playing / paused / stopped
	Position  time.Duration `json:"position"`  // 当前播放位置
	Volume    int           `json:"volume"`    // 音量（0-100）
	UpdatedAt time.Time     `json:"updatedAt"` // 状态更新时间
}

// Playlist 播放列表。
type Playlist struct {
	DeviceID  string    `json:"deviceId"`  // 设备 ID
	Tracks    []*Track  `json:"tracks"`    // 曲目列表
	Current   int       `json:"current"`   // 当前播放索引（-1 表示无）
	UpdatedAt time.Time `json:"updatedAt"` // 更新时间
}

// 哨兵错误。
var (
	ErrTrackNotFound  = errors.New("music: track not found")
	ErrNoActiveDevice = errors.New("music: no active playback device")
	ErrPlaylistEmpty  = errors.New("music: playlist is empty")
)