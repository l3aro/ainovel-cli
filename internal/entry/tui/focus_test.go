package tui

import (
	"strings"
	"testing"

	"github.com/charmbracelet/bubbles/textarea"
	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
)

// baseFocusModel tạo model ở trạng thái bàn làm việc với focus mặc định ở ô nhập,
// đủ các thành phần để xử lý phím/chuột không panic.
func baseFocusModel() Model {
	m := &Model{
		mode:       modeRunning,
		focusInput: true,
		textarea:   textarea.New(),
		viewport:   viewport.New(80, 10),
		streamVP:   viewport.New(80, 10),
		detailVP:   viewport.New(40, 10),
		stateVP:    viewport.New(32, 10),
		streamBuf:  &strings.Builder{},
		eventIndex: make(map[string]int),
		width:      120,
		height:     40,
	}
	// NewModel gọi ta.Focus() — textarea chưa focus sẽ nuốt mọi ký tự gõ.
	m.textarea.Focus()
	return *m
}

func handleKey(t *testing.T, m Model, msg tea.KeyMsg) Model {
	t.Helper()
	next, _ := m.handleBaseKeyMsg(msg)
	return next.(Model)
}

func TestTabCyclesFocusInputAndPanes(t *testing.T) {
	m := baseFocusModel()
	order := []focusPane{focusEvents, focusStream, focusDetail, focusState}
	for i, want := range order {
		m = handleKey(t, m, tea.KeyMsg{Type: tea.KeyTab})
		if m.focusInput {
			t.Fatalf("Tab #%d: expected view focus, got input", i+1)
		}
		if m.focusPane != want {
			t.Fatalf("Tab #%d: want pane %v, got %v", i+1, want, m.focusPane)
		}
	}
	// Vòng về ô nhập
	m = handleKey(t, m, tea.KeyMsg{Type: tea.KeyTab})
	if !m.focusInput {
		t.Fatalf("Tab after last pane must wrap to input, got pane %v", m.focusPane)
	}
	// Và vòng tiếp tục từ ô nhập
	m = handleKey(t, m, tea.KeyMsg{Type: tea.KeyTab})
	if m.focusInput || m.focusPane != focusEvents {
		t.Fatalf("Tab from input: want focusEvents, got input=%v pane=%v", m.focusInput, m.focusPane)
	}
}

func TestShiftTabCyclesReverse(t *testing.T) {
	m := baseFocusModel()
	m = handleKey(t, m, tea.KeyMsg{Type: tea.KeyShiftTab})
	if m.focusInput || m.focusPane != focusState {
		t.Fatalf("ShiftTab from input: want state pane, got input=%v pane=%v", m.focusInput, m.focusPane)
	}
	m = handleKey(t, m, tea.KeyMsg{Type: tea.KeyShiftTab})
	if m.focusInput || m.focusPane != focusDetail {
		t.Fatalf("ShiftTab: want detail pane, got input=%v pane=%v", m.focusInput, m.focusPane)
	}
	m = handleKey(t, m, tea.KeyMsg{Type: tea.KeyShiftTab})
	m = handleKey(t, m, tea.KeyMsg{Type: tea.KeyShiftTab})
	if m.focusInput || m.focusPane != focusEvents {
		t.Fatalf("ShiftTab: want events pane, got input=%v pane=%v", m.focusInput, m.focusPane)
	}
	m = handleKey(t, m, tea.KeyMsg{Type: tea.KeyShiftTab})
	if !m.focusInput {
		t.Fatalf("ShiftTab from first pane must wrap to input, got pane %v", m.focusPane)
	}
}

func TestEscReturnsToInputFromView(t *testing.T) {
	m := baseFocusModel()
	m = handleKey(t, m, tea.KeyMsg{Type: tea.KeyTab}) // input -> events
	if m.focusInput {
		t.Fatal("setup: Tab must move focus to a view")
	}
	m = handleKey(t, m, tea.KeyMsg{Type: tea.KeyEscape})
	if !m.focusInput {
		t.Fatalf("Esc from view must return focus to input, got pane %v", m.focusPane)
	}
}

