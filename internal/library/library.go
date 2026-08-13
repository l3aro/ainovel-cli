// Package library cung cấp API kho tiểu thuyết: liệt kê và tạo mới các truyện.
//
// Kho hợp nhất hai bố cục thư mục:
//   - legacyDir — bố cục cũ (mặc định output/novel): bản thân thư mục là gốc
//     store của MỘT tiểu thuyết (meta/, chapters/, … nằm trực tiếp trong đó).
//   - novelsDir — bố cục mới: thư mục chứa nhiều tiểu thuyết, mỗi tiểu thuyết
//     là một thư mục con trực tiếp (output/novels/<slug>/).
//
// Trạng thái của một truyện nằm trong meta/progress.json (store.Progress);
// gốc chưa có file này được coi là chưa khởi tạo và bị List bỏ qua.
package library

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"unicode"

	"github.com/voocel/ainovel-cli/internal/domain"
	"github.com/voocel/ainovel-cli/internal/store"
)

// Novel mô tả một tiểu thuyết trong kho.
type Novel struct {
	Name              string       // tên hiển thị: NovelName trong progress, dự phòng tên thư mục
	Dir               string       // đường dẫn thư mục gốc của truyện
	Phase             domain.Phase // giai đoạn sáng tác hiện tại
	CompletedChapters int          // số chương đã hoàn thành
	TotalWordCount    int          // tổng số từ đã viết
	Legacy            bool         // true: truyện là gốc store cũ (legacyDir)
}

// Library là kho tiểu thuyết hợp nhất giữa bố cục cũ (legacyDir) và mới (novelsDir).
type Library struct {
	legacyDir string
	novelsDir string
}

// Open tạo Library từ hai gốc thư mục. Không đọc đĩa tại thời điểm này;
// lỗi IO chỉ xuất hiện ở List/Create.
func Open(legacyDir, novelsDir string) *Library {
	return &Library{legacyDir: legacyDir, novelsDir: novelsDir}
}

// List liệt kê toàn bộ tiểu thuyết trong kho: truyện legacy (nếu có) trước,
// rồi các thư mục con trực tiếp của novelsDir, trong nhóm mới sắp xếp theo
// tên không phân biệt hoa thường, giữ thứ tự ổn định khi trùng khóa. Gốc
// thiếu meta/progress.json bị bỏ qua; progress hỏng hoặc không đọc được trả
// về lỗi có kèm tên thư mục. Thuần đọc: không tạo/sửa gì trên đĩa.
func (l *Library) List() ([]Novel, error) {
	var novels []Novel
	if legacy, err := l.loadLegacy(); err != nil {
		return nil, err
	} else if legacy != nil {
		novels = append(novels, *legacy)
	}
	modern, err := l.scanNovels()
	if err != nil {
		return nil, err
	}
	return append(novels, modern...), nil
}

// loadLegacy đọc truyện cũ trực tiếp từ legacyDir; trả về nil nếu gốc chưa
// khởi tạo (thiếu meta/progress.json) hoặc không tồn tại.
func (l *Library) loadLegacy() (*Novel, error) {
	p, err := store.NewStore(l.legacyDir).Progress.Load()
	if err != nil {
		return nil, fmt.Errorf("library: đọc progress %s: %w", l.legacyDir, err)
	}
	if p == nil {
		return nil, nil
	}
	name := strings.TrimSpace(p.NovelName)
	if name == "" {
		name = filepath.Base(l.legacyDir)
	}
	return &Novel{
		Name:              name,
		Dir:               l.legacyDir,
		Phase:             p.Phase,
		CompletedChapters: len(p.CompletedChapters),
		TotalWordCount:    p.TotalWordCount,
		Legacy:            true,
	}, nil
}

