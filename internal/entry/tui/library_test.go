package tui

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/voocel/ainovel-cli/internal/domain"
	"github.com/voocel/ainovel-cli/internal/library"
	"github.com/voocel/ainovel-cli/internal/store"
)

// initNovelRoot tạo một truyện đã khởi tạo (store + progress) trong dir.
func initNovelRoot(t *testing.T, dir, name string) {
	t.Helper()
	s := store.NewStore(dir)
	if err := s.Init(); err != nil {
		t.Fatalf("init store %s: %v", dir, err)
	}
	if err := s.Progress.Init(name, 0); err != nil {
		t.Fatalf("init progress %s: %v", dir, err)
	}
}

// libUpdate là kết quả Update của màn hình chọn truyện: model mới + lệnh trả về.
type libUpdate struct {
	model libraryModel
	cmd   tea.Cmd
}

// updateLibrary gọi Update của màn hình chọn truyện.
func updateLibrary(m libraryModel, msg tea.Msg) libUpdate {
	next, cmd := m.Update(msg)
	return libUpdate{model: next.(libraryModel), cmd: cmd}
}

// libraryModelWithList nạp danh sách vào màn hình như thể lệnh List đã trả kết quả.
func libraryModelWithList(m libraryModel, novels []library.Novel) libraryModel {
	return updateLibrary(m, libraryListMsg{novels: novels}).model
}

// mustQuitCmd khẳng định cmd là tea.Quit (thực thi ra tea.QuitMsg).
func mustQuitCmd(t *testing.T, cmd tea.Cmd) {
	t.Helper()
	if cmd == nil {
		t.Fatal("mong đợi lệnh tea.Quit, nhận nil")
	}
	if _, ok := cmd().(tea.QuitMsg); !ok {
		t.Fatalf("mong đợi tea.QuitMsg, nhận %T", cmd())
	}
}

func testNovels() []library.Novel {
	return []library.Novel{
		{Name: "Truyện A", Dir: "out/novels/a", Phase: domain.PhaseWriting, CompletedChapters: 3, TotalWordCount: 12345},
		{Name: "Truyện B", Dir: "out/novels/b", Phase: domain.PhaseOutline, CompletedChapters: 1, TotalWordCount: 500},
		{Name: "Truyện Cũ", Dir: "out/novel", Phase: domain.PhaseComplete, CompletedChapters: 12, TotalWordCount: 100000, Legacy: true},
	}
}

