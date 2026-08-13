package tui

import (
	"errors"
	"fmt"
	"strconv"
	"strings"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/voocel/ainovel-cli/internal/bootstrap"
	"github.com/voocel/ainovel-cli/internal/library"
)

// libraryResult là kết quả của màn hình chọn truyện sau khi chương trình kết thúc.
type libraryResult struct {
	novel    library.Novel // truyện được chọn (kể cả truyện vừa tạo mới)
	selected bool          // true: người dùng đã chọn/tạo truyện; false: thoát ứng dụng
}

// libraryListMsg là kết quả của lệnh tải danh sách kho (library.List).
type libraryListMsg struct {
	novels []library.Novel
	err    error
}

// libraryModel là màn hình chọn truyện chạy trước Host của mỗi phiên làm việc.
// Không cần Host: chỉ đọc kho qua library.List (nạp khi khởi động, tải lại bằng r)
// và tạo truyện mới qua library.Create. Màn hình kết thúc bằng tea.Quit kèm kết quả
// trong result — khi người dùng thoát (q/Ctrl+C), result.selected = false.
type libraryModel struct {
	lib     *library.Library
	novels  []library.Novel
	cursor  int
	loading bool
	listErr error

	creating  bool
	nameInput textinput.Model
	createErr error

	width  int
	result libraryResult
}

var (
	libraryTitleStyle  = lipgloss.NewStyle().Bold(true).Foreground(colorAccent)
	libraryCursorStyle = lipgloss.NewStyle().Bold(true).Foreground(colorAccent)
	libraryDimStyle    = lipgloss.NewStyle().Foreground(colorDim)
	libraryErrorStyle  = lipgloss.NewStyle().Foreground(colorError)
	libraryLegacyStyle = lipgloss.NewStyle().Foreground(colorAccent2)
)

// newLibraryModel tạo màn hình chọn truyện. loading = true ngay từ đầu để khung
// "Đang tải" hiện ra trước khi lệnh Init trả về danh sách đầu tiên.
func newLibraryModel(lib *library.Library) libraryModel {
	nameInput := textinput.New()
	nameInput.Placeholder = "Nhập tên tiểu thuyết…"
	nameInput.Width = 40
	return libraryModel{lib: lib, loading: true, nameInput: nameInput}
}

// Init nạp danh sách kho ngay khi màn hình khởi động.
func (m libraryModel) Init() tea.Cmd {
	return m.reloadCmd()
}

// reloadCmd trả về lệnh gọi library.List; kết quả quay về dưới dạng libraryListMsg.
func (m libraryModel) reloadCmd() tea.Cmd {
	return func() tea.Msg {
		novels, err := m.lib.List()
		return libraryListMsg{novels: novels, err: err}
	}
}

func (m libraryModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.nameInput.Width = max(20, msg.Width-8)
		return m, nil
	case libraryListMsg:
		m.loading = false
		m.listErr = msg.err
		if msg.err == nil {
			m.novels = msg.novels
			if m.cursor >= len(m.novels) {
				m.cursor = max(0, len(m.novels)-1)
			}
		}
		return m, nil
	case tea.KeyMsg:
		return m.handleLibraryKey(msg)
	default:
		return m, nil
	}
}

// handleLibraryKey xử lý phím của màn hình chọn truyện:
// q/Ctrl+C thoát toàn ứng dụng ở mọi trạng thái (kể cả form tạo tên);
// danh sách: ↑/k ↓/j chọn (chặn ở biên), Enter chọn, n tạo mới, r tải lại;
// tạo mới: Enter tạo, Esc hủy về danh sách; các phím khác vào ô nhập tên.
func (m libraryModel) handleLibraryKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	if msg.Type == tea.KeyCtrlC || isRuneKey(msg, 'q') {
		return m, tea.Quit
	}
	if m.creating {
		switch msg.Type {
		case tea.KeyEnter:
			return m.submitCreate()
		case tea.KeyEsc:
			m.creating = false
			m.createErr = nil
			m.nameInput.SetValue("")
			m.nameInput.Blur()
			return m, nil
		default:
			var cmd tea.Cmd
			m.nameInput, cmd = m.nameInput.Update(msg)
			return m, cmd
		}
	}
	switch {
	case msg.Type == tea.KeyUp || isRuneKey(msg, 'k'):
		if m.cursor > 0 {
			m.cursor--
		}
	case msg.Type == tea.KeyDown || isRuneKey(msg, 'j'):
		if m.cursor < len(m.novels)-1 {
			m.cursor++
		}
	case msg.Type == tea.KeyEnter:
		if m.listErr != nil || len(m.novels) == 0 {
			return m, nil
		}
		m.result = libraryResult{novel: m.novels[m.cursor], selected: true}
		return m, tea.Quit
	case isRuneKey(msg, 'n'):
		m.creating = true
		m.createErr = nil
		var cmd tea.Cmd
		cmd = m.nameInput.Focus()
		return m, cmd
	case isRuneKey(msg, 'r'):
		m.loading = true
		return m, m.reloadCmd()
	}
	return m, nil
}

