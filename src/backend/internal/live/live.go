// Package live HEVC 兼容播放的实时转码会话管理（Job000131 P1 方案）。
//
// 触发语义：仅当浏览器检测不支持源编码（HEVC）且用户在播放页显式点「兼容播放」
// 时启动——与「关闭自动 HLS 转码」的语义兼容（不自动转码，人工按需转码兜底）。
// 输出为 ffmpeg 直写的动态 HLS（hls_list_size=0，分片累积 + 结束自动 ENDLIST），
// 清单与分片文件原样服务，自研逻辑只负责会话生命周期。
//
// 护栏（调研报告 P1 节）：720p/1800k 单档 ultrafast（CPU 开销下限）、
// 全局并发上限（默认 1，防低配机 CPU 饱和）、会话空闲 10min 自动回收、
// seek = 前端销毁重建 session（旧 session 标记淘汰）。
package live

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sync"
	"time"
)

var (
	// ErrBusy 并发上限已达（另一路兼容播放进行中）。
	ErrBusy = errors.New("live: 已有实时转码任务进行中，请稍后重试")
	// ErrSourceGone 源文件缺失（与 Download 的 FILE_MISSING 语义一致）。
	ErrSourceGone = errors.New("live: 源文件不在磁盘上")
	// ErrManifestTimeout ffmpeg 未及时产出首份清单（启动异常）。
	ErrManifestTimeout = errors.New("live: 转码启动超时")
)

// Session 单次实时转码会话。
type Session struct {
	MediaID    string
	Dir        string   // 会话目录（分片与 ffmpeg 清单落在其中）
	Abs        string   // 源文件绝对路径
	StartPos   float64  // 起始秒（seek 重建时 >0）
	cmd        *exec.Cmd
	done       chan struct{}
	closeOnce  sync.Once
	mu         sync.Mutex
	ended      bool // ffmpeg 已退出（清单应含 ENDLIST）
	lastAccess time.Time
	startedAt  time.Time
}

// Manager 会话注册表 + 并发上限 + 空闲清扫。
type Manager struct {
	mu        sync.Mutex
	sessions  map[string]*Session // key = mediaID（同视频单会话，重建即替换）
	maxConc   int
	idleAfter time.Duration
	bin       string
	root      string
	now       func() time.Time // 测试可注入
}

// Config 构造参数。
type Config struct {
	FFmpegBin    string // 空 = PATH 查找
	MaxConcurrent int   // <=0 = 1
	RootDir      string // 会话根目录；空 = os.TempDir()/panomint-live
	IdleTimeout  time.Duration
}

// NewManager 构造并启动空闲清扫协程（Stop 时退出）。
func NewManager(cfg Config) *Manager {
	bin := cfg.FFmpegBin
	if bin == "" {
		bin = "ffmpeg"
	}
	maxConc := cfg.MaxConcurrent
	if maxConc <= 0 {
		maxConc = 1
	}
	root := cfg.RootDir
	if root == "" {
		root = filepath.Join(os.TempDir(), "panomint-live")
	}
	idle := cfg.IdleTimeout
	if idle <= 0 {
		idle = 10 * time.Minute
	}
	m := &Manager{
		sessions:  map[string]*Session{},
		maxConc:   maxConc,
		idleAfter: idle,
		bin:       bin,
		root:      root,
		now:       time.Now,
	}
	return m
}

// Get 取现有未结束会话（分片请求用；不创建、不影响并发计数）。
func (m *Manager) Get(mediaID string) (*Session, bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	s, ok := m.sessions[mediaID]
	if !ok || s.isEnded() {
		return nil, false
	}
	s.touch()
	return s, true
}

// Ensure 取或建 mediaID 的会话。posSec>0 表示从该秒起播（seek 重建）。
// 已有未结束会话且 posSec 相同语义（0 表示从头）→ 复用；否则淘汰旧会话新建。
func (m *Manager) Ensure(mediaID, abs string, posSec float64) (*Session, error) {
	m.mu.Lock()
	if s, ok := m.sessions[mediaID]; ok && !s.isEnded() && samePos(s, posSec) {
		s.touch()
		m.mu.Unlock()
		return s, nil
	}
	// seek 重建（同 mediaID 不同起点）：先摘除旧会话腾位——替换是 1:1，不占并发额度；
	// 若按全局上限判，单视频 seek 会被自己尚未结束的旧会话挡成 ErrBusy（Linux 实测缺陷）。
	if s, ok := m.sessions[mediaID]; ok {
		delete(m.sessions, mediaID)
		s.close()
	}
	if len(m.sessions) >= m.maxConc {
		// 旧会话若已结束，先回收腾位
		m.reapLocked()
		if len(m.sessions) >= m.maxConc {
			m.mu.Unlock()
			return nil, ErrBusy
		}
	}
	m.mu.Unlock()
	return m.start(mediaID, abs, posSec)
}

func samePos(s *Session, pos float64) bool {
	// 会话起点与请求起点一致（容 1s）才复用；seek 一律重建
	return abs(s.StartPos-pos) < 1.0
}

func abs(f float64) float64 {
	if f < 0 {
		return -f
	}
	return f
}

