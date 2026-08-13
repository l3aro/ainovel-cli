package tui

import (
	"github.com/voocel/ainovel-cli/internal/bootstrap"
	"github.com/voocel/ainovel-cli/internal/library"
)

// pickNovel chọn truyện để mở trước mỗi phiên làm việc — điểm nối (seam) cho màn hình
// chọn truyện. Bước 3 sẽ thay phần thân bằng UI chọn thực tế (liệt kê kho qua
// library.List, chọn truyện, tạo mới); hiện tại giữ hành vi cũ: không hiện UI,
// luôn mở workspace legacy.
//
// legacyDir là gốc bố cục cũ (cfg.OutputDir sau FillDefaults), novelsDir là thư mục
// chứa nhiều truyện nằm cạnh legacyDir (filepath.Join(filepath.Dir(legacyDir), "novels")).
// Trả về nil khi người dùng thoát từ màn hình chọn — Run kết thúc.
func pickNovel(cfg bootstrap.Config, legacyDir, novelsDir string) (*library.Novel, error) {
	return &library.Novel{Dir: legacyDir, Legacy: true}, nil
}