func TestLibraryModel_NavigationAndSelect(t *testing.T) {
	m := libraryModelWithList(newLibraryModel(library.Open("out/novel", "out/novels")), testNovels())

	view := m.View()
	for _, want := range []string{"Thư viện tiểu thuyết", "Truyện A", "Truyện B", "Truyện Cũ", "Viết", "Đề cương", "Hoàn thành", "3 chương hoàn thành", "12.345 từ", "100.000 từ"} {
		if !strings.Contains(view, want) {
			t.Errorf("danh sách phải hiển thị %q, thiếu trong view", want)
		}
	}
	// Nhãn "Di sản" chỉ gắn cho truyện legacy.
	if strings.Count(view, "[Di sản]") != 1 {
		t.Errorf("chỉ truyện legacy được gắn [Di sản], nhận %d nhãn", strings.Count(view, "[Di sản]"))
	}

	// Lên ở đầu danh sách: giữ nguyên.
	m = updateLibrary(m, tea.KeyMsg{Type: tea.KeyUp}).model
	if m.cursor != 0 {
		t.Fatalf("↑ ở đầu danh sách phải giữ cursor=0, nhận %d", m.cursor)
	}
	m = updateLibrary(m, keyRune(t, "k")).model
	if m.cursor != 0 {
		t.Fatalf("k ở đầu danh sách phải giữ cursor=0, nhận %d", m.cursor)
	}

	// Xuống: ↓/j đều di chuyển, chặn ở cuối; k lùi lại.
	m = updateLibrary(m, tea.KeyMsg{Type: tea.KeyDown}).model
	m = updateLibrary(m, keyRune(t, "j")).model
	if m.cursor != 2 {
		t.Fatalf("↓ rồi j phải tới cursor=2, nhận %d", m.cursor)
	}
	m = updateLibrary(m, tea.KeyMsg{Type: tea.KeyDown}).model
	if m.cursor != 2 {
		t.Fatalf("↓ ở cuối danh sách phải chặn cursor=2, nhận %d", m.cursor)
	}
	m = updateLibrary(m, keyRune(t, "k")).model
	if m.cursor != 1 {
		t.Fatalf("k phải lùi về cursor=1, nhận %d", m.cursor)
	}
	if !strings.Contains(m.View(), "❯ Truyện B") {
		t.Errorf("con trỏ phải đánh dấu dòng Truyện B, view:\n%s", m.View())
	}

	// Enter chọn đúng truyện đang trỏ và thoát.
	u := updateLibrary(m, tea.KeyMsg{Type: tea.KeyEnter})
	mustQuitCmd(t, u.cmd)
	if !u.model.result.selected {
		t.Fatal("Enter phải đánh dấu đã chọn truyện")
	}
	if got := u.model.result.novel; got.Name != "Truyện B" || got.Dir != "out/novels/b" {
		t.Errorf("Enter phải chọn truyện đang trỏ, nhận %+v", got)
	}
	if p := u.model.picked(); p == nil || p.Dir != "out/novels/b" {
		t.Errorf("picked() phải trả truyện đã chọn, nhận %+v", p)
	}
}

func TestLibraryModel_EmptyState(t *testing.T) {
	m := libraryModelWithList(newLibraryModel(library.Open("out/novel", "out/novels")), nil)

	if view := m.View(); !strings.Contains(view, "Kho trống. Nhấn n để tạo truyện mới.") {
		t.Errorf("kho rỗng phải hiển thị trạng thái rỗng, view:\n%s", view)
	}

	// Enter trên kho rỗng: không chọn, không thoát.
	u := updateLibrary(m, tea.KeyMsg{Type: tea.KeyEnter})
	if u.cmd != nil {
		t.Fatalf("Enter trên kho rỗng không được trả lệnh, nhận %T", u.cmd)
	}
	if u.model.result.selected {
		t.Fatal("kho rỗng không được đánh dấu đã chọn")
	}
	if u.model.picked() != nil {
		t.Fatal("picked() phải nil khi chưa chọn")
	}
}

func TestLibraryModel_InitLoadsList(t *testing.T) {
	root := t.TempDir()
	legacy := filepath.Join(root, "novel")
	initNovelRoot(t, legacy, "Truyện Cũ")

	m := newLibraryModel(library.Open(legacy, filepath.Join(root, "novels")))
	if !m.loading {
		t.Fatal("màn hình mới tạo phải ở trạng thái đang tải")
	}
	msg := m.Init()()
	list, ok := msg.(libraryListMsg)
	if !ok {
		t.Fatalf("lệnh Init phải trả libraryListMsg, nhận %T", msg)
	}
	if list.err != nil {
		t.Fatalf("List không mong đợi lỗi, nhận %v", list.err)
	}
	m = updateLibrary(m, list).model
	if m.loading {
		t.Fatal("sau khi nhận kết quả phải hết trạng thái đang tải")
	}
	if len(m.novels) != 1 || m.novels[0].Name != "Truyện Cũ" || !m.novels[0].Legacy {
		t.Errorf("Init phải nạp đúng truyện legacy, nhận %+v", m.novels)
	}
	if m.cursor != 0 {
		t.Errorf("cursor phải bắt đầu ở 0, nhận %d", m.cursor)
	}
}

