package library

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/voocel/ainovel-cli/internal/domain"
	"github.com/voocel/ainovel-cli/internal/store"
)

// newNovel tạo một truyện đã khởi tạo trong dir.
func newNovel(t *testing.T, dir, name string, totalChapters int) {
	t.Helper()
	s := store.NewStore(dir)
	if err := s.Init(); err != nil {
		t.Fatalf("init store %s: %v", dir, err)
	}
	if err := s.Progress.Init(name, totalChapters); err != nil {
		t.Fatalf("init progress %s: %v", dir, err)
	}
}

// writeProgress ghi thẳng meta/progress.json (dùng cho các ca lỗi/hỏng).
func writeProgress(t *testing.T, dir, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Join(dir, "meta"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "meta", "progress.json"), []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestList_LegacyOnly(t *testing.T) {
	root := t.TempDir()
	legacy := filepath.Join(root, "novel")
	newNovel(t, legacy, "Truyện Cũ", 5)

	lib := Open(legacy, filepath.Join(root, "novels"))
	got, err := lib.List()
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("muốn 1 truyện legacy, nhận %d: %+v", len(got), got)
	}
	n := got[0]
	if n.Name != "Truyện Cũ" || n.Dir != legacy || !n.Legacy {
		t.Errorf("sai thông tin legacy: %+v", n)
	}
}

func TestList_LegacyFallbackName(t *testing.T) {
	root := t.TempDir()
	legacy := filepath.Join(root, "novel")
	newNovel(t, legacy, "", 0) // NovelName rỗng -> dự phòng tên thư mục

	got, err := Open(legacy, filepath.Join(root, "novels")).List()
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(got) != 1 || got[0].Name != "novel" {
		t.Fatalf("muốn tên dự phòng 'novel', nhận %+v", got)
	}
}

func TestList_LegacyFirstThenModernSorted(t *testing.T) {
	root := t.TempDir()
	legacy := filepath.Join(root, "novel")
	newNovel(t, legacy, "Chuyện Cũ", 2)

	novels := filepath.Join(root, "novels")
	newNovel(t, filepath.Join(novels, "zeta"), "Zeta", 0)
	newNovel(t, filepath.Join(novels, "alpha"), "Alpha", 0)
	newNovel(t, filepath.Join(novels, "beta"), "Beta", 0)

	got, err := Open(legacy, novels).List()
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	want := []string{"Chuyện Cũ", "Alpha", "Beta", "Zeta"}
	if len(got) != len(want) {
		t.Fatalf("muốn %d truyện, nhận %d: %+v", len(want), len(got), got)
	}
	for i, name := range want {
		if got[i].Name != name {
			t.Errorf("vị trí %d: muốn %q, nhận %q", i, name, got[i].Name)
		}
	}
	if !got[0].Legacy {
		t.Error("truyện legacy phải đứng trước")
	}
}

func TestList_CaseInsensitiveStableOrder(t *testing.T) {
	root := t.TempDir()
	novels := filepath.Join(root, "novels")
	newNovel(t, filepath.Join(novels, "b"), "Beta", 0)
	newNovel(t, filepath.Join(novels, "B"), "beta", 0) // trùng khóa không phân biệt hoa thường

	got, err := Open(filepath.Join(root, "novel"), novels).List()
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("muốn 2 truyện, nhận %d", len(got))
	}
	// SliceStable: trùng khóa giữ nguyên thứ tự đọc (ReadDir sắp byte: B trước b).
	if filepath.Base(got[0].Dir) != "B" || filepath.Base(got[1].Dir) != "b" {
		t.Errorf("thứ tự ổn định bị phá vỡ: %+v", got)
	}
}

func TestList_LoadsProgressFields(t *testing.T) {
	root := t.TempDir()
	novels := filepath.Join(root, "novels")
	dir := filepath.Join(novels, "my-book")
	newNovel(t, dir, "Sách Hay", 0)

	s := store.NewStore(dir)
	p, err := s.Progress.Load()
	if err != nil {
		t.Fatal(err)
	}
	p.Phase = domain.PhaseWriting
	p.CompletedChapters = []int{1, 2, 3}
	p.TotalWordCount = 12345
	if err := s.Progress.Save(p); err != nil {
		t.Fatal(err)
	}

	got, err := Open(filepath.Join(root, "novel"), novels).List()
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("muốn 1 truyện, nhận %d", len(got))
	}
	n := got[0]
	if n.Phase != domain.PhaseWriting || n.CompletedChapters != 3 || n.TotalWordCount != 12345 {
		t.Errorf("sai trường progress: %+v", n)
	}
	if n.Legacy {
		t.Error("truyện mới không được đánh dấu Legacy")
	}
}

