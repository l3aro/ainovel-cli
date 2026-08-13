package host

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/voocel/agentcore"
	"github.com/voocel/ainovel-cli/internal/domain"
	"github.com/voocel/ainovel-cli/internal/host/flow"
	"github.com/voocel/ainovel-cli/internal/store"
)

// blockingModel là ChatModel chặn vĩnh viễn: giữ vòng lặp coordinator "đang
// chạy" để StartPrepared trả về ngay sau khi khởi tạo progress mà không kích
// hoạt waitDone (không gọi LLM, không cần usage/notifier).
type blockingModel struct{}

func (blockingModel) Generate(ctx context.Context, _ []agentcore.Message, _ []agentcore.ToolSpec, _ ...agentcore.CallOption) (*agentcore.LLMResponse, error) {
	<-ctx.Done()
	return nil, ctx.Err()
}

func (blockingModel) GenerateStream(ctx context.Context, _ []agentcore.Message, _ []agentcore.ToolSpec, _ ...agentcore.CallOption) (<-chan agentcore.StreamEvent, error) {
	ch := make(chan agentcore.StreamEvent)
	go func() {
		<-ctx.Done()
		close(ch)
	}()
	return ch, nil
}

func (blockingModel) SupportsTools() bool { return false }

// newStartTestHost tạo Host tối giản đủ để chạy StartPrepared: store thật trên
// thư mục tạm, coordinator thật với mô hình chặn (không phát sinh lỗi bất đồng
// bộ), observer/router gắn như New.
func newStartTestHost(t *testing.T, dir string) *Host {
	t.Helper()
	coordinator := agentcore.NewAgent(agentcore.WithModel(blockingModel{}))
	s := store.NewStore(dir)
	if err := s.Init(); err != nil {
		t.Fatalf("init store: %v", err)
	}
	h := &Host{
		lifecycle:   lifecycleIdle,
		store:       s,
		events:      make(chan Event, 16),
		streamCh:    make(chan string, 256),
		coordinator: coordinator,
		done:        make(chan struct{}, 4),
	}
	h.observer = newObserver(coordinator, s, h.emitEvent, h.emitDelta, h.emitClear)
	h.router = flow.NewDispatcher(coordinator, s)
	return h
}

// TestStartPrepared_PreservesNovelName: sách cũ đã có tên phải giữ nguyên tên
// khi StartPrepared khởi tạo lại progress (kho tiểu thuyết), phase về init,
// tổng chương về 0.
func TestStartPrepared_PreservesNovelName(t *testing.T) {
	h := newStartTestHost(t, t.TempDir())
	if err := h.store.Progress.Init("Mắt Biếc", 3); err != nil {
		t.Fatal(err)
	}
	if err := h.StartPrepared("viết truyện mới"); err != nil {
		t.Fatalf("StartPrepared: %v", err)
	}
	p, err := h.store.Progress.Load()
	if err != nil {
		t.Fatal(err)
	}
	if p == nil {
		t.Fatal("progress phải tồn tại sau StartPrepared")
	}
	if p.NovelName != "Mắt Biếc" {
		t.Errorf("tên sách phải được giữ nguyên, nhận %q", p.NovelName)
	}
	if p.Phase != domain.PhaseInit || p.TotalChapters != 0 || len(p.CompletedChapters) != 0 {
		t.Errorf("tiến trình phải được khởi tạo lại (init/0 chương), nhận %+v", p)
	}
}

// TestStartPrepared_FreshStoreNoName: store chưa có progress → StartPrepared
// vẫn chạy, tên rỗng.
func TestStartPrepared_FreshStoreNoName(t *testing.T) {
	h := newStartTestHost(t, t.TempDir())
	if err := h.StartPrepared("viết truyện mới"); err != nil {
		t.Fatalf("StartPrepared: %v", err)
	}
	p, err := h.store.Progress.Load()
	if err != nil {
		t.Fatal(err)
	}
	if p == nil || p.NovelName != "" {
		t.Errorf("store mới phải có progress tên rỗng, nhận %+v", p)
	}
}

// TestStartPrepared_PropagatesProgressLoadError: progress hỏng → lỗi đọc phải
// được truyền lên (không phải "không tồn tại") TRƯỚC mọi reset/ghi: cả
// progress.json lẫn meta/checkpoints.jsonl đều không được đụng tới.
func TestStartPrepared_PropagatesProgressLoadError(t *testing.T) {
	dir := t.TempDir()
	s := store.NewStore(dir)
	if err := s.Init(); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, "meta", "progress.json")
	content := []byte("{hỏng")
	if err := os.WriteFile(path, content, 0o644); err != nil {
		t.Fatal(err)
	}
	// Checkpoint có sẵn: Reset sẽ xóa file này, nên nó là bằng chứng việc load
	// phải xảy ra trước mọi mutation.
	ckPath := filepath.Join(dir, "meta", "checkpoints.jsonl")
	ckContent := []byte(`{"step":1}` + "\n")
	if err := os.WriteFile(ckPath, ckContent, 0o644); err != nil {
		t.Fatal(err)
	}

	h := newStartTestHost(t, dir)
	err := h.StartPrepared("viết truyện mới")
	if err == nil {
		t.Fatal("progress hỏng phải làm StartPrepared trả lỗi")
	}
	if !strings.Contains(err.Error(), "load progress") {
		t.Errorf("lỗi phải xuất phát từ bước load progress, nhận: %v", err)
	}
	got, rerr := os.ReadFile(path)
	if rerr != nil {
		t.Fatal(rerr)
	}
	if string(got) != string(content) {
		t.Error("progress hỏng không được bị ghi đè")
	}
	ck, cerr := os.ReadFile(ckPath)
	if cerr != nil {
		t.Fatalf("checkpoints phải còn nguyên (Reset không được chạy): %v", cerr)
	}
	if string(ck) != string(ckContent) {
		t.Error("checkpoints không được bị xóa/sửa khi progress hỏng")
	}
}