func TestTypingSwallowedWhenViewFocused(t *testing.T) {
	m := baseFocusModel()
	m = handleKey(t, m, tea.KeyMsg{Type: tea.KeyTab}) // input -> events

	// Gõ ký tự không được vào textarea khi panel giữ focus
	m = handleKey(t, m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("hello")})
	if m.textarea.Value() != "" {
		t.Fatalf("typing must not reach textarea while view focused: %q", m.textarea.Value())
	}
	// Enter không submit
	m = handleKey(t, m, tea.KeyMsg{Type: tea.KeyEnter})
	if m.textarea.Value() != "" {
		t.Fatalf("Enter must not submit while view focused: %q", m.textarea.Value())
	}
	// Esc về ô nhập thì gõ hoạt động trở lại
	m = handleKey(t, m, tea.KeyMsg{Type: tea.KeyEscape})
	m = handleKey(t, m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("abc")})
	if m.textarea.Value() != "abc" {
		t.Fatalf("typing must reach textarea when input focused: %q", m.textarea.Value())
	}
}

func TestArrowKeysScrollFocusedView(t *testing.T) {
	m := baseFocusModel()
	m.streamVP.SetContent(strings.Repeat("dòng\n", 50))
	m.focusViewPane(focusStream)

	before := m.streamVP.YOffset
	m = handleKey(t, m, tea.KeyMsg{Type: tea.KeyDown})
	if m.streamVP.YOffset <= before {
		t.Fatalf("arrow down must scroll the focused stream view (before=%d after=%d)", before, m.streamVP.YOffset)
	}
	// Ô nhập giữ focus: ↑↓ ưu tiên lịch sử; không có lịch sử thì fallback cuộn panel hiện tại (hành vi cũ giữ nguyên).
	m.focusInputArea()
	before = m.streamVP.YOffset
	m = handleKey(t, m, tea.KeyMsg{Type: tea.KeyUp})
	if m.streamVP.YOffset >= before {
		t.Fatalf("arrow up with input focus must fall back to scrolling the pane (before=%d after=%d)", before, m.streamVP.YOffset)
	}
}

func TestPaneHighlightRequiresViewFocus(t *testing.T) {
	m := baseFocusModel()
	m.focusPane = focusStream
	if m.paneHighlighted(focusStream) {
		t.Fatal("pane must not highlight while input focused")
	}
	m.focusViewPane(focusStream)
	if !m.paneHighlighted(focusStream) {
		t.Fatal("focused pane must highlight")
	}
	if m.paneHighlighted(focusDetail) {
		t.Fatal("non-focused pane must not highlight")
	}
}

func TestMouseClickFocusesPaneAndInput(t *testing.T) {
	m := baseFocusModel()
	topH, _, bodyH := m.layoutHeights()

	// Nhấp vào vùng luồng sự kiện (cột giữa, phần trên): panel giữ focus.
	centerX := m.sidebarWidth() + 10
	m = handleMouse(t, m, tea.MouseMsg{X: centerX, Y: topH + 2, Action: tea.MouseActionPress})
	if m.focusInput {
		t.Fatal("click on pane must move focus to the pane")
	}
	if m.focusPane != focusEvents {
		t.Fatalf("click on events area: want focusEvents, got %v", m.focusPane)
	}

	// Nhấp vào vùng ô nhập (dưới phần thân): focus về ô nhập.
	m = handleMouse(t, m, tea.MouseMsg{X: centerX, Y: topH + bodyH + 1, Action: tea.MouseActionPress})
	if !m.focusInput {
		t.Fatalf("click on input area must return focus to input, got pane %v", m.focusPane)
	}
}

func handleMouse(t *testing.T, m Model, msg tea.MouseMsg) Model {
	t.Helper()
	next, _ := m.handleMouseMsg(msg)
	return next.(Model)
}

func TestTabInNewModeKeepsStartupToggle(t *testing.T) {
	m := baseFocusModel()
	m.mode = modeNew
	m.startupMode = startupModeQuick

	m = handleKey(t, m, tea.KeyMsg{Type: tea.KeyTab})
	if m.startupMode != startupModeCoCreate {
		t.Fatalf("Tab in new mode must toggle startup mode, got %v", m.startupMode)
	}
	if !m.focusInput {
		t.Fatal("Tab in new mode must not move focus away from input")
	}
	// Shift+Tab ở trang chào là no-op
	m = handleKey(t, m, tea.KeyMsg{Type: tea.KeyShiftTab})
	if m.startupMode != startupModeCoCreate {
		t.Fatalf("ShiftTab in new mode must be a no-op, got %v", m.startupMode)
	}
	if !m.focusInput {
		t.Fatal("ShiftTab in new mode must not move focus")
	}
}

func keyRune(t *testing.T, s string) tea.KeyMsg {
	t.Helper()
	return tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(s)}
}

