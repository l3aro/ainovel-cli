package tui

import (
	"strings"
	"testing"

	"github.com/voocel/ainovel-cli/internal/host"
)

func TestRenderTopBarShowsVersion(t *testing.T) {
	out := renderTopBar(host.UISnapshot{
		Provider:  "openrouter",
		ModelName: "test-model",
		NovelName: "测试小说",
	}, 120, "", "v1.2.3")
	if !strings.Contains(out, "ainovel-cli v1.2.3") {
		t.Fatalf("top bar missing version: %q", out)
	}
}

// TestStreamRoundMaterialization kiểm tra tích lũy delta vào builder và đóng round:
// không mất dữ liệu, không trùng lặp qua các ranh giới round, và render luôn thấy round đang mở.
func TestStreamRoundMaterialization(t *testing.T) {
	m := &Model{streamBuf: &strings.Builder{}}

	// Delta tích lũy vào builder; render input gồm cả round đang mở, chưa có gì trong rounds.
	m.streamBuf.WriteString("▸ Agent A")
	m.streamBuf.WriteString("\n nhiệm vụ")
	got := m.streamRoundsForRender()
	if len(got) != 1 || got[0] != "▸ Agent A\n nhiệm vụ" {
		t.Fatalf("active round not exposed for render: %#v", got)
	}
	if len(m.streamRounds) != 0 {
		t.Fatalf("active round leaked into closed rounds: %#v", m.streamRounds)
	}

	// Đóng round: nội dung chuyển nguyên vẹn vào rounds, builder reset — không mất/dúplicate.
	m.closeStreamRound()
	if len(m.streamRounds) != 1 || m.streamRounds[0] != "▸ Agent A\n nhiệm vụ" {
		t.Fatalf("closed round wrong: %#v", m.streamRounds)
	}
	if m.streamBuf.Len() != 0 {
		t.Fatalf("builder not reset after close: %q", m.streamBuf.String())
	}

	// Round trống (không delta nào) không tạo round đóng.
	m.closeStreamRound()
	if len(m.streamRounds) != 1 {
		t.Fatalf("empty round must not close: %#v", m.streamRounds)
	}

	// Nhiều round liên tiếp: nội dung đủ, thứ tự đúng, không trùng lặp.
	for _, delta := range []string{"a", "b"} {
		m.streamBuf.WriteString(delta)
	}
	m.closeStreamRound()
	for _, delta := range []string{"c", "d"} {
		m.streamBuf.WriteString(delta)
	}
	m.closeStreamRound()
	got = m.streamRoundsForRender()
	want := []string{"▸ Agent A\n nhiệm vụ", "ab", "cd"}
	if len(got) != len(want) {
		t.Fatalf("round count = %d, want %d: %#v", len(got), len(want), got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("round %d = %q, want %q", i, got[i], want[i])
		}
	}

	// Round chỉ có khoảng trắng vẫn được đóng (giữ byte, không render gì).
	m.streamBuf.WriteString(" \n ")
	m.closeStreamRound()
	if len(m.streamRounds) != 4 || m.streamRounds[3] != " \n " {
		t.Fatalf("whitespace round not closed verbatim: %#v", m.streamRounds)
	}

	// Trim giới hạn số round đã đóng, dành 1 slot cho round đang mở (cap cũ tính cả round active).
	for i := 0; i < maxStreamRounds+5; i++ {
		m.streamBuf.WriteString("x")
		m.closeStreamRound()
	}
	m.trimStreamRounds()
	if len(m.streamRounds) != maxStreamRounds-1 {
		t.Fatalf("trimStreamRounds len = %d, want %d", len(m.streamRounds), maxStreamRounds-1)
	}

	// Ngay sau ranh giới clear (builder rỗng): viewport chỉ còn maxStreamRounds-1 round đóng,
	// khớp cửa sổ "31 round đóng + slot active rỗng" của logic cũ.
	got = m.streamRoundsForRender()
	if len(got) != maxStreamRounds-1 {
		t.Fatalf("post-clear render rounds = %d, want %d", len(got), maxStreamRounds-1)
	}
	if got[0] != "x" || got[len(got)-1] != "x" {
		t.Fatalf("post-clear window wrong: %#v", got)
	}

	// Có round đang mở: tổng = maxStreamRounds-1 round đóng + round active = maxStreamRounds, không vượt cap.
	m.streamBuf.WriteString("y")
	got = m.streamRoundsForRender()
	if len(got) != maxStreamRounds {
		t.Fatalf("render rounds = %d, want %d", len(got), maxStreamRounds)
	}
	if got[len(got)-1] != "y" {
		t.Fatalf("active round missing from render view: %#v", got)
	}
	m.streamBuf.Reset()
	got = m.streamRoundsForRender()
	if len(got) != maxStreamRounds-1 || got[len(got)-1] != "x" {
		t.Fatalf("closed-only render view wrong: %#v", got)
	}
}
