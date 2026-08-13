package tui

import (
	"testing"

	"github.com/voocel/ainovel-cli/internal/host"
)

func TestNovelsCommandRegisteredVisibleIdleOnly(t *testing.T) {
	registry := commandRegistryInstance()
	spec, ok := registry.Find("novels")
	if !ok {
		t.Fatal("expected /novels command to be registered")
	}
	if spec.Usage != "/novels" {
		t.Fatalf("usage = %q, want /novels", spec.Usage)
	}
	if spec.Group != "system" {
		t.Fatalf("group = %q, want system", spec.Group)
	}
	if spec.Description != "Quay về thư viện để chuyển tiểu thuyết" {
		t.Fatalf("description = %q, want the exact Vietnamese string", spec.Description)
	}
	if !spec.NeedsIdle {
		t.Fatal("expected /novels to require idle state")
	}
	if spec.Hidden {
		t.Fatal("expected /novels to be visible in palette")
	}
	if len(spec.Aliases) != 0 {
		t.Fatalf("expected /novels to have no aliases, got %v", spec.Aliases)
	}

	items := builtinCommandItems()
	var item *commandPaletteItem
	for i := range items {
		if items[i].Name == "novels" {
			item = &items[i]
			break
		}
	}
	if item == nil {
		t.Fatalf("expected /novels in palette items, got %+v", items)
	}
	if item.Usage != "/novels" {
		t.Fatalf("palette usage = %q, want /novels", item.Usage)
	}
	if item.Description != "Quay về thư viện để chuyển tiểu thuyết" {
		t.Fatalf("palette description = %q, want the exact Vietnamese string", item.Description)
	}
	if len(item.Aliases) != 0 {
		t.Fatalf("expected palette item to have no aliases, got %v", item.Aliases)
	}
}

func TestNovelsCommandQuitsAndRequestsLibrarySwitchWhenIdle(t *testing.T) {
	// runtime chứa Host rỗng: /novels KHÔNG được đóng Host (runWorkspace đóng bằng
	// defer rt.Close sau khi chương trình kết thúc). Host.Close trên Host rỗng sẽ
	// panic, nên nếu handler lỡ đóng Host thì test này vỡ.
	m := Model{runtime: &host.Host{}, snapshot: host.UISnapshot{IsRunning: false}, eventIndex: map[string]int{}}
	next, cmd := m.handleSlashCommand(slashCommand{name: "novels"})
	got := next.(Model)
	mustQuitCmd(t, cmd)
	if !got.openLibrary {
		t.Fatal("expected openLibrary=true so runSelectionLoop reopens the library")
	}
	if got.runtime != m.runtime {
		t.Fatal("handler must not replace the runtime")
	}
	if len(got.events) != 0 {
		t.Fatalf("expected no error events while idle, got %+v", got.events)
	}
}

func TestNovelsCommandBlockedWhileRunning(t *testing.T) {
	m := Model{snapshot: host.UISnapshot{IsRunning: true}, eventIndex: map[string]int{}}
	next, cmd := m.handleSlashCommand(slashCommand{name: "novels"})
	got := next.(Model)
	if cmd != nil {
		t.Fatal("expected no command while runtime is running (must not quit)")
	}
	if got.openLibrary {
		t.Fatal("must not switch to library while runtime is running")
	}
	if len(got.events) != 1 || got.events[0].Category != "ERROR" {
		t.Fatalf("expected NeedsIdle to emit one error, got %+v", got.events)
	}
	want := "Lệnh chỉ có thể thực thi khi ở trạng thái rảnh: /novels"
	if got.events[0].Summary != want {
		t.Fatalf("error summary = %q, want %q", got.events[0].Summary, want)
	}
}
