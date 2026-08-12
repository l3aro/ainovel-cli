package store

import (
	"os"

	"github.com/voocel/ainovel-cli/internal/stylestat"
)

// StyleStatsStore quản lý bộ đệm thống kê phong cách toàn tác phẩm (meta/style_stats.json).
// Thống kê chỉ thay đổi khi commit chương, nên commit lưu sẵn kết quả để novel_context tái sử dụng
// thay vì đọc lại toàn bộ sách và tính lại mỗi lần gọi.
type StyleStatsStore struct {
	io *IO
}

func NewStyleStatsStore(io *IO) *StyleStatsStore {
	return &StyleStatsStore{io: io}
}

// styleStatsFile là dữ liệu lưu tại meta/style_stats.json: vân tay nội dung của các đầu vào
// đã thống kê + kết quả thống kê tương ứng. Vân tay dùng để phát hiện bộ đệm cũ.
type styleStatsFile struct {
	Fingerprint string           `json:"fingerprint"`
	Stats       *stylestat.Stats `json:"stats"`
}

// Load đọc bộ đệm thống kê đã lưu. Trả về (nil, "", nil) khi chưa từng lưu.
func (s *StyleStatsStore) Load() (*stylestat.Stats, string, error) {
	var f styleStatsFile
	if err := s.io.ReadJSON("meta/style_stats.json", &f); err != nil {
		if os.IsNotExist(err) {
			return nil, "", nil
		}
		return nil, "", err
	}
	return f.Stats, f.Fingerprint, nil
}

// Save lưu bộ đệm thống kê mới nhất kèm vân tay nội dung tương ứng.
func (s *StyleStatsStore) Save(stats *stylestat.Stats, fingerprint string) error {
	return s.io.WithWriteLock(func() error {
		return s.io.WriteJSONUnlocked("meta/style_stats.json", styleStatsFile{
			Fingerprint: fingerprint,
			Stats:       stats,
		})
	})
}
