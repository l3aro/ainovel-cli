package store

import (
	"fmt"
	"os"
	"sync"

	"github.com/voocel/ainovel-cli/internal/domain"
)

// Store là gốc kết hợp của quản lý trạng thái, giữ tất cả các store con.
type Store struct {
	dir string

	Progress    *ProgressStore
	Outline     *OutlineStore
	Drafts      *DraftStore
	Summaries   *SummaryStore
	RunMeta     *RunMetaStore
	Directives  *DirectivesStore
	Signals     *SignalStore
	Runtime     *RuntimeStore
	Characters  *CharacterStore
	Cast        *CastStore
	World       *WorldStore
	Checkpoints *CheckpointStore
	Sessions    *SessionStore
	Usage       *UsageStore
	Simulation  *SimulationStore
	StyleStats  *StyleStatsStore

	crossMu sync.Mutex // bảo vệ các thao tác nguyên tử liên miền

	// ios là tập hợp các instance IO của mọi store con (có thể lặp: Outline và
	// Checkpoints dùng chung một instance), để FlushLogs đẩy toàn bộ log bộ nhớ đệm.
	ios []*IO
}

// NewStore tạo bộ quản lý trạng thái, dir là thư mục gốc đầu ra của tiểu thuyết.
func NewStore(dir string) *Store {
	io := newIO(dir)
	outline := NewOutlineStore(io)
	s := &Store{
		dir:         dir,
		Progress:    NewProgressStore(newIO(dir)),
		Outline:     outline,
		Drafts:      NewDraftStore(newIO(dir)),
		Summaries:   NewSummaryStore(newIO(dir), outline),
		RunMeta:     NewRunMetaStore(newIO(dir)),
		Directives:  NewDirectivesStore(newIO(dir)),
		Signals:     NewSignalStore(newIO(dir)),
		Runtime:     NewRuntimeStore(newIO(dir)),
		Characters:  NewCharacterStore(newIO(dir), outline),
		Cast:        NewCastStore(newIO(dir)),
		World:       NewWorldStore(newIO(dir)),
		Checkpoints: NewCheckpointStore(io),
		Sessions:    NewSessionStore(newIO(dir)),
		Usage:       NewUsageStore(newIO(dir)),
		Simulation:  NewSimulationStore(newIO(dir)),
		StyleStats:  NewStyleStatsStore(newIO(dir)),
	}
	s.ios = []*IO{
		io,
		s.Progress.io, s.Outline.io, s.Drafts.io, s.Summaries.io, s.RunMeta.io,
		s.Directives.io, s.Signals.io, s.Runtime.io, s.Characters.io, s.Cast.io,
		s.World.io, s.Checkpoints.io, s.Sessions.io, s.Usage.io, s.Simulation.io,
		s.StyleStats.io,
	}
	return s
}

// FlushLogs đẩy toàn bộ dữ liệu ghi nối tiếp bộ nhớ đệm của mọi store con xuống
// kernel (chỉ buf.Flush, không fsync — log quan sát). Gọi ở các đường thoát chuẩn
// (Host.Close, headless Run) để log đầy đủ khi thoát bình thường.
func (s *Store) FlushLogs() error {
	var firstErr error
	for _, io := range s.ios {
		if err := io.FlushBuffered(); err != nil && firstErr == nil {
			firstErr = err
		}
	}
	return firstErr
}

// Dir trả về thư mục gốc đầu ra.
func (s *Store) Dir() string { return s.dir }

// CheckConsistency thực hiện một lần kiểm tra nông trên tầng dữ liệu, dùng để sinh cảnh báo khi khởi động/phục hồi.
// Hoàn toàn chỉ đọc: không sửa dữ liệu, chỉ trả về mô tả vấn đề có thể đọc được. Bên gọi quyết định cách hiển thị (log / UI).
// Để tránh chi phí IO khi quét toàn bộ thư mục, chỉ kiểm tra các điểm then chốt của Progress:
//   - Chương hoàn thành cuối cùng phải có bản thảo hoàn chỉnh trong chapters/
//   - Ở chế độ Layered, Volume/Arc hiện tại phải tìm được trong layered_outline
func (s *Store) CheckConsistency() []string {
	var warnings []string
	progress, err := s.Progress.Load()
	if err != nil || progress == nil {
		return warnings
	}
	if n := len(progress.CompletedChapters); n > 0 {
		lastCh := progress.CompletedChapters[n-1]
		if text, err := s.Drafts.LoadChapterText(lastCh); err == nil && text == "" {
			warnings = append(warnings, fmt.Sprintf("progress đánh dấu chương %d đã hoàn thành, nhưng chapters/%02d.md không tồn tại hoặc rỗng", lastCh, lastCh))
		}
	}
	if progress.Layered && progress.CurrentVolume > 0 && progress.CurrentArc > 0 {
		volumes, err := s.Outline.LoadLayeredOutline()
		if err == nil && len(volumes) > 0 {
			found := false
			for _, v := range volumes {
				if v.Index != progress.CurrentVolume {
					continue
				}
				for _, a := range v.Arcs {
					if a.Index == progress.CurrentArc {
						found = true
						break
					}
				}
				break
			}
			if !found {
				warnings = append(warnings, fmt.Sprintf("progress hiện tại V%d A%d không tìm thấy mục tương ứng trong đề cương phân lớp", progress.CurrentVolume, progress.CurrentArc))
			}
		}
	}
	return warnings
}