func (m *Manager) start(mediaID, abs string, posSec float64) (*Session, error) {
	if _, err := os.Stat(abs); err != nil {
		return nil, ErrSourceGone
	}
	dir := filepath.Join(m.root, fmt.Sprintf("%s-%d", mediaID, m.now().UnixNano()))
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return nil, err
	}
	s := &Session{
		MediaID:    mediaID,
		Dir:        dir,
		Abs:        abs,
		StartPos:   posSec,
		done:       make(chan struct{}),
		lastAccess: m.now(),
		startedAt:  m.now(),
	}
	// Job000131 护栏：720p 单档 ultrafast——低配机 CPU 开销下限；
	// -ss 在 -i 前 = 快速定位（关键帧对齐，误差 <2s，前端进度条按请求值显示）。
	args := []string{
		"-hide_banner", "-loglevel", "error",
		"-ss", fmt.Sprintf("%.1f", posSec),
		"-i", abs,
		"-c:v", "libx264", "-preset", "ultrafast",
		"-b:v", "1800k", "-maxrate", "2200k", "-bufsize", "4400k",
		"-vf", "scale='min(1280,iw)':-2",
		"-c:a", "aac", "-b:a", "128k", "-ac", "2",
		"-f", "hls", "-hls_time", "2", "-hls_list_size", "0",
		"-hls_segment_filename", filepath.Join(dir, "seg_%05d.ts"),
		filepath.Join(dir, "stream.m3u8"),
	}
	s.cmd = exec.Command(m.bin, args...)
	if err := s.cmd.Start(); err != nil {
		os.RemoveAll(dir)
		return nil, fmt.Errorf("live: ffmpeg 启动失败: %w", err)
	}
	go func() {
		_ = s.cmd.Wait()
		s.mu.Lock()
		s.ended = true
		s.mu.Unlock()
		close(s.done)
	}()
	m.mu.Lock()
	// Ensure 已负责同 ID 替换腾位；此处兜底防并发路径竞态（幂等无害）
	if old, ok := m.sessions[mediaID]; ok {
		old.close()
	}
	m.sessions[mediaID] = s
	m.mu.Unlock()
	return s, nil
}

// ManifestPath 等待首份 ffmpeg 清单出现（启动期最多 8s），返回路径与是否已结束。
func (m *Manager) ManifestPath(s *Session) (string, bool, error) {
	p := filepath.Join(s.Dir, "stream.m3u8")
	deadline := m.now().Add(8 * time.Second)
	for {
		if _, err := os.Stat(p); err == nil {
			s.touch()
			return p, s.isEnded(), nil
		}
		if s.isEnded() {
			// 进程秒退（源损坏等）：清单可能从未写出
			return p, true, nil
		}
		if m.now().After(deadline) {
			return "", false, ErrManifestTimeout
		}
		time.Sleep(200 * time.Millisecond)
	}
}

// SegmentPath 校验并返回分片路径（防路径穿越：只允许 seg_XXXXX.ts 形态）。
// 清单引用可能先于分片落盘（ffmpeg HLS muxer 的 rename 时序），短暂等待消除竞态窗口，
// 超时才 404 —— 兼容 hls.js 的首撞重试语义，避免无谓的 fatal 重试风暴。
func (m *Manager) SegmentPath(s *Session, name string) (string, bool) {
	if !validSegName(name) {
		return "", false
	}
	p := filepath.Join(s.Dir, name)
	deadline := m.now().Add(3 * time.Second)
	for {
		if _, err := os.Stat(p); err == nil {
			s.touch()
			return p, true
		}
		if s.isEnded() || m.now().After(deadline) {
			return "", false
		}
		time.Sleep(150 * time.Millisecond)
	}
}

func validSegName(name string) bool {
	if len(name) != len("seg_00000.ts") {
		return false
	}
	if name[:4] != "seg_" || name[len(name)-3:] != ".ts" {
		return false
	}
	for _, ch := range name[4 : len(name)-3] {
		if ch < '0' || ch > '9' {
			return false
		}
	}
	return true
}

// ReapIdle 回收空闲超时/已结束的会话（Stop 与清扫协程共用）。
func (m *Manager) ReapIdle() {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.reapLocked()
}

func (m *Manager) reapLocked() {
	for id, s := range m.sessions {
		if s.isEnded() || m.now().Sub(s.lastAccess) > m.idleAfter {
			s.close()
			delete(m.sessions, id)
		}
	}
}

// Stop 关闭全部会话并删除目录（api 退出时调用）。
func (m *Manager) Stop() {
	m.mu.Lock()
	defer m.mu.Unlock()
	for id, s := range m.sessions {
		s.close()
		delete(m.sessions, id)
	}
	os.RemoveAll(m.root)
}

// StartReaper 启动空闲清扫协程，返回停止函数。
func (m *Manager) StartReaper(interval time.Duration) func() {
	stop := make(chan struct{})
	go func() {
		t := time.NewTicker(interval)
		defer t.Stop()
		for {
			select {
			case <-t.C:
				m.ReapIdle()
			case <-stop:
				return
			}
		}
	}()
	return func() { close(stop) }
}

func (s *Session) touch() {
	s.mu.Lock()
	s.lastAccess = time.Now()
	s.mu.Unlock()
}

func (s *Session) isEnded() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.ended
}

// close 终止 ffmpeg 并删除会话目录（幂等）。
func (s *Session) close() {
	s.closeOnce.Do(func() {
		if s.cmd != nil && s.cmd.Process != nil {
			_ = s.cmd.Process.Kill()
		}
		go func() {
			<-s.done // 等进程回收（最多秒级）
			os.RemoveAll(s.Dir)
		}()
	})
}
