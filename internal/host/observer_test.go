package host

import (
	"encoding/json"
	"reflect"
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/voocel/agentcore"
	"github.com/voocel/ainovel-cli/internal/utils"
)

func TestParseSubagentResultError(t *testing.T) {
	cases := []struct {
		name   string
		result string
		want   string
	}{
		{"empty", ``, ""},
		{"object form", `{"error":"unknown agent \"writer2\""}`, `unknown agent "writer2"`},
		{"object empty error field", `{"error":""}`, ""},
		{"bare string - invalid params", `"Invalid parameters: provide exactly one mode (agent+task, tasks, or chain)"`, "Invalid parameters: provide exactly one mode (agent+task, tasks, or chain)"},
		{"bare string - background", `"background mode requires agent + task"`, "background mode requires agent + task"},
		{"bare string - parallel cap", `"Too many parallel tasks (5). Max is 3."`, "Too many parallel tasks (5). Max is 3."},
		{"bare string - normal result not flagged", `"Chapter committed"`, ""},
		{"success object not flagged", `{"chapter":1,"status":"ok"}`, ""},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := parseSubagentResultError(json.RawMessage(c.result))
			if got != c.want {
				t.Fatalf("parseSubagentResultError(%q) = %q, want %q", c.result, got, c.want)
			}
		})
	}
}

// newThinkingTestObserver dựng observer tối giản chỉ để kiểm tra handleThinkingProgress:
// emitD thu toàn bộ delta (kể cả ThinkingSep) vào danh sách; emitC là no-op để streamClear an toàn.
func newThinkingTestObserver() (*observer, *[]string) {
	deltas := &[]string{}
	o := &observer{
		emitD:           func(d string) { *deltas = append(*deltas, d) },
		emitC:           func() {},
		thinkingByAgent: make(map[string]thinkingState),
	}
	return o, deltas
}

// thinkingEvent tạo sự kiện ProgressThinking với văn bản thinking tích lũy.
func thinkingEvent(agent, thinking string) agentcore.Event {
	return agentcore.Event{
		Type: agentcore.EventMessageUpdate,
		Progress: &agentcore.ProgressPayload{
			Kind:     agentcore.ProgressThinking,
			Agent:    agent,
			Thinking: thinking,
		},
	}
}

// thinkingDeltas loại bỏ ThinkingSep khỏi danh sách delta (phân tách chuyển trạng thái
// là hành vi có sẵn của emitStreamDelta, không thuộc phạm vi kiểm tra trích xuất delta).
func thinkingDeltas(deltas []string) []string {
	out := make([]string, 0, len(deltas))
	for _, d := range deltas {
		if d != utils.ThinkingSep {
			out = append(out, d)
		}
	}
	return out
}

// TestThinkingProgressAppendPattern: các bản cập nhật nối tiếp (mỗi bản mở rộng bản
// trước) phải phát ra đúng phần tăng thêm, không phát lại toàn bộ.
func TestThinkingProgressAppendPattern(t *testing.T) {
	o, deltas := newThinkingTestObserver()
	o.handleThinkingProgress(thinkingEvent("a", "t1"))
	o.handleThinkingProgress(thinkingEvent("a", "t1t2"))
	o.handleThinkingProgress(thinkingEvent("a", "t1t2t3"))

	want := []string{"t1", "t2", "t3"}
	if got := thinkingDeltas(*deltas); !reflect.DeepEqual(got, want) {
		t.Fatalf("deltas = %q, want %q", got, want)
	}
}

// TestThinkingProgressRewrite: văn bản mới không bắt đầu bằng văn bản cũ (khối mới
// hoặc viết lại, cùng độ dài hoặc dài hơn) phải phát lại toàn bộ, không mất nội dung.
func TestThinkingProgressRewrite(t *testing.T) {
	o, deltas := newThinkingTestObserver()
	o.handleThinkingProgress(thinkingEvent("a", "t1"))
	o.handleThinkingProgress(thinkingEvent("a", "xy"))            // cùng độ dài, không phải tiền tố
	o.handleThinkingProgress(thinkingEvent("a", "zebra rewrite")) // dài hơn, không phải tiền tố

	want := []string{"t1", "xy", "zebra rewrite"}
	if got := thinkingDeltas(*deltas); !reflect.DeepEqual(got, want) {
		t.Fatalf("deltas = %q, want %q", got, want)
	}
}

// TestThinkingProgressMessageEndReset: trạng thái thinking phải được xóa tại
// EventMessageEnd — khối thứ hai bắt đầu bằng cùng tiền tố với khối thứ nhất vẫn
// phải phát đầy đủ (không bị nuốt phần đầu).
func TestThinkingProgressMessageEndReset(t *testing.T) {
	o, deltas := newThinkingTestObserver()
	o.handleThinkingProgress(thinkingEvent("a", "prefix block one"))
	o.handle(agentcore.Event{Type: agentcore.EventMessageEnd})
	if len(o.thinkingByAgent) != 0 {
		t.Fatalf("thinkingByAgent phải rỗng sau EventMessageEnd, còn %d mục", len(o.thinkingByAgent))
	}
	o.handleThinkingProgress(thinkingEvent("a", "prefix block two continues"))

	want := []string{"prefix block one", "prefix block two continues"}
	if got := thinkingDeltas(*deltas); !reflect.DeepEqual(got, want) {
		t.Fatalf("deltas = %q, want %q", got, want)
	}
}

// TestThinkingProgressWindowTruncation: thinking vượt thinkingWindowBytes vẫn phải
// phát delta tăng dần (cửa sổ bị cắt nhưng emitted là biên hợp lệ).
func TestThinkingProgressWindowTruncation(t *testing.T) {
	o, deltas := newThinkingTestObserver()

	// Văn bản dài đúng 4096 byte (không bị cắt), rồi nối tiếp phần sau.
	big := strings.Repeat("a", thinkingWindowBytes)
	o.handleThinkingProgress(thinkingEvent("a", big))
	o.handleThinkingProgress(thinkingEvent("a", big+"tail"))

	// Rune nhiều byte nằm ngay tại biên cắt: cửa sổ phải lùi về biên rune hợp lệ
	// (bỏ toàn bộ rune dang dở), delta nối tiếp vẫn là phần tăng thêm.
	edge := strings.Repeat("b", thinkingWindowBytes-2) + "😀"
	o.handleThinkingProgress(thinkingEvent("b", edge))
	o.handleThinkingProgress(thinkingEvent("b", edge+"x"))

	want := []string{big, "tail", edge, "x"}
	if got := thinkingDeltas(*deltas); !reflect.DeepEqual(got, want) {
		t.Fatalf("deltas = %q, want %q", got, want)
	}

	// Cửa sổ lưu trong trạng thái phải luôn là chuỗi UTF-8 hợp lệ.
	st := o.thinkingByAgent["b"]
	if st.emitted != len(edge+"x") {
		t.Fatalf("emitted = %d, want %d", st.emitted, len(edge+"x"))
	}
	if !utf8.ValidString(st.window) {
		t.Fatalf("window không phải UTF-8 hợp lệ: %q", st.window)
	}
	wantWindow := strings.Repeat("b", thinkingWindowBytes-2)
	if st.window != wantWindow {
		t.Fatalf("window = %q, want %q", st.window, wantWindow)
	}
}
