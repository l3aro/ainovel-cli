package tui

import (
	"strings"
	"testing"
	"time"

	"github.com/charmbracelet/bubbles/viewport"
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

// TestEventViewportCacheSpinnerOnlyRunningLines kiểm tra cache render luồng sự kiện:
// thay đổi khung spinner (tick 150ms) chỉ làm thay đổi các dòng đang chạy; mọi dòng hoàn
// thành phải byte-for-byte giống nhau giữa hai lần render. Kết quả cache cũng phải khớp
// chính xác renderEventContent toàn bộ (bảo toàn hành vi render cũ).
func TestEventViewportCacheSpinnerOnlyRunningLines(t *testing.T) {
	now := time.Now()
	m := &Model{viewport: viewport.New(80, 10), eventIndex: make(map[string]int)}
	m.updateViewportSize() // refresh lấy chiều rộng render từ viewport thực tế
	// 2 dòng đang chạy (DISPATCH + TOOL lồng nhau) + 2 dòng hoàn thành
	m.applyEvent(host.Event{ID: "d1", Time: now, Category: "DISPATCH", Agent: "coordinator", Summary: "writer（Viết chương 3）", Level: "info"})
	m.applyEvent(host.Event{ID: "t1", Time: now.Add(100 * time.Millisecond), Category: "TOOL", Agent: "writer", Summary: "draft_chapter", Level: "info", Depth: 1})
	m.applyEvent(host.Event{Time: now.Add(200 * time.Millisecond), Category: "SYSTEM", Summary: "Đã lưu chương 3", Level: "info"})
	m.applyEvent(host.Event{ID: "t2", Time: now.Add(300 * time.Millisecond), Category: "TOOL", Agent: "coordinator", Summary: "novel_context", Level: "info", FinishedAt: now.Add(3 * time.Second), Duration: 3 * time.Second})

	m.toolSpinnerIdx = 0
	m.refreshEventViewport()
	first := m.viewport.View()

	m.toolSpinnerIdx = 4
	m.refreshEventViewport()
	second := m.viewport.View()

	// View() đệm bằng dòng trống đến đủ chiều cao viewport; chỉ so sánh các dòng nội dung
	firstLines := strings.Split(first, "\n")[:len(m.events)]
	secondLines := strings.Split(second, "\n")[:len(m.events)]
	if len(firstLines) != 4 {
		t.Fatalf("expected 4 event lines, got %d: %q", len(firstLines), first)
	}
	for i := range firstLines {
		if i < 2 {
			// Dòng đang chạy phải đổi khung spinner
			if firstLines[i] == secondLines[i] {
				t.Fatalf("running line %d must change with spinner frame: %q", i, firstLines[i])
			}
			continue
		}
		if firstLines[i] != secondLines[i] {
			t.Fatalf("non-running line %d changed across ticks: %q vs %q", i, firstLines[i], secondLines[i])
		}
	}
	// Khung mới thực sự được áp dụng cho dòng đang chạy
	if !strings.Contains(secondLines[0], toolSpinnerFrames[4]) || !strings.Contains(secondLines[1], toolSpinnerFrames[4]) {
		t.Fatalf("running lines missing new spinner frame: %q / %q", secondLines[0], secondLines[1])
	}
	// Cache phải khớp chính xác render toàn bộ thủ công (bảo toàn hành vi)
	if got, want := strings.Join(m.eventLines, "\n"), renderEventContent(m.events, m.eventFlowWidth(), m.toolSpinnerIdx); got != want {
		t.Fatalf("cached content != full render:\n got: %q\nwant: %q", got, want)
	}
}

// TestEventViewportCacheInvalidation kiểm tra logic vô hiệu hóa cache:
// sự kiện hoàn thành phải render lại đúng dòng của nó (✓ thay spinner, thêm duration),
// danh sách dòng đang chạy được duy trì tăng dần, thay đổi chiều rộng xây lại toàn bộ,
// và cắt trượt giữ cache đồng bộ với events theo cap maxEvents.
func TestEventViewportCacheInvalidation(t *testing.T) {
	now := time.Now()
	m := &Model{viewport: viewport.New(80, 10), width: 200, eventIndex: make(map[string]int)} // eventFlowWidth = 100
	m.updateViewportSize() // ứng dụng gọi updateViewportSize khi resize; refresh lấy chiều rộng từ viewport
	centerW := m.eventFlowWidth()
	if centerW != 100 {
		t.Fatalf("eventFlowWidth = %d, want 100", centerW)
	}

	m.applyEvent(host.Event{ID: "d1", Time: now, Category: "DISPATCH", Agent: "coordinator", Summary: "writer", Level: "info"})
	m.refreshEventViewport()
	if len(m.runningEventIdx) != 1 || m.runningEventIdx[0] != 0 {
		t.Fatalf("runningEventIdx = %v, want [0]", m.runningEventIdx)
	}
	if got := m.viewport.View(); !strings.Contains(got, toolSpinnerFrames[0]) {
		t.Fatalf("running dispatch missing spinner: %q", got)
	}

	// Hoàn thành d1: dòng phải render lại (✓ + duration), runningEventIdx rỗng
	m.applyEvent(host.Event{ID: "d1", Time: now, Category: "DISPATCH", Agent: "coordinator", Summary: "writer", Level: "info", FinishedAt: now.Add(3 * time.Second), Duration: 3 * time.Second})
	m.refreshEventViewport()
	if len(m.runningEventIdx) != 0 {
		t.Fatalf("runningEventIdx = %v, want empty after finish", m.runningEventIdx)
	}
	line := strings.Split(m.viewport.View(), "\n")[0]
	if !strings.Contains(line, "✓") || !strings.Contains(line, "3.0s") {
		t.Fatalf("finished line not re-rendered: %q", line)
	}
	if got, want := strings.Join(m.eventLines, "\n"), renderEventContent(m.events, centerW, m.toolSpinnerIdx); got != want {
		t.Fatalf("cached content != full render after finish:\n got: %q\nwant: %q", got, want)
	}

	// Đổi chiều rộng cửa sổ: cache phải xây lại toàn bộ theo chiều rộng mới
	m.width = 100 // eventFlowWidth = 50
	m.updateViewportSize()
	newW := m.eventFlowWidth()
	if newW != 50 {
		t.Fatalf("eventFlowWidth = %d, want 50", newW)
	}
	m.refreshEventViewport()
	if m.eventRenderWidth != newW {
		t.Fatalf("eventRenderWidth = %d, want %d", m.eventRenderWidth, newW)
	}
	if got, want := strings.Join(m.eventLines, "\n"), renderEventContent(m.events, newW, m.toolSpinnerIdx); got != want {
		t.Fatalf("cached content != full render after resize:\n got: %q\nwant: %q", got, want)
	}

	// Vượt cap maxEvents: cắt trượt phải đồng bộ events / cache / runningEventIdx
	for i := range maxEvents {
		m.applyEvent(host.Event{ID: "e" + string(rune('0'+i%10)) + string(rune('0'+i/10)), Time: now, Category: "TOOL", Agent: "writer", Summary: "tool", Level: "info"})
	}
	m.refreshEventViewport()
	if len(m.events) != maxEvents {
		t.Fatalf("events len = %d, want %d", len(m.events), maxEvents)
	}
	if len(m.eventLines) != maxEvents {
		t.Fatalf("eventLines len = %d, want %d", len(m.eventLines), maxEvents)
	}
	if len(m.runningEventIdx) != maxEvents {
		t.Fatalf("runningEventIdx len = %d, want %d", len(m.runningEventIdx), maxEvents)
	}
	// d1 (đã hoàn thành) bị cắt khỏi đầu; sự kiện mới nhất vẫn là dòng cuối
	if m.eventIndex["e"+string(rune('0'+9))+string(rune('0'+49))] != maxEvents-1 {
		t.Fatalf("eventIndex of newest event wrong")
	}
	if got, want := strings.Join(m.eventLines, "\n"), renderEventContent(m.events, newW, m.toolSpinnerIdx); got != want {
		t.Fatalf("cached content != full render after trim:\n got: %q\nwant: %q", got, want)
	}

	// Hoàn thành sự kiện mới nhất: gỡ đúng chỉ số khỏi runningEventIdx
	m.applyEvent(host.Event{ID: "e" + string(rune('0'+9)) + string(rune('0'+49)), Time: now, Category: "TOOL", Agent: "writer", Summary: "tool", Level: "info", FinishedAt: now.Add(time.Second), Duration: time.Second})
	if len(m.runningEventIdx) != maxEvents-1 {
		t.Fatalf("runningEventIdx len = %d, want %d", len(m.runningEventIdx), maxEvents-1)
	}
	for _, idx := range m.runningEventIdx {
		if idx < 0 || idx >= len(m.events) {
			t.Fatalf("runningEventIdx out of range: %d", idx)
		}
		if !m.events[idx].Running() {
			t.Fatalf("runningEventIdx %d not running: %+v", idx, m.events[idx])
		}
	}
}
