package music

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"time"
)

// memoryMusicService 基于内存的音乐服务实现。
type memoryMusicService struct {
	mu       sync.RWMutex
	tracks   map[string]*Track         // trackID → Track
	states   map[string]*PlaybackState // deviceID → PlaybackState
	playlists map[string]*Playlist     // deviceID → Playlist
}

// NewService 创建基于内存的音乐服务。
func NewService() Service {
	svc := &memoryMusicService{
		tracks:    make(map[string]*Track),
		states:    make(map[string]*PlaybackState),
		playlists: make(map[string]*Playlist),
	}
	svc.initMockTracks()
	return svc
}

// initMockTracks 初始化 5 首测试曲目。
func (s *memoryMusicService) initMockTracks() {
	mock := []*Track{
		{
			ID:       "track-001",
			Title:    "月光",
			Artist:   "周杰伦",
			Album:    "范特西",
			CoverURL: "https://img.example.com/moonlight.jpg",
			Duration: 3*time.Minute + 51*time.Second,
			Format:   "mp3",
			Source:   "local",
		},
		{
			ID:       "track-002",
			Title:    "晴天",
			Artist:   "周杰伦",
			Album:    "叶惠美",
			CoverURL: "https://img.example.com/sunny.jpg",
			Duration: 4*time.Minute + 29*time.Second,
			Format:   "mp3",
			Source:   "local",
		},
		{
			ID:       "track-003",
			Title:    "七里香",
			Artist:   "周杰伦",
			Album:    "七里香",
			CoverURL: "https://img.example.com/qilixiang.jpg",
			Duration: 4*time.Minute + 57*time.Second,
			Format:   "mp3",
			Source:   "local",
		},
		{
			ID:       "track-004",
			Title:    "稻香",
			Artist:   "周杰伦",
			Album:    "魔杰座",
			CoverURL: "https://img.example.com/rice.jpg",
			Duration: 3*time.Minute + 43*time.Second,
			Format:   "mp3",
			Source:   "local",
		},
		{
			ID:       "track-005",
			Title:    "夜曲",
			Artist:   "周杰伦",
			Album:    "十一月的萧邦",
			CoverURL: "https://img.example.com/nocturne.jpg",
			Duration: 3*time.Minute + 48*time.Second,
			Format:   "mp3",
			Source:   "local",
		},
	}
	for _, t := range mock {
		s.tracks[t.ID] = t
	}
}

// Search 搜索音乐。
func (s *memoryMusicService) Search(ctx context.Context, query string, filter SearchFilter) (*SearchResult, error) {
	if ctx.Err() != nil {
		return nil, ctx.Err()
	}

	s.mu.RLock()
	defer s.mu.RUnlock()

	query = strings.ToLower(query)
	var matched []*Track
	for _, t := range s.tracks {
		if query == "" ||
			strings.Contains(strings.ToLower(t.Title), query) ||
			strings.Contains(strings.ToLower(t.Artist), query) ||
			strings.Contains(strings.ToLower(t.Album), query) {
			if filter.Source != "" && filter.Source != "all" && t.Source != filter.Source {
				continue
			}
			matched = append(matched, t)
		}
	}

	total := int64(len(matched))
	page := filter.Page
	if page < 1 {
		page = 1
	}
	limit := filter.Limit
	if limit <= 0 || limit > 50 {
		limit = 20
	}

	start := (page - 1) * limit
	if start >= len(matched) {
		return &SearchResult{
			Items:   []*Track{},
			Total:   total,
			Page:    page,
			Limit:   limit,
			HasMore: false,
			Source:  "local",
		}, nil
	}

	end := start + limit
	if end > len(matched) {
		end = len(matched)
	}

	return &SearchResult{
		Items:   matched[start:end],
		Total:   total,
		Page:    page,
		Limit:   limit,
		HasMore: end < len(matched),
		Source:  "local",
	}, nil
}

// Get 获取曲目详情。
func (s *memoryMusicService) Get(ctx context.Context, trackID string) (*Track, error) {
	if ctx.Err() != nil {
		return nil, ctx.Err()
	}

	s.mu.RLock()
	defer s.mu.RUnlock()

	t, ok := s.tracks[trackID]
	if !ok {
		return nil, fmt.Errorf("%w: %s", ErrTrackNotFound, trackID)
	}
	return t, nil
}