func TestFTogglesFullscreenForFocusedView(t *testing.T) {
	m := baseFocusModel()
	m.focusViewPane(focusStream)

	// f với view giữ focus: vào toàn màn hình
	m = handleKey(t, m, keyRune(t, "f"))
	if !m.fullscreen {
		t.Fatal("f with view focused must enter fullscreen")
	}
	if m.focusPane != focusStream {
		t.Fatalf("fullscreen must keep current view, got %v", m.focusPane)
	}
	// f lần nữa: thoát toàn màn hình
	m = handleKey(t, m, keyRune(t, "f"))
	if m.fullscreen {
		t.Fatal("f again must exit fullscreen")
	}
	if m.focusPane != focusStream {
		t.Fatalf("exiting fullscreen must keep the view focused, got %v", m.focusPane)
	}
}

func TestFTypesNormallyWhenInputFocused(t *testing.T) {
	m := baseFocusModel()
	m = handleKey(t, m, keyRune(t, "f"))
	if m.fullscreen {
		t.Fatal("f must not toggle fullscreen while input is focused")
	}
	if m.textarea.Value() != "f" {
		t.Fatalf("f must type into input when input focused: %q", m.textarea.Value())
	}
}

func TestTabInFullscreenCyclesViewsOnly(t *testing.T) {
	m := baseFocusModel()
	m.focusViewPane(focusEvents)
	m.toggleFullscreen()

	order := []focusPane{focusStream, focusDetail, focusState, focusEvents}
	for i, want := range order {
		m = handleKey(t, m, tea.KeyMsg{Type: tea.KeyTab})
		if !m.fullscreen {
			t.Fatalf("Tab #%d: fullscreen must be kept", i+1)
		}
		if m.focusInput {
			t.Fatalf("Tab #%d: fullscreen Tab must never return to input", i+1)
		}
		if m.focusPane != want {
			t.Fatalf("Tab #%d: want view %v, got %v", i+1, want, m.focusPane)
		}
	}
	// Shift+Tab đảo ngược, vẫn ở toàn màn hình
	m = handleKey(t, m, tea.KeyMsg{Type: tea.KeyShiftTab})
	if !m.fullscreen || m.focusPane != focusState {
		t.Fatalf("ShiftTab in fullscreen: want state view + fullscreen kept, got fullscreen=%v pane=%v", m.fullscreen, m.focusPane)
	}
}

func TestEscExitsFullscreenBeforeInput(t *testing.T) {
	m := baseFocusModel()
	m.focusViewPane(focusStream)
	m.toggleFullscreen()

	m = handleKey(t, m, tea.KeyMsg{Type: tea.KeyEscape})
	if m.fullscreen {
		t.Fatal("Esc must exit fullscreen first")
	}
	if m.focusInput {
		t.Fatal("first Esc must keep focus on the view")
	}
	m = handleKey(t, m, tea.KeyMsg{Type: tea.KeyEscape})
	if !m.focusInput {
		t.Fatal("second Esc must return focus to input")
	}
}

func TestFullscreenRendersOnlyFocusedPanel(t *testing.T) {
	m := baseFocusModel()
	m.mode = modeRunning
	m.viewport.SetContent("sự kiện\n")
	m.streamVP.SetContent("đầu ra\n")

	// Chia cột: cả hai panel đều xuất hiện
	split := m.View()
	if !strings.Contains(split, ":: Luồng sự kiện") || !strings.Contains(split, "▍Đầu ra trực tiếp") {
		t.Fatalf("split view must render both panels")
	}

	// Toàn màn hình luồng sự kiện: chỉ panel đó
	m.focusViewPane(focusEvents)
	m.toggleFullscreen()
	full := m.View()
	if !strings.Contains(full, ":: Luồng sự kiện") {
		t.Fatal("fullscreen events view must render the events panel")
	}
	if strings.Contains(full, "▍Đầu ra trực tiếp") {
		t.Fatal("fullscreen events view must not render the stream panel")
	}

	// Tab đổi sang stream: render panel stream, không còn events
	m = handleKey(t, m, tea.KeyMsg{Type: tea.KeyTab})
	full = m.View()
	if !strings.Contains(full, "▍Đầu ra trực tiếp") {
		t.Fatal("fullscreen stream view must render the stream panel")
	}
	if strings.Contains(full, ":: Luồng sự kiện") {
		t.Fatal("fullscreen stream view must not render the events panel")
	}

	// Thoát toàn màn hình: trở lại chia cột
	m = handleKey(t, m, tea.KeyMsg{Type: tea.KeyEscape})
	split = m.View()
	if !strings.Contains(split, ":: Luồng sự kiện") || !strings.Contains(split, "▍Đầu ra trực tiếp") {
		t.Fatal("exiting fullscreen must restore the split view")
	}
}
