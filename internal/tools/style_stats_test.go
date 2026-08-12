package tools

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/voocel/ainovel-cli/internal/domain"
	"github.com/voocel/ainovel-cli/internal/rules"
	"github.com/voocel/ainovel-cli/internal/store"
)

// TestStyleStatsFingerprintCanonical kiểm tra vân tay là dạng chuẩn duy nhất: hai tập đầu vào
// khác nhau không bao giờ trùng vân tay do ký tự phân tách trùng nội dung
// (case do review chỉ ra: title "a\nstop:b" vs stopword "b"), còn cùng nội dung
// luôn cho cùng vân tay bất kể thứ tự lưu.
func TestStyleStatsFingerprintCanonical(t *testing.T) {
	mk := func(title string, names ...string) *store.Store {
		st := store.NewStore(t.TempDir())
		if err := st.Init(); err != nil {
			t.Fatalf("Init: %v", err)
		}
		if err := st.Outline.SaveOutline([]domain.OutlineEntry{{Chapter: 1, Title: title}}); err != nil {
			t.Fatalf("SaveOutline: %v", err)
		}
		if len(names) > 0 {
			chars := make([]domain.Character, 0, len(names))
			for _, n := range names {
				chars = append(chars, domain.Character{Name: n, Role: "phụ"})
			}
			if err := st.Characters.Save(chars); err != nil {
				t.Fatalf("Save characters: %v", err)
			}
		}
		return st
	}

	// Review case: title chứa chuỗi giống dòng stopword phải cho vân tay khác
	fpTitle := styleStatsFingerprint(mk("a\nstop:b"), []int{1})
	fpStop := styleStatsFingerprint(mk("a", "b"), []int{1})
	if fpTitle == fpStop {
		t.Fatalf("vân tay trùng giữa hai tập đầu vào khác nhau: %q", fpTitle)
	}

	// Cùng nội dung → cùng vân tay (tính xác định)
	if fpTitle != styleStatsFingerprint(mk("a\nstop:b"), []int{1}) {
		t.Fatal("cùng nội dung phải cho cùng vân tay")
	}

	// Thứ tự completed / tiêu đề không ảnh hưởng vân tay
	st := mk("a", "Cửu Uyên", "甲")
	if styleStatsFingerprint(st, []int{5, 1, 3}) != styleStatsFingerprint(st, []int{1, 3, 5}) {
		t.Fatal("thứ tự completed phải không ảnh hưởng vân tay")
	}
}

// TestContextToolStyleStatsCacheFingerprint kiểm tra phát hiện bộ đệm cũ theo vân tay nội dung:
// thêm nhân vật (stopword) sau khi cache được lưu làm vân tay thay đổi → cache cũ bị từ chối,
// tính lại theo dữ liệu hiện tại và lưu bộ đệm mới.
func TestContextToolStyleStatsCacheFingerprint(t *testing.T) {
	dir := t.TempDir()
	st := store.NewStore(dir)
	if err := st.Init(); err != nil {
		t.Fatalf("Init: %v", err)
	}

	progress := &domain.Progress{TotalChapters: 10}
	body := "# 第N章\n他不是迟疑，而是恐惧。沉默了几息。像一道光。\n夜色落下。\n他走了。"
	for ch := 1; ch <= 6; ch++ {
		if err := st.Drafts.SaveFinalChapter(ch, body); err != nil {
			t.Fatalf("SaveFinalChapter %d: %v", ch, err)
		}
		progress.CompletedChapters = append(progress.CompletedChapters, ch)
	}
	if err := st.Progress.Save(progress); err != nil {
		t.Fatalf("Save progress: %v", err)
	}
	if err := st.Characters.Save([]domain.Character{{Name: "Cửu Uyên", Role: "chính"}}); err != nil {
		t.Fatalf("Save characters: %v", err)
	}

	tool := NewContextTool(st, References{}, "default", rules.LoadOptions{})
	call := func(chapter int) (map[string]json.RawMessage, error) {
		args, _ := json.Marshal(map[string]any{"chapter": chapter})
		raw, err := tool.Execute(context.Background(), args)
		if err != nil {
			return nil, err
		}
		var payload struct {
			Episodic map[string]json.RawMessage `json:"episodic_memory"`
		}
		if err := json.Unmarshal(raw, &payload); err != nil {
			return nil, err
		}
		return payload.Episodic, nil
	}
	deleteChapters := func() {
		entries, _ := os.ReadDir(filepath.Join(dir, "chapters"))
		for _, e := range entries {
			if err := os.Remove(filepath.Join(dir, "chapters", e.Name())); err != nil {
				t.Fatalf("rm chapter: %v", err)
			}
		}
	}
	restoreChapters := func() {
		for ch := 1; ch <= 6; ch++ {
			if err := st.Drafts.SaveFinalChapter(ch, body); err != nil {
				t.Fatalf("restore chapter %d: %v", ch, err)
			}
		}
	}

	// 1. Lần gọi đầu: cache chưa có → tính và lưu, tiêm style_stats
	ep, err := call(7)
	if err != nil {
		t.Fatalf("novel_context: %v", err)
	}
	if _, ok := ep["style_stats"]; !ok {
		t.Fatal("expected style_stats on first call")
	}

	// 2. Thêm nhân vật (đổi stopwords) sau khi cache đã lưu → vân tay thay đổi
	if err := st.Characters.Save([]domain.Character{{Name: "Cửu Uyên", Role: "chính"}, {Name: "甲", Role: "phụ"}}); err != nil {
		t.Fatalf("Save characters v2: %v", err)
	}

	// 3. Xóa toàn bộ file chương: cache cũ khớp sẽ vẫn tiêm, cache cũ bị từ chối sẽ tính lại → không đủ chương → không tiêm
	deleteChapters()
	ep, err = call(7)
	if err != nil {
		t.Fatalf("novel_context: %v", err)
	}
	if _, ok := ep["style_stats"]; ok {
		t.Fatal("expected stale cache rejected: recompute without chapters must not inject")
	}

	// 4. Khôi phục chương → tính lại và lưu cache với vân tay mới
	restoreChapters()
	ep, err = call(7)
	if err != nil {
		t.Fatalf("novel_context: %v", err)
	}
	if _, ok := ep["style_stats"]; !ok {
		t.Fatal("expected style_stats after restore")
	}

	// 5. Xóa chương lần nữa: cache mới khớp vân tay hiện tại → tiêm từ cache
	deleteChapters()
	ep, err = call(7)
	if err != nil {
		t.Fatalf("novel_context: %v", err)
	}
	if _, ok := ep["style_stats"]; !ok {
		t.Fatal("expected style_stats from refreshed cache")
	}
}