func TestLibraryModel_CreateValidationAndErrors(t *testing.T) {
	root := t.TempDir()
	lib := library.Open(filepath.Join(root, "novel"), filepath.Join(root, "novels"))
	m := newLibraryModel(lib)

	// n mở form tạo mới với ô nhập focus.
	m = updateLibrary(m, keyRune(t, "n")).model
	if !m.creating {
		t.Fatal("n phải mở form tạo truyện mới")
	}
	if !m.nameInput.Focused() {
		t.Error("ô nhập tên phải được focus trong form tạo")
	}
	if view := m.View(); !strings.Contains(view, "Tạo truyện mới") {
		t.Errorf("form tạo phải có tiêu đề riêng, view:\n%s", view)
	}

	// Enter với tên rỗng: lỗi inline, không thoát, không tạo.
	u := updateLibrary(m, tea.KeyMsg{Type: tea.KeyEnter})
	if u.cmd != nil {
		t.Fatalf("tên rỗng không được trả lệnh thoát, nhận %T", u.cmd)
	}
	m = u.model
	if m.createErr == nil || !strings.Contains(m.createErr.Error(), "không được để trống") {
		t.Fatalf("tên rỗng phải báo lỗi xác thực, nhận %v", m.createErr)
	}
	if view := m.View(); !strings.Contains(view, "không được để trống") {
		t.Errorf("lỗi xác thực phải hiển thị ngay trong form, view:\n%s", view)
	}
	if m.result.selected {
		t.Fatal("tên rỗng không được đánh dấu đã chọn")
	}

	// Gõ tên rồi Esc: hủy form, xóa lỗi và nội dung ô nhập.
	m = updateLibrary(m, keyRune(t, "Đang nhập")).model
	if m.nameInput.Value() != "Đang nhập" {
		t.Fatalf("ô nhập phải nhận ký tự gõ, nhận %q", m.nameInput.Value())
	}
	m = updateLibrary(m, tea.KeyMsg{Type: tea.KeyEsc}).model
	if m.creating {
		t.Fatal("Esc phải hủy form tạo")
	}
	if m.createErr != nil {
		t.Fatalf("Esc phải xóa lỗi tạo, nhận %v", m.createErr)
	}
	if m.nameInput.Value() != "" {
		t.Errorf("Esc phải xóa nội dung ô nhập, nhận %q", m.nameInput.Value())
	}

	// Lỗi từ library.Create hiển thị inline: novelsDir là file → tạo thất bại.
	if err := os.WriteFile(filepath.Join(root, "novels"), []byte("không phải thư mục"), 0o644); err != nil {
		t.Fatal(err)
	}
	m = updateLibrary(m, keyRune(t, "n")).model
	m = updateLibrary(m, keyRune(t, "Truyện Mới")).model
	u = updateLibrary(m, tea.KeyMsg{Type: tea.KeyEnter})
	if u.cmd != nil {
		t.Fatalf("tạo thất bại không được trả lệnh thoát, nhận %T", u.cmd)
	}
	m = u.model
	if m.createErr == nil || !strings.Contains(m.createErr.Error(), "library:") {
		t.Fatalf("lỗi tạo phải hiển thị inline, nhận %v", m.createErr)
	}
	if view := m.View(); !strings.Contains(view, "library:") {
		t.Errorf("lỗi tạo phải nằm trong view, view:\n%s", view)
	}
	if m.result.selected {
		t.Fatal("tạo thất bại không được đánh dấu đã chọn")
	}
}

