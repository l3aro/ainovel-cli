package store

import (
	"testing"

	"github.com/voocel/ainovel-cli/internal/stylestat"
)

// TestStyleStatsStore_RoundTrip kiểm tra lưu/đọc bộ đệm thống kê phong cách giữ nguyên dữ liệu,
// kèm vân tay nội dung để phát hiện bộ đệm cũ.
func TestStyleStatsStore_RoundTrip(t *testing.T) {
	s := newTestStore(t)

	// Chưa từng lưu → (nil, "", nil), thống nhất với hành vi đọc rỗng của các store khác
	if stats, fp, err := s.StyleStats.Load(); err != nil || stats != nil || fp != "" {
		t.Fatalf("Load empty: want (nil, \"\", nil), got (%v, %q, %v)", stats, fp, err)
	}

	want := &stylestat.Stats{
		Chapters:          7,
		Patterns:          []stylestat.PatternStat{{Name: "Câu chỉnh chuẩn", Total: 3, PerChapter: 0.4}},
		TopPhrases:        []stylestat.PhraseStat{{Text: "cửa miệng", Count: 2}},
		RepeatedSentences: []stylestat.SentenceStat{{Text: "câu lặp", Chapters: 3, Count: 4}},
		Ending:            stylestat.EndingStat{ShortRatio: 0.5, MedianRunes: 12},
		OpeningTimeRate:   0.3,
		TitleFormats:      &stylestat.TitleStat{WithPrefix: 6, WithoutPrefix: 1},
	}
	if err := s.StyleStats.Save(want, "abc123"); err != nil {
		t.Fatalf("Save: %v", err)
	}

	got, fp, err := s.StyleStats.Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if fp != "abc123" {
		t.Errorf("fingerprint: want abc123, got %q", fp)
	}
	if got == nil || got.Chapters != 7 ||
		len(got.Patterns) != 1 || got.Patterns[0].Name != "Câu chỉnh chuẩn" ||
		len(got.RepeatedSentences) != 1 || got.RepeatedSentences[0].Count != 4 ||
		got.Ending.MedianRunes != 12 || got.OpeningTimeRate != 0.3 ||
		got.TitleFormats == nil || got.TitleFormats.WithPrefix != 6 {
		t.Errorf("stats roundtrip mismatch: %+v", got)
	}
}

// TestStyleStatsStore_FingerprintRoundTrip kiểm tra các giá trị vân tay khác nhau
// (kể cả rỗng và unicode) được bảo toàn nguyên vẹn qua round-trip.
func TestStyleStatsStore_FingerprintRoundTrip(t *testing.T) {
	fingerprints := []string{"", "a", "0123456789abcdef0123456789abcdef", "vân tay unicode 汉字", "dấu cách và\nxuống dòng"}
	for _, fp := range fingerprints {
		s := newTestStore(t)
		if err := s.StyleStats.Save(&stylestat.Stats{Chapters: 5}, fp); err != nil {
			t.Fatalf("Save(%q): %v", fp, err)
		}
		got, gotFP, err := s.StyleStats.Load()
		if err != nil {
			t.Fatalf("Load(%q): %v", fp, err)
		}
		if got == nil || got.Chapters != 5 {
			t.Errorf("Load(%q): stats mismatch: %+v", fp, got)
		}
		if gotFP != fp {
			t.Errorf("fingerprint roundtrip: want %q, got %q", fp, gotFP)
		}
	}
}