// FoundationMissing trả về các mục còn thiếu trong cài đặt nền tảng, theo thứ tự ổn định dùng cho Prompt/Reminder.
// Chế độ dài tập (đã có layered_outline) yêu cầu thêm compass.
func (s *Store) FoundationMissing() []string {
	var missing []string
	if p, _ := s.Outline.LoadPremise(); p == "" {
		missing = append(missing, "premise")
	}
	if o, _ := s.Outline.LoadOutline(); len(o) == 0 {
		missing = append(missing, "outline")
	}
	if c, _ := s.Characters.Load(); len(c) == 0 {
		missing = append(missing, "characters")
	}
	if r, _ := s.World.LoadWorldRules(); len(r) == 0 {
		missing = append(missing, "world_rules")
	}
	if layered, _ := s.Outline.LoadLayeredOutline(); len(layered) > 0 {
		if c, _ := s.Outline.LoadCompass(); c == nil {
			missing = append(missing, "compass")
		}
	}
	return missing
}

// Init tạo cấu trúc thư mục con cần thiết.
func (s *Store) Init() error {
	return s.Progress.io.EnsureDirs([]string{
		"chapters", "summaries", "drafts", "reviews", "meta", "meta/runtime", "meta/runtime/tasks", "meta/sessions", "meta/sessions/agents",
	})
}

// ── Phương thức điều phối liên miền ──

// ExpandArc mở rộng cung truyện khung thành các chương chi tiết (Outline + Progress liên động).
func (s *Store) ExpandArc(volumeIdx, arcIdx int, chapters []domain.OutlineEntry) error {
	s.crossMu.Lock()
	defer s.crossMu.Unlock()

	s.Outline.io.mu.Lock()
	defer s.Outline.io.mu.Unlock()

	volumes, err := s.Outline.expandArcUnlocked(volumeIdx, arcIdx, chapters)
	if err != nil {
		return err
	}

	s.Progress.io.mu.Lock()
	defer s.Progress.io.mu.Unlock()

	p, err := s.Progress.loadUnlocked()
	if err != nil {
		return err
	}
	if p == nil {
		p = &domain.Progress{}
	}
	p.TotalChapters = domain.TotalChapters(volumes)
	return s.Progress.saveUnlocked(p)
}

// AppendVolume thêm tập mới vào cuối đề cương phân lớp (Outline + Progress liên động).
func (s *Store) AppendVolume(vol domain.VolumeOutline) error {
	s.crossMu.Lock()
	defer s.crossMu.Unlock()

	s.Outline.io.mu.Lock()
	defer s.Outline.io.mu.Unlock()

	volumes, err := s.Outline.appendVolumeUnlocked(vol)
	if err != nil {
		return err
	}

	s.Progress.io.mu.Lock()
	defer s.Progress.io.mu.Unlock()

	p, err := s.Progress.loadUnlocked()
	if err != nil {
		return err
	}
	if p == nil {
		p = &domain.Progress{}
	}
	p.TotalChapters = domain.TotalChapters(volumes)
	return s.Progress.saveUnlocked(p)
}

// ClearHandledSteer xóa PendingSteer theo cách nguyên tử và đặt lại trạng thái FlowSteering
// (RunMeta + Progress liên động).
func (s *Store) ClearHandledSteer() error {
	s.crossMu.Lock()
	defer s.crossMu.Unlock()

	s.RunMeta.io.mu.Lock()
	defer s.RunMeta.io.mu.Unlock()

	meta, err := s.RunMeta.loadUnlocked()
	if err != nil && !os.IsNotExist(err) {
		return err
	}
	if meta != nil && meta.PendingSteer != "" {
		meta.PendingSteer = ""
		if err := s.RunMeta.saveUnlocked(*meta); err != nil {
			return err
		}
	}

	s.Progress.io.mu.Lock()
	defer s.Progress.io.mu.Unlock()

	p, err := s.Progress.loadUnlocked()
	if err != nil && !os.IsNotExist(err) {
		return err
	}
	if p != nil && p.Flow == domain.FlowSteering {
		if err := domain.ValidateFlowTransition(p.Flow, domain.FlowWriting); err != nil {
			return err
		}
		p.Flow = domain.FlowWriting
		if err := s.Progress.saveUnlocked(p); err != nil {
			return err
		}
	}
	return nil
}