func TestList_SkipsMissingProgress(t *testing.T) {
	root := t.TempDir()
	legacy := filepath.Join(root, "novel")
	newNovel(t, legacy, "Có Progress", 0)
	novels := filepath.Join(root, "novels")
	newNovel(t, filepath.Join(novels, "ready"), "Sẵn Sàng", 0)
	// Thư mục chưa khởi tạo: không có meta/progress.json.
	if err := os.MkdirAll(filepath.Join(novels, "empty"), 0o755); err != nil {
		t.Fatal(err)
	}
	// File không phải thư mục cũng bị bỏ qua.
	if err := os.WriteFile(filepath.Join(novels, "notes.txt"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}

	got, err := Open(legacy, novels).List()
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("muốn 2 truyện (bỏ qua thư mục rỗng và file), nhận %d: %+v", len(got), got)
	}
}

func TestList_MissingRootsNoErrorNoCreate(t *testing.T) {
	root := t.TempDir()
	legacy := filepath.Join(root, "novel")
	novels := filepath.Join(root, "novels")
	got, err := Open(legacy, novels).List()
	if err != nil {
		t.Fatalf("List với gốc chưa tồn tại phải không lỗi: %v", err)
	}
	if len(got) != 0 {
		t.Fatalf("muốn danh sách rỗng, nhận %+v", got)
	}
	// List thuần đọc: không được tạo gốc nào.
	for _, p := range []string{legacy, novels} {
		if _, err := os.Stat(p); !os.IsNotExist(err) {
			t.Errorf("List không được tạo %s (err=%v)", p, err)
		}
	}
}

func TestList_MalformedProgressErrorNamesDir(t *testing.T) {
	root := t.TempDir()
	novels := filepath.Join(root, "novels")
	bad := filepath.Join(novels, "broken")
	writeProgress(t, bad, "{không phải json")

	_, err := Open(filepath.Join(root, "novel"), novels).List()
	if err == nil {
		t.Fatal("progress hỏng phải trả lỗi")
	}
	if !strings.Contains(err.Error(), bad) {
		t.Errorf("lỗi phải kèm tên thư mục %q, nhận: %v", bad, err)
	}

	// Cùng ca cho gốc legacy.
	legacy := filepath.Join(root, "novel")
	writeProgress(t, legacy, "{không phải json")
	_, err = Open(legacy, novels).List()
	if err == nil || !strings.Contains(err.Error(), legacy) {
		t.Errorf("lỗi legacy phải kèm thư mục %q, nhận: %v", legacy, err)
	}
}

func TestList_UnreadableProgressErrorNamesDir(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("chạy với quyền root: không mô phỏng được lỗi quyền")
	}
	root := t.TempDir()
	novels := filepath.Join(root, "novels")
	dir := filepath.Join(novels, "locked")
	newNovel(t, dir, "Khoá", 0)
	if err := os.Chmod(filepath.Join(dir, "meta", "progress.json"), 0o000); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(filepath.Join(dir, "meta", "progress.json"), 0o644) })

	_, err := Open(filepath.Join(root, "novel"), novels).List()
	if err == nil {
		t.Fatal("progress không đọc được phải trả lỗi")
	}
	if !strings.Contains(err.Error(), dir) {
		t.Errorf("lỗi phải kèm tên thư mục %q, nhận: %v", dir, err)
	}
}

func TestCreate_TrimsAndSlugs(t *testing.T) {
	root := t.TempDir()
	lib := Open(filepath.Join(root, "novel"), filepath.Join(root, "novels"))

	n, err := lib.Create("  Mắt Biếc!  ")
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if n.Name != "Mắt Biếc!" {
		t.Errorf("Name phải là tên gốc đã trim, nhận %q", n.Name)
	}
	wantDir := filepath.Join(root, "novels", "m-t-bi-c")
	if n.Dir != wantDir {
		t.Errorf("Dir phải là %q, nhận %q", wantDir, n.Dir)
	}
	if n.Phase != domain.PhaseInit || n.Legacy {
		t.Errorf("sai Novel trả về: %+v", n)
	}

	// Progress ghi đúng một lần, giữ tên gốc: NovelName, Phase init, 0 chương.
	s := store.NewStore(wantDir)
	p, err := s.Progress.Load()
	if err != nil {
		t.Fatalf("load progress: %v", err)
	}
	if p == nil {
		t.Fatal("progress phải tồn tại sau Create")
	}
	if p.NovelName != "Mắt Biếc!" || p.Phase != domain.PhaseInit || p.TotalChapters != 0 {
		t.Errorf("sai progress: %+v", p)
	}
	// Cấu trúc store đã tạo.
	for _, sub := range []string{"chapters", "drafts", "meta"} {
		if fi, err := os.Stat(filepath.Join(wantDir, sub)); err != nil || !fi.IsDir() {
			t.Errorf("thiếu thư mục %s: %v", sub, err)
		}
	}
}