func TestLibraryModel_CreateSuccessSelectsRoot(t *testing.T) {
	root := t.TempDir()
	legacy := filepath.Join(root, "novel")
	novelsDir := filepath.Join(root, "novels")
	initNovelRoot(t, legacy, "Truyện Cũ")

	m := newLibraryModel(library.Open(legacy, novelsDir))
	m = updateLibrary(m, keyRune(t, "n")).model
	m = updateLibrary(m, keyRune(t, "Tên Mới")).model
	u := updateLibrary(m, tea.KeyMsg{Type: tea.KeyEnter})
	mustQuitCmd(t, u.cmd)
	m = u.model

	if !m.result.selected {
		t.Fatal("tạo thành công phải chọn thẳng truyện mới")
	}
	novel := m.result.novel
	if novel.Name != "Tên Mới" {
		t.Errorf("tên truyện mới phải là %q, nhận %q", "Tên Mới", novel.Name)
	}
	// Slug ASCII thường: dấu tiếng Việt bị bỏ, chuỗi khác gộp thành dấu '-'.
	if want := filepath.Join(novelsDir, "t-n-m-i"); novel.Dir != want {
		t.Errorf("truyện mới phải nằm ở %q, nhận %q", want, novel.Dir)
	}
	if novel.Legacy {
		t.Error("truyện mới không được đánh dấu legacy")
	}
	if p := m.picked(); p == nil || p.Dir != novel.Dir {
		t.Errorf("picked() phải trả truyện vừa tạo, nhận %+v", p)
	}

	// Truyện mới phải là gốc store thật trên đĩa.
	p, err := store.NewStore(novel.Dir).Progress.Load()
	if err != nil {
		t.Fatalf("đọc progress truyện mới: %v", err)
	}
	if p == nil || p.NovelName != "Tên Mới" {
		t.Errorf("progress truyện mới phải có NovelName=Tên Mới, nhận %+v", p)
	}

	// Và xuất hiện trong kho khi liệt kê lại.
	got, err := library.Open(legacy, novelsDir).List()
	if err != nil {
		t.Fatalf("List sau khi tạo: %v", err)
	}
	found := false
	for _, n := range got {
		if n.Dir == novel.Dir && n.Name == "Tên Mới" {
			found = true
		}
	}
	if !found {
		t.Errorf("truyện mới phải có trong List sau khi tạo, nhận %+v", got)
	}
}

