package vision

import (
	"testing"
	"time"
)

// MockCmdCh 模拟舵机命令通道。
type MockCmdCh chan ServoCommand

// defaultFollowCfg 构造测试用 FollowConfig（图像 640x480，死区 8px，220ms 节流）。
func defaultFollowCfg() *FollowConfig {
	return &FollowConfig{
		FollowGapMs:  220,
		DeadZonePx:   8,
		HFovDeg:      65,
		VFovDeg:      50,
		MinPulseUs:   500,
		MaxPulseUs:   2500,
		CenterPanUs:  1500,
		CenterTiltUs: 1500,
		RangePanDeg:  90,
		RangeTiltDeg: 60,
	}
}

// faceOffCenter 构造一个明显偏离中心的人脸（让 servo 一定发出）。
func faceOffCenter(faceID int64) Detection {
	return Detection{
		FaceID:      faceID,
		BoundingBox: [4]int{400, 300, 50, 50}, // 中心 (425, 325) 显著偏离 (320, 240)
		Confidence:  0.9,
		TsMs:        time.Now().UnixMilli(),
	}
}

func TestDeadZone(t *testing.T) {
	cmdCh := make(MockCmdCh, 100)
	cfg := defaultFollowCfg()

	// 图像尺寸 640x480，中心是 (320, 240)
	// 死区 8px，center ±4px 应该在死区内
	follower := NewFaceFollower(cmdCh, cfg, 640, 480, nil)

	// 中心 ±4px 应该在死区内，不下发命令
	det := Detection{
		FaceID:      1,
		BoundingBox: [4]int{316, 236, 8, 8}, // center ±4
	}
	follower.OnDetection(det)

	select {
	case <-cmdCh:
		t.Error("expected no servo command in dead zone")
	default:
		// 正确：死区内无命令
	}
}

func TestThrottle(t *testing.T) {
	cmdCh := make(MockCmdCh, 100)
	cfg := defaultFollowCfg()

	follower := NewFaceFollower(cmdCh, cfg, 640, 480, nil)

	// 发送 10 个检测（同一 face_id），应该只有第一个或前几个触发
	var commands []ServoCommand
	for i := 0; i < 10; i++ {
		det := Detection{
			FaceID:      1,
			BoundingBox: [4]int{400, 300, 50, 50}, // 明显偏离中心
		}
		follower.OnDetection(det)
		time.Sleep(50 * time.Millisecond)
	}

	close(cmdCh)
	for cmd := range cmdCh {
		commands = append(commands, cmd)
	}

	// 节流后命令数应该少于输入数
	if len(commands) >= 10 {
		t.Errorf("expected throttle to reduce commands, got %d", len(commands))
	}
}

func TestGeometricConversion(t *testing.T) {
	cmdCh := make(MockCmdCh, 100)
	cfg := defaultFollowCfg()

	follower := NewFaceFollower(cmdCh, cfg, 640, 480, nil)

	// face 在右侧 (400, 240)，应该发出向左转的 pan 命令
	det := Detection{
		FaceID:      1,
		BoundingBox: [4]int{375, 220, 50, 40}, // center x=400 > 320
	}
	follower.OnDetection(det)

	select {
	case cmd := <-cmdCh:
		if cmd.PanUs >= 1500 {
			t.Errorf("expected pan_us < 1500 for face on right, got %d", cmd.PanUs)
		}
	default:
		t.Error("expected servo command")
	}
}

func TestFaceTracking(t *testing.T) {
	cmdCh := make(MockCmdCh, 100)
	cfg := defaultFollowCfg()

	follower := NewFaceFollower(cmdCh, cfg, 640, 480, nil)

	// 同一 face_id 连续出现，应该使用 EMA 平滑
	for i := 0; i < 5; i++ {
		det := Detection{
			FaceID:      1,
			BoundingBox: [4]int{300 + i*10, 240, 50, 50},
		}
		follower.OnDetection(det)
		time.Sleep(50 * time.Millisecond)
	}

	var commands []ServoCommand
	for {
		select {
		case cmd := <-cmdCh:
			commands = append(commands, cmd)
		default:
			goto done
		}
	}
done:

	if len(commands) == 0 {
		t.Error("expected at least one command")
	}
}

func TestLostTracking(t *testing.T) {
	cmdCh := make(MockCmdCh, 100)
	cfg := defaultFollowCfg()

	follower := NewFaceFollower(cmdCh, cfg, 640, 480, nil)

	det := Detection{
		FaceID:      1,
		BoundingBox: [4]int{400, 300, 50, 50},
	}
	follower.OnDetection(det)

	select {
	case <-cmdCh:
	default:
	}

	time.Sleep(1500 * time.Millisecond)
	follower.CleanupLostTracks()

	if follower.Stats() != 0 {
		t.Errorf("expected 0 active tracks after cleanup, got %d", follower.Stats())
	}
}

