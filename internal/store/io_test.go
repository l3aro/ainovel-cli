package store

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/voocel/ainovel-cli/internal/domain"
)

// readLines đọc nội dung file theo dòng (bỏ dòng trống).
func readLines(t *testing.T, path string) []string {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	lines := strings.Split(strings.TrimSuffix(string(data), "\n"), "\n")
	var out []string
	for _, l := range lines {
		if l != "" {
			out = append(out, l)
		}
	}
	return out
}

// TestIOAppendLineBuffered_FlushVisibility xác minh dòng nhỏ ghi qua AppendLineBuffered
// chưa xuất hiện trên đĩa cho tới khi FlushBuffered, và sau flush nội dung khớp.
func TestIOAppendLineBuffered_FlushVisibility(t *testing.T) {
	dir := t.TempDir()
	io := newIO(dir)
	rel := "meta/sessions/probe.jsonl"

	if err := io.AppendLineBuffered(rel, []byte("line-one\n")); err != nil {
		t.Fatalf("AppendLineBuffered: %v", err)
	}
	// Dòng nhỏ, chưa quá 200ms: phải còn nằm trong bộ nhớ đệm.
	if lines := readLines(t, filepath.Join(dir, rel)); len(lines) != 0 {
		t.Fatalf("dữ liệu lộ ra trước FlushBuffered: %v", lines)
	}

	if err := io.FlushBuffered(); err != nil {
		t.Fatalf("FlushBuffered: %v", err)
	}
	lines := readLines(t, filepath.Join(dir, rel))
	if len(lines) != 1 || lines[0] != "line-one" {
		t.Fatalf("sau flush got %v, want [line-one]", lines)
	}
}

// TestIOAppendLineBuffered_Accumulates xác minh nhiều dòng cùng đường dẫn gộp đúng
// thứ tự sau một lần flush, và flush lặp lại không sinh trùng lặp.
func TestIOAppendLineBuffered_Accumulates(t *testing.T) {
	dir := t.TempDir()
	io := newIO(dir)
	rel := "meta/sessions/probe.jsonl"

	for i, line := range []string{"first", "second", "third"} {
		if err := io.AppendLineBuffered(rel, []byte(line+"\n")); err != nil {
			t.Fatalf("AppendLineBuffered %d: %v", i, err)
		}
	}
	if err := io.FlushBuffered(); err != nil {
		t.Fatalf("FlushBuffered: %v", err)
	}
	// Flush lần hai (đường thoát chuẩn gọi lại) phải là no-op.
	if err := io.FlushBuffered(); err != nil {
		t.Fatalf("FlushBuffered lần hai: %v", err)
	}

	lines := readLines(t, filepath.Join(dir, rel))
	if len(lines) != 3 {
		t.Fatalf("got %d lines, want 3: %v", len(lines), lines)
	}
	for i, want := range []string{"first", "second", "third"} {
		if lines[i] != want {
			t.Fatalf("lines[%d] = %q, want %q", i, lines[i], want)
		}
	}
}

// TestIOAppendLineBuffered_SizeThresholdFlush xác minh flush tự động khi bộ đệm đạt
// ngưỡng 64KB: sau khi ghi vượt 64KB (chưa gọi FlushBuffered), toàn bộ bản ghi phải
// nằm trên đĩa, NGUYÊN VẸN — bộ đệm bufio cấp 128KB (2× ngưỡng) bảo đảm flush chỉ xảy
// ra ở ranh giới dòng cho mọi dòng ≤ 64KB, không bao giờ để lộ bản ghi cụt.
func TestIOAppendLineBuffered_SizeThresholdFlush(t *testing.T) {
	dir := t.TempDir()
	io := newIO(dir)
	rel := "meta/sessions/probe.jsonl"

	line := strings.Repeat("x", 20*1024) + "\n" // 20KB/dòng
	for range 4 {                               // tổng 80KB > 64KB
		if err := io.AppendLineBuffered(rel, []byte(line)); err != nil {
			t.Fatalf("AppendLineBuffered: %v", err)
		}
	}

	// Chưa hề gọi flush tường minh: 4 dòng phải đã có trên đĩa, nguyên vẹn, đúng thứ tự.
	lines := readLines(t, filepath.Join(dir, rel))
	if len(lines) != 4 {
		t.Fatalf("ngưỡng 64KB không kích hoạt flush: got %d lines", len(lines))
	}
	for i, l := range lines {
		if len(l) != 20*1024 {
			t.Fatalf("lines[%d] bị cắt giữa dòng: len=%d, want %d", i, len(l), 20*1024)
		}
	}
	if data, err := os.ReadFile(filepath.Join(dir, rel)); err != nil {
		t.Fatalf("read: %v", err)
	} else if len(data) != 4*len(line) {
		t.Fatalf("dung lượng file = %d, want %d (bản ghi cụt)", len(data), 4*len(line))
	}
}