// Play 在指定设备上播放曲目。
func (s *memoryMusicService) Play(ctx context.Context, deviceID, trackID string) error {
	if ctx.Err() != nil {
		return ctx.Err()
	}
	if deviceID == "" {
		return ErrNoActiveDevice
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	t, ok := s.tracks[trackID]
	if !ok {
		return fmt.Errorf("%w: %s", ErrTrackNotFound, trackID)
	}

	s.states[deviceID] = &PlaybackState{
		DeviceID:  deviceID,
		Track:     t,
		Status:    "playing",
		Position:  0,
		Volume:    80,
		UpdatedAt: time.Now(),
	}

	// 确保播放列表存在
	if _, ok := s.playlists[deviceID]; !ok {
		s.playlists[deviceID] = &Playlist{
			DeviceID:  deviceID,
			Tracks:    []*Track{},
			Current:   -1,
			UpdatedAt: time.Now(),
		}
	}

	return nil
}

// Pause 暂停当前播放。
func (s *memoryMusicService) Pause(ctx context.Context, deviceID string) error {
	if ctx.Err() != nil {
		return ctx.Err()
	}
	if deviceID == "" {
		return ErrNoActiveDevice
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	state, ok := s.states[deviceID]
	if !ok || state.Track == nil {
		return ErrNoActiveDevice
	}

	state.Status = "paused"
	state.UpdatedAt = time.Now()
	return nil
}

// Resume 恢复播放。
func (s *memoryMusicService) Resume(ctx context.Context, deviceID string) error {
	if ctx.Err() != nil {
		return ctx.Err()
	}
	if deviceID == "" {
		return ErrNoActiveDevice
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	state, ok := s.states[deviceID]
	if !ok || state.Track == nil {
		return ErrNoActiveDevice
	}

	state.Status = "playing"
	state.UpdatedAt = time.Now()
	return nil
}

// Stop 停止播放。
func (s *memoryMusicService) Stop(ctx context.Context, deviceID string) error {
	if ctx.Err() != nil {
		return ctx.Err()
	}
	if deviceID == "" {
		return ErrNoActiveDevice
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	state, ok := s.states[deviceID]
	if !ok || state.Track == nil {
		return ErrNoActiveDevice
	}

	state.Status = "stopped"
	state.Track = nil
	state.Position = 0
	state.UpdatedAt = time.Now()
	return nil
}

// GetPlaybackState 获取当前播放状态。
func (s *memoryMusicService) GetPlaybackState(ctx context.Context, deviceID string) (*PlaybackState, error) {
	if ctx.Err() != nil {
		return nil, ctx.Err()
	}

	s.mu.RLock()
	defer s.mu.RUnlock()

	state, ok := s.states[deviceID]
	if !ok {
		// 返回停止状态而非错误
		return &PlaybackState{
			DeviceID:  deviceID,
			Status:    "stopped",
			UpdatedAt: time.Now(),
		}, nil
	}
	return state, nil
}

// GetPlaylist 获取设备的播放列表。
func (s *memoryMusicService) GetPlaylist(ctx context.Context, deviceID string) (*Playlist, error) {
	if ctx.Err() != nil {
		return nil, ctx.Err()
	}

	s.mu.RLock()
	defer s.mu.RUnlock()

	pl, ok := s.playlists[deviceID]
	if !ok {
		return &Playlist{
			DeviceID:  deviceID,
			Tracks:    []*Track{},
			Current:   -1,
			UpdatedAt: time.Now(),
		}, nil
	}
	return pl, nil
}

// AddToPlaylist 添加曲目到播放列表。
func (s *memoryMusicService) AddToPlaylist(ctx context.Context, deviceID, trackID string) error {
	if ctx.Err() != nil {
		return ctx.Err()
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	t, ok := s.tracks[trackID]
	if !ok {
		return fmt.Errorf("%w: %s", ErrTrackNotFound, trackID)
	}

	pl, ok := s.playlists[deviceID]
	if !ok {
		pl = &Playlist{
			DeviceID:  deviceID,
			Tracks:    []*Track{},
			Current:   -1,
			UpdatedAt: time.Now(),
		}
		s.playlists[deviceID] = pl
	}

	pl.Tracks = append(pl.Tracks, t)
	pl.UpdatedAt = time.Now()
	return nil
}

// RemoveFromPlaylist 从播放列表移除曲目。
func (s *memoryMusicService) RemoveFromPlaylist(ctx context.Context, deviceID, trackID string) error {
	if ctx.Err() != nil {
		return ctx.Err()
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	pl, ok := s.playlists[deviceID]
	if !ok {
		return ErrPlaylistEmpty
	}

	for i, t := range pl.Tracks {
		if t.ID == trackID {
			pl.Tracks = append(pl.Tracks[:i], pl.Tracks[i+1:]...)
			if pl.Current >= len(pl.Tracks) {
				pl.Current = len(pl.Tracks) - 1
			}
			pl.UpdatedAt = time.Now()
			return nil
		}
	}
	return fmt.Errorf("%w: %s", ErrTrackNotFound, trackID)
}

// ClearPlaylist 清空播放列表。
func (s *memoryMusicService) ClearPlaylist(ctx context.Context, deviceID string) error {
	if ctx.Err() != nil {
		return ctx.Err()
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	s.playlists[deviceID] = &Playlist{
		DeviceID:  deviceID,
		Tracks:    []*Track{},
		Current:   -1,
		UpdatedAt: time.Now(),
	}
	return nil
}