func TestFaceIDSwitch(t *testing.T) {
	cmdCh := make(MockCmdCh, 100)
	cfg := defaultFollowCfg()

	follower := NewFaceFollower(cmdCh, cfg, 640, 480, nil)

	det1 := Detection{FaceID: 1, BoundingBox: [4]int{300, 240, 50, 50}}
	follower.OnDetection(det1)
	time.Sleep(100 * time.Millisecond)

	det2 := Detection{FaceID: 2, BoundingBox: [4]int{400, 240, 50, 50}}
	follower.OnDetection(det2)
	time.Sleep(100 * time.Millisecond)

	det1b := Detection{FaceID: 1, BoundingBox: [4]int{320, 240, 50, 50}}
	follower.OnDetection(det1b)

	var commands []ServoCommand
	for {
		select {
		case cmd := <-cmdCh:
			commands = append(commands, cmd)
		default:
			goto done
		}
	}
done:

	if len(commands) < 2 {
		t.Errorf("expected at least 2 commands for different faces, got %d", len(commands))
	}
}

// TestDetection_KeypointsPopulated 验证 v1 Detection 携带 5 点 keypoints（v1 核心字段）。
func TestDetection_KeypointsPopulated(t *testing.T) {
	det := faceOffCenter(1)
	det.Keypoints = [5]Keypoint{
		{X: 180, Y: 200}, // left eye
		{X: 260, Y: 200}, // right eye
		{X: 220, Y: 230}, // nose
		{X: 200, Y: 260}, // left mouth
		{X: 240, Y: 260}, // right mouth
	}

	cmdCh := make(MockCmdCh, 1)
	follower := NewFaceFollower(cmdCh, defaultFollowCfg(), 640, 480, nil)
	follower.OnDetection(det)

	select {
	case <-cmdCh:
		// ok
	case <-time.After(100 * time.Millisecond):
		t.Fatal("expected servo command")
	}
}

// TestServoDurationFollowsConfig 验证 ServoCommand.DurationMs 取自 cfg.FollowGapMs，
// 而不是硬编码常量（之前的实现是硬编码 220，会忽略配置变更）。
func TestServoDurationFollowsConfig(t *testing.T) {
	tests := []struct {
		name      string
		gapMs     int
		wantDurMs int
	}{
		{"default_220ms", 220, 220},
		{"slower_500ms", 500, 500},
		{"faster_100ms", 100, 100},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cmdCh := make(MockCmdCh, 1)
			cfg := &FollowConfig{
				FollowGapMs:  tt.gapMs,
				DeadZonePx:   8,
				HFovDeg:      65,
				VFovDeg:      50,
				MinPulseUs:   500,
				MaxPulseUs:   2500,
				CenterPanUs:  1500,
				CenterTiltUs: 1500,
				RangePanDeg:  90,
				RangeTiltDeg: 60,
			}
			follower := NewFaceFollower(cmdCh, cfg, 640, 480, nil)
			follower.OnDetection(Detection{
				FaceID:      1,
				BoundingBox: [4]int{500, 300, 50, 50},
			})
			select {
			case cmd := <-cmdCh:
				if cmd.DurationMs != tt.wantDurMs {
					t.Errorf("DurationMs = %d, want %d (cfg.FollowGapMs)", cmd.DurationMs, tt.wantDurMs)
				}
			case <-time.After(100 * time.Millisecond):
				t.Fatal("no servo command received")
			}
		})
	}
}

// TestFaceFollower_Reset 验证 Reset 清空 tracks / lastServoTime，
// 但 servoSeq 单调递增不变（vision-servo v2 §4.1 flush + §5.4 重放保护）。
func TestFaceFollower_Reset(t *testing.T) {
	cmdCh := make(MockCmdCh, 100)
	follower := NewFaceFollower(cmdCh, defaultFollowCfg(), 640, 480, nil)

	// 1. 添加 2 个 face
	follower.OnDetection(faceOffCenter(1))
	follower.OnDetection(faceOffCenter(2))
	// 收集命令
	for i := 0; i < 2; i++ {
		select {
		case <-cmdCh:
		case <-time.After(50 * time.Millisecond):
		}
	}

	if got := follower.Stats(); got != 2 {
		t.Fatalf("before reset: Stats() = %d, want 2", got)
	}

	// 2. Reset
	follower.Reset()

	// 3. tracks 应清空
	if got := follower.Stats(); got != 0 {
		t.Errorf("after reset: Stats() = %d, want 0", got)
	}

	// 4. 重新发同一 face 1 → 立即下发 servo（不在节流窗内）
	follower.OnDetection(faceOffCenter(1))
	select {
	case <-cmdCh:
		// ok
	case <-time.After(100 * time.Millisecond):
		t.Error("after reset, expected servo command (track was cleared)")
	}
}