// TestIOAppendLineBuffered_TimeThresholdFlush xác minh flush tự động khi đã quá
// 200ms kể từ lần flush trước: lần ghi kế tiếp kích hoạt kiểm tra lười và đẩy
// toàn bộ dữ liệu đang chờ (kể cả dòng vừa ghi) xuống đĩa.
func TestIOAppendLineBuffered_TimeThresholdFlush(t *testing.T) {
	dir := t.TempDir()
	io := newIO(dir)
	rel := "meta/sessions/probe.jsonl"

	if err := io.AppendLineBuffered(rel, []byte("old\n")); err != nil {
		t.Fatalf("AppendLineBuffered 1: %v", err)
	}
	time.Sleep(300 * time.Millisecond) // chắc chắn vượt bufferedFlushInterval
	if err := io.AppendLineBuffered(rel, []byte("new\n")); err != nil {
		t.Fatalf("AppendLineBuffered 2: %v", err)
	}

	lines := readLines(t, filepath.Join(dir, rel))
	if len(lines) != 2 || lines[0] != "old" || lines[1] != "new" {
		t.Fatalf("ngưỡng 200ms không kích hoạt flush: got %v, want [old new]", lines)
	}
}

// TestIOAppendLineBuffered_DropAfterRemove xác minh sau khi xóa file (và drop writer
// bộ nhớ đệm), lần ghi kế tiếp tạo file mới từ đầu thay vì ghi vào inode đã bị unlink.
func TestIOAppendLineBuffered_DropAfterRemove(t *testing.T) {
	dir := t.TempDir()
	io := newIO(dir)
	rel := "meta/sessions/probe.jsonl"

	if err := io.AppendLineBuffered(rel, []byte("old\n")); err != nil {
		t.Fatalf("AppendLineBuffered 1: %v", err)
	}
	if err := io.RemoveFile(rel); err != nil {
		t.Fatalf("RemoveFile: %v", err)
	}
	if err := io.AppendLineBuffered(rel, []byte("new\n")); err != nil {
		t.Fatalf("AppendLineBuffered 2: %v", err)
	}
	if err := io.FlushBuffered(); err != nil {
		t.Fatalf("FlushBuffered: %v", err)
	}

	lines := readLines(t, filepath.Join(dir, rel))
	if len(lines) != 1 || lines[0] != "new" {
		t.Fatalf("got %v, want [new]", lines)
	}
}

// TestStoreFlushLogs_FlushesAllStores xác minh Store.FlushLogs đẩy log bộ nhớ đệm
// của mọi store con: task log ghi qua RuntimeStore (đường bộ nhớ đệm), flush qua Store.
func TestStoreFlushLogs_FlushesAllStores(t *testing.T) {
	dir := t.TempDir()
	st := NewStore(dir)

	for _, summary := range []string{"one", "two"} {
		if err := st.Runtime.AppendTaskLog("task-1", domain.RuntimeTaskLogEntry{
			Agent:   "writer",
			Event:   "stream",
			Summary: summary,
		}); err != nil {
			t.Fatalf("AppendTaskLog %s: %v", summary, err)
		}
	}

	// Đọc thẳng từ đĩa (không qua LoadTaskLog — đường đó tự flush): chưa có gì.
	if lines := readLines(t, filepath.Join(dir, taskLogPath("task-1"))); len(lines) != 0 {
		t.Fatalf("task log lộ ra trước Store.FlushLogs: %v", lines)
	}
	if err := st.FlushLogs(); err != nil {
		t.Fatalf("FlushLogs: %v", err)
	}
	lines := readLines(t, filepath.Join(dir, taskLogPath("task-1")))
	if len(lines) != 2 {
		t.Fatalf("sau FlushLogs got %d lines, want 2: %v", len(lines), lines)
	}
}

// TestRuntimeQueueAppend_ImmediatelyDurable xác minh queue runtime KHÔNG đi qua
// đường bộ nhớ đệm: mục vừa AppendQueue phải có trên đĩa ngay lập tức (fsync),
// vì queue là dữ liệu khôi phục (ReplayQueue / ensureSeqLoadedLocked / LoadQueueAfter).
func TestRuntimeQueueAppend_ImmediatelyDurable(t *testing.T) {
	dir := t.TempDir()
	st := NewStore(dir)

	if _, err := st.Runtime.AppendQueue(domain.RuntimeQueueItem{
		Kind:     domain.RuntimeQueueUIEvent,
		Priority: domain.RuntimePriorityBackground,
		Summary:  "durable",
	}); err != nil {
		t.Fatalf("AppendQueue: %v", err)
	}

	lines := readLines(t, filepath.Join(dir, runtimeQueuePath))
	if len(lines) != 1 || !strings.Contains(lines[0], "durable") {
		t.Fatalf("queue phải bền vững ngay sau AppendQueue, got %v", lines)
	}
}
