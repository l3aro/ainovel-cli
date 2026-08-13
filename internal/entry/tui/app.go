package tui

import (
	"path/filepath"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/voocel/ainovel-cli/assets"
	"github.com/voocel/ainovel-cli/internal/bootstrap"
	"github.com/voocel/ainovel-cli/internal/host"
	"github.com/voocel/ainovel-cli/internal/library"
	"github.com/voocel/ainovel-cli/internal/logger"
)

// workspaceResult là kết quả trả về của chương trình Bubble Tea của một phiên làm việc.
type workspaceResult struct {
	openLibrary bool // true: phiên yêu cầu quay lại màn hình chọn truyện (mở kho lần nữa)
}

// Run khởi động TUI.
// Vòng lặp chọn/phiên: trước mỗi phiên làm việc chạy màn hình chọn truyện (pickNovel),
// chạy một phiên Bubble Tea trên truyện đã chọn; phiên kết thúc bình thường (Ctrl+C)
// thì thoát Run, chỉ khi phiên yêu cầu mở lại kho (openLibrary) mới lặp lại từ màn hình chọn.
// Quy ước phân tầng chế độ khởi động:
// 1. Chế độ nhanh, chế độ đồng sáng tác thuộc “biên soạn khởi động”;
// 2. Phiên sáng tác chính thức vào host.Host;
// 3. Tương lai nếu thêm các chế độ dùng chung như “tiếp tục tiểu thuyết có sẵn”, thống nhất đặt vào internal/entry/startup.
func Run(cfg bootstrap.Config, bundle assets.Bundle, version string) error {
	return runSelectionLoop(cfg, bundle, version, pickNovel, runWorkspace)
}

// runSelectionLoop vòng lặp chọn truyện → phiên làm việc. Trước mỗi phiên:
// điền mặc định config, suy legacyDir (gốc bố cục cũ) và novelsDir (thư mục chứa nhiều
// truyện, cạnh legacyDir), chạy pick để chọn truyện; chép config và chỉ đổi OutputDir
// sang thư mục truyện đã chọn. Mỗi phiên có Host/logger/cầu nối/Model mới hoàn toàn
// (không tái dùng kênh, cầu nối hay model); Host và logger được đóng trước khi pick
// lặp lại. pick trả về nil = người dùng thoát từ màn hình chọn; run trả về
// workspaceResult{openLibrary:false} = Ctrl+C bình thường, thoát Run.
func runSelectionLoop(
	cfg bootstrap.Config,
	bundle assets.Bundle,
	version string,
	pick func(cfg bootstrap.Config, legacyDir, novelsDir string) (*library.Novel, error),
	run func(cfg bootstrap.Config, bundle assets.Bundle, version string) (workspaceResult, error),
) error {
	for {
		cfg.FillDefaults()
		legacyDir := cfg.OutputDir
		novelsDir := filepath.Join(filepath.Dir(legacyDir), "novels")
		novel, err := pick(cfg, legacyDir, novelsDir)
		if err != nil {
			return err
		}
		if novel == nil {
			return nil // thoát từ màn hình chọn
		}
		wsCfg := cfg
		wsCfg.OutputDir = novel.Dir
		res, err := run(wsCfg, bundle, version)
		if err != nil {
			return err
		}
		if !res.openLibrary {
			return nil // Ctrl+C bình thường: thoát Run
		}
	}
}

// runWorkspace chạy một phiên làm việc trên một truyện: khởi tạo Host, cầu nối hỏi
// người dùng, logger, Model và chương trình Bubble Tea giống hệt trước đây; trả về
// kết quả phiên. Host và logger được đóng trước khi hàm trả về — tức trước khi màn
// hình chọn chạy lại ở vòng lặp tiếp theo.
func runWorkspace(cfg bootstrap.Config, bundle assets.Bundle, version string) (workspaceResult, error) {
	rt, err := host.New(cfg, bundle)
	if err != nil {
		return workspaceResult{}, err
	}
	bridge := newAskUserBridge()
	rt.AskUser().SetHandler(bridge.handler)
	cleanup := logger.SetupFile(rt.Dir(), "tui.log", false)
	defer cleanup()
	defer rt.Close()

	m := NewModel(rt, bridge, version)
	// Không bật báo cáo chuột toàn cục khi khởi động: trang chào mừng không cần chuột,
	// tắt báo cáo giúp giữ nguyên tính năng kéo-chọn-sao chép gốc của terminal.
	// Khi vào bàn làm việc sáng tác (modeRunning), enterRunning sẽ bật báo cáo,
	// để hỗ trợ nhấp chuyển panel / cuộn chuột / kéo thanh bên.
	p := tea.NewProgram(m, tea.WithAltScreen())
	final, err := p.Run()
	if err != nil {
		return workspaceResult{}, err
	}
	fm, ok := final.(Model)
	if !ok {
		return workspaceResult{}, nil
	}
	return workspaceResult{openLibrary: fm.openLibrary}, nil
}