// scanNovels liệt kê các thư mục con trực tiếp của novelsDir đã khởi tạo,
// sắp theo tên không phân biệt hoa thường (ổn định).
func (l *Library) scanNovels() ([]Novel, error) {
	entries, err := os.ReadDir(l.novelsDir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("library: đọc thư mục %s: %w", l.novelsDir, err)
	}
	novels := make([]Novel, 0, len(entries))
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		dir := filepath.Join(l.novelsDir, e.Name())
		p, err := store.NewStore(dir).Progress.Load()
		if err != nil {
			return nil, fmt.Errorf("library: đọc progress %s: %w", dir, err)
		}
		if p == nil {
			continue // chưa khởi tạo: thiếu meta/progress.json
		}
		name := strings.TrimSpace(p.NovelName)
		if name == "" {
			name = e.Name()
		}
		novels = append(novels, Novel{
			Name:              name,
			Dir:               dir,
			Phase:             p.Phase,
			CompletedChapters: len(p.CompletedChapters),
			TotalWordCount:    p.TotalWordCount,
			Legacy:            false,
		})
	}
	sort.SliceStable(novels, func(i, j int) bool {
		return strings.ToLower(novels[i].Name) < strings.ToLower(novels[j].Name)
	})
	return novels, nil
}

// Create tạo tiểu thuyết mới trong novelsDir và trả về Novel tương ứng.
// Tên được trim; tên rỗng hoặc chứa ký tự điều khiển bị từ chối. Thư mục đích
// là slug ASCII thường của tên (giữ chữ cái/chữ số, mọi chuỗi khác gộp thành
// một dấu '-', cắt dấu '-' ở hai đầu); slug rỗng dự phòng "novel". Khi slug
// trùng với truyện đã có trong novelsDir (hoặc với chính gốc legacy đã khởi
// tạo), thêm hậu tố -2, -3, … Khởi tạo cấu trúc store rồi ghi progress với tên
// gốc đã trim (mỗi việc đúng một lần); không tạo manifest.
func (l *Library) Create(name string) (Novel, error) {
	name = strings.TrimSpace(name)
	if name == "" {
		return Novel{}, errors.New("library: tên tiểu thuyết không được để trống")
	}
	if strings.ContainsFunc(name, unicode.IsControl) {
		return Novel{}, errors.New("library: tên tiểu thuyết chứa ký tự điều khiển")
	}
	dir := filepath.Join(l.novelsDir, l.uniqueSlug(name))
	s := store.NewStore(dir)
	if err := s.Init(); err != nil {
		return Novel{}, fmt.Errorf("library: khởi tạo thư mục %s: %w", dir, err)
	}
	if err := s.Progress.Init(name, 0); err != nil {
		return Novel{}, fmt.Errorf("library: khởi tạo progress %s: %w", dir, err)
	}
	return Novel{Name: name, Dir: dir, Phase: domain.PhaseInit, Legacy: false}, nil
}

// slug chuyển tên thành slug ASCII thường, quyết định và đơn điệu:
// giữ [a-z0-9], mọi chuỗi ký tự khác gộp thành một dấu '-', bỏ dấu '-' hai đầu;
// trả về "novel" khi kết quả rỗng.
func slug(name string) string {
	var b strings.Builder
	dash := false
	for _, r := range strings.ToLower(name) {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') {
			b.WriteRune(r)
			dash = false
			continue
		}
		if !dash {
			b.WriteByte('-')
			dash = true
		}
	}
	s := strings.Trim(b.String(), "-")
	if s == "" {
		return "novel"
	}
	return s
}

// uniqueSlug chọn slug chưa bị chiếm trong novelsDir; gắn hậu tố -2, -3, …
// cho tới khi hết trùng. Gốc legacy được chọn/liệt kê riêng biệt nên không
// dành riêng slug nào — kể cả "novel".
func (l *Library) uniqueSlug(name string) string {
	base := slug(name)
	cand := base
	for i := 2; dirExists(filepath.Join(l.novelsDir, cand)); i++ {
		cand = fmt.Sprintf("%s-%d", base, i)
	}
	return cand
}

// dirExists kiểm tra đường dẫn đã tồn tại chưa.
func dirExists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}
