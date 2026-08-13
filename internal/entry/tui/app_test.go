package tui

import (
	"errors"
	"testing"

	"github.com/voocel/ainovel-cli/assets"
	"github.com/voocel/ainovel-cli/internal/bootstrap"
	"github.com/voocel/ainovel-cli/internal/library"
)

// fakePick/fakeRun ghi lại lời gọi và trả kết quả theo kịch bản.
type pickCall struct {
	legacyDir string
	novelsDir string
}
type runCall struct {
	cfg     bootstrap.Config
	bundle  assets.Bundle
	version string
}

func TestRunSelectionLoop_NormalExit(t *testing.T) {
	var picks []pickCall
	var runs []runCall
	err := runSelectionLoop(
		bootstrap.Config{OutputDir: "out/legacy"},
		assets.Bundle{},
		"v-test",
		func(cfg bootstrap.Config, legacyDir, novelsDir string) (*library.Novel, error) {
			picks = append(picks, pickCall{legacyDir, novelsDir})
			return &library.Novel{Name: "Truyện A", Dir: "out/novels/truyen-a"}, nil
		},
		func(cfg bootstrap.Config, bundle assets.Bundle, version string) (workspaceResult, error) {
			runs = append(runs, runCall{cfg, bundle, version})
			return workspaceResult{}, nil // Ctrl+C bình thường
		},
	)
	if err != nil {
		t.Fatalf("Ctrl+C bình thường phải thoát Run với nil error, nhận %v", err)
	}
	if len(picks) != 1 || len(runs) != 1 {
		t.Fatalf("mong muốn đúng 1 lượt chọn + 1 phiên, nhận %d lượt chọn, %d phiên", len(picks), len(runs))
	}
	if got := runs[0].cfg.OutputDir; got != "out/novels/truyen-a" {
		t.Errorf("phiên phải chạy trên thư mục truyện đã chọn, nhận %q", got)
	}
	if runs[0].version != "v-test" {
		t.Errorf("version phải truyền nguyên vẹn, nhận %q", runs[0].version)
	}
}

func TestRunSelectionLoop_OpenLibraryLoops(t *testing.T) {
	var picks []pickCall
	var runs []runCall
	err := runSelectionLoop(
		bootstrap.Config{OutputDir: "out/legacy"},
		assets.Bundle{},
		"v-test",
		func(cfg bootstrap.Config, legacyDir, novelsDir string) (*library.Novel, error) {
			picks = append(picks, pickCall{legacyDir, novelsDir})
			return &library.Novel{Name: "Truyện B", Dir: "out/novels/truyen-b"}, nil
		},
		func(cfg bootstrap.Config, bundle assets.Bundle, version string) (workspaceResult, error) {
			runs = append(runs, runCall{cfg, bundle, version})
			if len(runs) < 2 {
				return workspaceResult{openLibrary: true}, nil // mở lại kho
			}
			return workspaceResult{}, nil // lần sau Ctrl+C bình thường
		},
	)
	if err != nil {
		t.Fatalf("vòng lặp kết thúc bình thường phải trả nil error, nhận %v", err)
	}
	if len(picks) != 2 || len(runs) != 2 {
		t.Fatalf("openLibrary phải lặp lại đúng 1 lần (2 lượt chọn, 2 phiên), nhận %d lượt chọn, %d phiên", len(picks), len(runs))
	}
	for i, p := range picks {
		if p.legacyDir != "out/legacy" || p.novelsDir != "out/novels" {
			t.Errorf("lượt chọn %d phải nhận legacyDir=out/legacy, novelsDir=out/novels, nhận %+v", i, p)
		}
	}
	if got := runs[1].cfg.OutputDir; got != "out/novels/truyen-b" {
		t.Errorf("phiên thứ 2 phải chạy trên thư mục truyện đã chọn, nhận %q", got)
	}
}

func TestRunSelectionLoop_PickNilExits(t *testing.T) {
	runs := 0
	err := runSelectionLoop(
		bootstrap.Config{OutputDir: "out/legacy"},
		assets.Bundle{},
		"v-test",
		func(cfg bootstrap.Config, legacyDir, novelsDir string) (*library.Novel, error) {
			return nil, nil // thoát từ màn hình chọn
		},
		func(cfg bootstrap.Config, bundle assets.Bundle, version string) (workspaceResult, error) {
			runs++
			return workspaceResult{}, nil
		},
	)
	if err != nil {
		t.Fatalf("thoát từ màn hình chọn phải trả nil error, nhận %v", err)
	}
	if runs != 0 {
		t.Fatalf("không được chạy phiên nào khi thoát từ màn hình chọn, nhận %d", runs)
	}
}

func TestRunSelectionLoop_PickError(t *testing.T) {
	want := errors.New("lỗi chọn")
	runs := 0
	err := runSelectionLoop(
		bootstrap.Config{OutputDir: "out/legacy"},
		assets.Bundle{},
		"v-test",
		func(cfg bootstrap.Config, legacyDir, novelsDir string) (*library.Novel, error) {
			return nil, want
		},
		func(cfg bootstrap.Config, bundle assets.Bundle, version string) (workspaceResult, error) {
			runs++
			return workspaceResult{}, nil
		},
	)
	if !errors.Is(err, want) {
		t.Fatalf("lỗi màn hình chọn phải được truyền ra, nhận %v", err)
	}
	if runs != 0 {
		t.Fatalf("không được chạy phiên nào khi chọn lỗi, nhận %d", runs)
	}
}

func TestRunSelectionLoop_RunError(t *testing.T) {
	want := errors.New("lỗi phiên")
	picks := 0
	err := runSelectionLoop(
		bootstrap.Config{OutputDir: "out/legacy"},
		assets.Bundle{},
		"v-test",
		func(cfg bootstrap.Config, legacyDir, novelsDir string) (*library.Novel, error) {
			picks++
			return &library.Novel{Dir: "out/novels/truyen-a"}, nil
		},
		func(cfg bootstrap.Config, bundle assets.Bundle, version string) (workspaceResult, error) {
			return workspaceResult{}, want
		},
	)
	if !errors.Is(err, want) {
		t.Fatalf("lỗi phiên phải được truyền ra, nhận %v", err)
	}
	if picks != 1 {
		t.Fatalf("lỗi phiên phải dừng vòng lặp sau 1 lượt chọn, nhận %d", picks)
	}
}

func TestRunSelectionLoop_FillDefaultsBeforeDerive(t *testing.T) {
	// OutputDir rỗng: FillDefaults điền output/novel trước khi suy legacyDir/novelsDir.
	legacyDir, novelsDir := "", ""
	err := runSelectionLoop(
		bootstrap.Config{},
		assets.Bundle{},
		"v-test",
		func(cfg bootstrap.Config, l, n string) (*library.Novel, error) {
			legacyDir, novelsDir = l, n
			return &library.Novel{Dir: l}, nil
		},
		func(cfg bootstrap.Config, bundle assets.Bundle, version string) (workspaceResult, error) {
			return workspaceResult{}, nil
		},
	)
	if err != nil {
		t.Fatalf("không mong đợi lỗi, nhận %v", err)
	}
	if legacyDir != "output/novel" || novelsDir != "output/novels" {
		t.Errorf("FillDefaults phải chạy trước khi suy thư mục: nhận legacyDir=%q novelsDir=%q", legacyDir, novelsDir)
	}
}