func TestCreate_NoManifest(t *testing.T) {
	root := t.TempDir()
	lib := Open(filepath.Join(root, "novel"), filepath.Join(root, "novels"))
	if _, err := lib.Create("Không Manifest"); err != nil {
		t.Fatalf("Create: %v", err)
	}
	meta := filepath.Join(root, "novels", "kh-ng-manifest", "meta")
	entries, err := os.ReadDir(meta)
	if err != nil {
		t.Fatal(err)
	}
	files := 0
	for _, e := range entries {
		if !e.IsDir() {
			files++
		}
		if !e.IsDir() && e.Name() != "progress.json" {
			t.Errorf("không được tạo manifest/file lạ trong meta/: %s", e.Name())
		}
	}
	if files != 1 {
		t.Errorf("meta/ phải có đúng 1 file (progress.json), nhận %d", files)
	}
}

func TestCreate_RejectsEmptyAndControl(t *testing.T) {
	root := t.TempDir()
	lib := Open(filepath.Join(root, "novel"), filepath.Join(root, "novels"))

	for _, name := range []string{"", "   ", "\t\n"} {
		if _, err := lib.Create(name); err == nil {
			t.Errorf("tên %q phải bị từ chối", name)
		}
	}
	for _, name := range []string{"Mắt\nBiếc", "Truyện\x00Mới", "A\tB"} {
		if _, err := lib.Create(name); err == nil {
			t.Errorf("tên chứa ký tự điều khiển %q phải bị từ chối", name)
		}
	}
	// Không truyện nào được tạo.
	got, err := lib.List()
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(got) != 0 {
		t.Errorf("không được tạo truyện nào, nhận %+v", got)
	}
}

func TestCreate_SlugDeterminismAndFallback(t *testing.T) {
	cases := []struct{ title, slug string }{
		{"Hello World", "hello-world"},
		{"  Hello   World  ", "hello-world"}, // trim + gộp khoảng trắng
		{"Mắt Biếc!", "m-t-bi-c"},            // giữ chữ ASCII, bỏ dấu tiếng Việt
		{"Đêm 7/7", "m-7-7"},                 // chữ số giữ lại
		{"!!!", "novel"},                     // dự phòng khi slug rỗng
		{"---", "novel"},
	}
	for _, c := range cases {
		// Mỗi ca một gốc mới: slug chỉ va chạm với chính mình khi lặp trong cùng gốc.
		root := t.TempDir()
		lib := Open(filepath.Join(root, "novel"), filepath.Join(root, "novels"))
		n, err := lib.Create(c.title)
		if err != nil {
			t.Fatalf("Create(%q): %v", c.title, err)
		}
		if filepath.Base(n.Dir) != c.slug {
			t.Errorf("Create(%q): muốn slug %q, nhận %q", c.title, c.slug, filepath.Base(n.Dir))
		}
	}
}

func TestCreate_CollisionSuffix(t *testing.T) {
	root := t.TempDir()
	lib := Open(filepath.Join(root, "novel"), filepath.Join(root, "novels"))

	// Truyện trước chiếm slug gốc.
	newNovel(t, filepath.Join(root, "novels", "hello-world"), "Kẻ Chiếm Chỗ", 0)

	n1, err := lib.Create("Hello World")
	if err != nil {
		t.Fatalf("Create 1: %v", err)
	}
	if base := filepath.Base(n1.Dir); base != "hello-world-2" {
		t.Errorf("va chạm novelsDir: muốn hello-world-2, nhận %s", base)
	}
	n2, err := lib.Create("Hello World")
	if err != nil {
		t.Fatalf("Create 2: %v", err)
	}
	if base := filepath.Base(n2.Dir); base != "hello-world-3" {
		t.Errorf("va chạm liên tiếp: muốn hello-world-3, nhận %s", base)
	}

	// Gốc legacy được chọn riêng biệt: không dành riêng slug "novel" cho mình.
	legacy := filepath.Join(root, "novel")
	newNovel(t, legacy, "Novel Cũ", 0)
	n3, err := lib.Create("Novel")
	if err != nil {
		t.Fatalf("Create 3: %v", err)
	}
	if base := filepath.Base(n3.Dir); base != "novel" {
		t.Errorf("legacy không được dành riêng slug 'novel', nhận %s", base)
	}
	if _, err := os.Stat(filepath.Join(root, "novels", "novel", "meta", "progress.json")); err != nil {
		t.Errorf("truyện mới 'novel' phải được tạo trong novelsDir: %v", err)
	}

	got, err := lib.List()
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(got) != 5 {
		t.Errorf("kho phải có 5 truyện (1 legacy + 4 mới), nhận %d: %+v", len(got), got)
	}
}

func TestList_RoundTripAfterCreate(t *testing.T) {
	root := t.TempDir()
	lib := Open(filepath.Join(root, "novel"), filepath.Join(root, "novels"))
	created, err := lib.Create("Truyện Mới")
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	got, err := lib.List()
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("muốn 1 truyện, nhận %d", len(got))
	}
	if got[0].Name != created.Name || got[0].Dir != created.Dir || got[0].Legacy {
		t.Errorf("List sau Create không khớp: %+v vs %+v", got[0], created)
	}
}