func TestLibraryModel_ReloadErrorThenClear(t *testing.T) {
	root := t.TempDir()
	legacy := filepath.Join(root, "novel")
	novelsDir := filepath.Join(root, "novels")
	initNovelRoot(t, legacy, "Truyện Cũ")
	// Progress hỏng trong novels/bad làm List lỗi, kèm tên thư mục.
	bad := filepath.Join(novelsDir, "bad")
	if err := os.MkdirAll(filepath.Join(bad, "meta"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(bad, "meta", "progress.json"), []byte("{hỏng"), 0o644); err != nil {
		t.Fatal(err)
	}

	m := newLibraryModel(library.Open(legacy, novelsDir))
	msg := m.Init()()
	list, ok := msg.(libraryListMsg)
	if !ok {
		t.Fatalf("lệnh Init phải trả libraryListMsg, nhận %T", msg)
	}
	m = updateLibrary(m, list).model
	if m.listErr == nil || !strings.Contains(m.listErr.Error(), "bad") {
		t.Fatalf("progress hỏng phải báo lỗi kèm tên thư mục, nhận %v", m.listErr)
	}
	view := m.View()
	if !strings.Contains(view, "Không tải được kho") || !strings.Contains(view, "Nhấn r để thử lại") {
		t.Errorf("lỗi tải phải hiển thị kèm gợi ý r, view:\n%s", view)
	}
	// Đang lỗi: Enter không chọn được.
	u := updateLibrary(m, tea.KeyMsg{Type: tea.KeyEnter})
	if u.cmd != nil || u.model.result.selected {
		t.Error("khi danh sách lỗi, Enter không được chọn truyện")
	}

	// Sửa lỗi rồi nhấn r: tải lại thành công, xóa lỗi, hiện danh sách.
	initNovelRoot(t, bad, "Truyện B")
	u = updateLibrary(m, keyRune(t, "r"))
	if !u.model.loading {
		t.Fatal("r phải chuyển sang trạng thái đang tải")
	}
	if u.cmd == nil {
		t.Fatal("r phải trả lệnh tải lại")
	}
	msg2 := u.cmd()
	list2, ok := msg2.(libraryListMsg)
	if !ok {
		t.Fatalf("lệnh tải lại phải trả libraryListMsg, nhận %T", msg2)
	}
	if list2.err != nil {
		t.Fatalf("tải lại sau khi sửa không được lỗi, nhận %v", list2.err)
	}
	m = updateLibrary(u.model, list2).model
	if m.listErr != nil {
		t.Fatalf("tải lại thành công phải xóa lỗi, nhận %v", m.listErr)
	}
	view = m.View()
	for _, want := range []string{"Truyện Cũ", "Truyện B", "[Di sản]"} {
		if !strings.Contains(view, want) {
			t.Errorf("sau tải lại phải hiển thị %q, view:\n%s", want, view)
		}
	}

	// Con trỏ chặn khi danh sách thu nhỏ sau tải lại.
	m = libraryModelWithList(m, testNovels())
	m = updateLibrary(m, tea.KeyMsg{Type: tea.KeyDown}).model
	m = updateLibrary(m, tea.KeyMsg{Type: tea.KeyDown}).model
	if m.cursor != 2 {
		t.Fatalf("chuẩn bị: cursor phải ở 2, nhận %d", m.cursor)
	}
	m = libraryModelWithList(m, testNovels()[:1])
	if m.cursor != 0 {
		t.Errorf("tải lại danh sách nhỏ hơn phải chặn cursor về 0, nhận %d", m.cursor)
	}
}

func TestLibraryModel_QuitResult(t *testing.T) {
	// Ctrl+C và q từ danh sách: thoát toàn ứng dụng, không chọn truyện.
	for _, quit := range []tea.Msg{
		tea.KeyMsg{Type: tea.KeyCtrlC},
		keyRune(t, "q"),
	} {
		m := libraryModelWithList(newLibraryModel(library.Open("out/novel", "out/novels")), testNovels())
		u := updateLibrary(m, quit)
		mustQuitCmd(t, u.cmd)
		if u.model.result.selected {
			t.Errorf("%T: thoát không được đánh dấu đã chọn", quit)
		}
		if p := u.model.picked(); p != nil {
			t.Errorf("%T: picked() phải nil khi thoát, nhận %+v", quit, p)
		}
	}

	// q từ kho rỗng cũng thoát.
	m := libraryModelWithList(newLibraryModel(library.Open("out/novel", "out/novels")), nil)
	u := updateLibrary(m, keyRune(t, "q"))
	mustQuitCmd(t, u.cmd)
	if u.model.picked() != nil {
		t.Error("q từ kho rỗng phải thoát với picked()=nil")
	}

	// Ctrl+C và q trong form tạo: thoát toàn ứng dụng.
	m = updateLibrary(newLibraryModel(library.Open("out/novel", "out/novels")), keyRune(t, "n")).model
	u = updateLibrary(m, tea.KeyMsg{Type: tea.KeyCtrlC})
	mustQuitCmd(t, u.cmd)
	if u.model.picked() != nil {
		t.Error("Ctrl+C trong form tạo phải thoát với picked()=nil")
	}
	// q trong form tạo: thoát toàn ứng dụng, không nhập ký tự, không chọn truyện.
	m = updateLibrary(newLibraryModel(library.Open("out/novel", "out/novels")), keyRune(t, "n")).model
	u = updateLibrary(m, keyRune(t, "q"))
	mustQuitCmd(t, u.cmd)
	if u.model.result.selected {
		t.Error("q trong form tạo phải thoát mà không đánh dấu đã chọn")
	}
	if u.model.picked() != nil {
		t.Error("q trong form tạo phải thoát với picked()=nil")
	}
	if u.model.nameInput.Value() != "" {
		t.Errorf("q trong form tạo không được vào ô nhập, nhận %q", u.model.nameInput.Value())
	}
}