// isRuneKey kiểm tra phím gõ ký tự đơn (ví dụ 'n', 'q').
func isRuneKey(msg tea.KeyMsg, r rune) bool {
	return msg.Type == tea.KeyRunes && len(msg.Runes) == 1 && msg.Runes[0] == r
}

// submitCreate xác thực tên (không được để trống sau trim) rồi tạo truyện qua
// library.Create. Lỗi hiển thị ngay dưới ô nhập; thành công chọn thẳng truyện mới.
func (m libraryModel) submitCreate() (tea.Model, tea.Cmd) {
	name := strings.TrimSpace(m.nameInput.Value())
	if name == "" {
		m.createErr = errors.New("Tên tiểu thuyết không được để trống")
		return m, nil
	}
	novel, err := m.lib.Create(name)
	if err != nil {
		m.createErr = err
		return m, nil
	}
	m.result = libraryResult{novel: novel, selected: true}
	return m, tea.Quit
}

// picked trả về truyện được chọn sau khi màn hình kết thúc; nil khi người dùng thoát.
func (m libraryModel) picked() *library.Novel {
	if !m.result.selected {
		return nil
	}
	novel := m.result.novel
	return &novel
}

func (m libraryModel) View() string {
	var b strings.Builder
	b.WriteString(libraryTitleStyle.Render("Thư viện tiểu thuyết"))
	b.WriteString("\n\n")
	if m.creating {
		b.WriteString(m.renderCreateForm())
	} else {
		b.WriteString(m.renderLibraryBody())
	}
	b.WriteString("\n")
	b.WriteString(libraryDimStyle.Render(m.renderHints()))
	return b.String()
}

// renderLibraryBody hiển thị trạng thái danh sách: đang tải / lỗi / rỗng / các dòng truyện.
func (m libraryModel) renderLibraryBody() string {
	switch {
	case m.loading:
		return "Đang tải kho…"
	case m.listErr != nil:
		return libraryErrorStyle.Render("Không tải được kho: "+m.listErr.Error()) +
			"\n\n" + libraryDimStyle.Render("Nhấn r để thử lại.")
	case len(m.novels) == 0:
		return "Kho trống. Nhấn n để tạo truyện mới."
	}
	var b strings.Builder
	for i, n := range m.novels {
		marker := "  "
		name := n.Name
		if i == m.cursor {
			marker = "❯ "
			name = libraryCursorStyle.Render(n.Name)
		}
		legacy := ""
		if n.Legacy {
			legacy = " " + libraryLegacyStyle.Render("[Di sản]")
		}
		b.WriteString(fmt.Sprintf("%s%s%s · %s · %d chương hoàn thành · %s từ\n",
			marker, name, legacy, snapshotPhaseLabel(string(n.Phase)),
			n.CompletedChapters, formatWordCount(n.TotalWordCount)))
	}
	return strings.TrimSuffix(b.String(), "\n")
}

// renderCreateForm hiển thị ô nhập tên truyện mới, kèm lỗi tạo (nếu có) ngay dưới.
func (m libraryModel) renderCreateForm() string {
	var b strings.Builder
	b.WriteString(libraryTitleStyle.Render("Tạo truyện mới"))
	b.WriteString("\n\nTên: ")
	b.WriteString(m.nameInput.View())
	if m.createErr != nil {
		b.WriteString("\n\n")
		b.WriteString(libraryErrorStyle.Render(m.createErr.Error()))
	}
	return b.String()
}

func (m libraryModel) renderHints() string {
	if m.creating {
		return "Enter tạo · Esc hủy · Ctrl+C thoát"
	}
	return "↑/k ↓/j chọn · Enter mở · n tạo mới · r tải lại · q/Ctrl+C thoát"
}

// formatWordCount hiển thị số từ với dấu chấm phân tách hàng nghìn (12345 → "12.345").
func formatWordCount(n int) string {
	digits := strconv.Itoa(n)
	var b strings.Builder
	for i, r := range digits {
		if i > 0 && (len(digits)-i)%3 == 0 {
			b.WriteByte('.')
		}
		b.WriteRune(r)
	}
	return b.String()
}

// pickNovel chạy màn hình chọn truyện trước mỗi phiên làm việc và trả về truyện
// người dùng đã chọn. Màn hình không cần Host: chỉ đọc kho qua library.List và tạo
// mới qua library.Create. Báo cáo chuột không được bật — như trang chào của bàn làm
// việc, người dùng giữ được tính năng kéo-chọn-sao chép nguyên bản của terminal.
// legacyDir là gốc bố cục cũ (cfg.OutputDir sau FillDefaults), novelsDir là thư mục
// chứa nhiều truyện nằm cạnh legacyDir (filepath.Join(filepath.Dir(legacyDir), "novels")).
// Trả về nil khi người dùng thoát từ màn hình chọn — Run kết thúc.
func pickNovel(cfg bootstrap.Config, legacyDir, novelsDir string) (*library.Novel, error) {
	m := newLibraryModel(library.Open(legacyDir, novelsDir))
	p := tea.NewProgram(m, tea.WithAltScreen())
	final, err := p.Run()
	if err != nil {
		return nil, err
	}
	fm, ok := final.(libraryModel)
	if !ok {
		return nil, nil
	}
	return fm.picked(), nil
}
