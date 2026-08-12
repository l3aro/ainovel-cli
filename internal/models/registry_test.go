package models

import "testing"

// TestResolveEveryEntry đảm bảo mọi mục trong bảng đều phân giải được bằng chính ID của nó.
func TestResolveEveryEntry(t *testing.T) {
	r := NewModelRegistry()
	for _, m := range r.models {
		e, ok := r.Resolve(m.ID)
		if !ok {
			t.Errorf("Resolve(%q) không tìm thấy mục đã đăng ký", m.ID)
			continue
		}
		// Resolve có thể trả về alias không hậu tố ngày thay vì chính mục đó,
		// nên so sánh theo chuẩn SameModelID thay vì chuỗi ID.
		if !SameModelID(e.ID, m.ID) {
			t.Errorf("Resolve(%q) = %q, không tương thích với mục đã đăng ký", m.ID, e.ID)
		}
	}
}

// TestResolveByNameSubstring đảm bảo chuỗi con của Name phân giải được sang một mục.
func TestResolveByNameSubstring(t *testing.T) {
	r := NewModelRegistry()
	for _, m := range r.models {
		if len(m.Name) < 2 {
			continue
		}
		pattern := m.Name[:len(m.Name)/2]
		if _, ok := r.Resolve(pattern); !ok {
			t.Errorf("Resolve(%q) (chuỗi con của Name %q) không phân giải được", pattern, m.Name)
		}
	}
}

// TestResolveProviderModel đảm bảo dạng "provider/model" phân giải được cho mọi mục.
func TestResolveProviderModel(t *testing.T) {
	r := NewModelRegistry()
	for _, m := range r.models {
		pattern := m.Provider + "/" + m.ID
		e, ok := r.Resolve(pattern)
		if !ok {
			t.Errorf("Resolve(%q) không phân giải được", pattern)
			continue
		}
		if !SameModelID(e.ID, m.ID) {
			t.Errorf("Resolve(%q) = %q, không tương thích", pattern, e.ID)
		}
	}
}

// TestResolveVendorPrefixFallback đảm bảo tiền tố vendor OpenRouter (google/, x-ai/)
// rơi xuống nhánh chỉ theo modelID khi không khớp Provider nội bộ.
func TestResolveVendorPrefixFallback(t *testing.T) {
	r := NewModelRegistry()
	geminiID := ""
	for _, m := range r.models {
		if m.Provider == "gemini" {
			geminiID = m.ID
			break
		}
	}
	if geminiID == "" {
		t.Skip("bảng không có mô hình gemini")
	}
	e, ok := r.Resolve("google/" + geminiID)
	if !ok {
		t.Fatalf("Resolve(google/%s) không phân giải được", geminiID)
	}
	if !SameModelID(e.ID, geminiID) {
		t.Errorf("Resolve(google/%s) = %q, muốn mục gemini %s", geminiID, e.ID, geminiID)
	}
}

// TestResolveEmptyAndTrimmed đảm bảo pattern rỗng trả nil, false và pattern
// có khoảng trắng/chữ hoa hai đầu vẫn phân giải được (khớp chính xác, không phân biệt hoa thường).
func TestResolveEmptyAndTrimmed(t *testing.T) {
	r := NewModelRegistry()
	if e, ok := r.Resolve("  "); ok || e != nil {
		t.Errorf("Resolve(\"  \") = %v, %v — muốn nil, false", e, ok)
	}
	if len(r.models) == 0 {
		t.Fatal("bảng mô hình rỗng")
	}
	id := r.models[0].ID
	if _, ok := r.Resolve("  " + id + "  "); !ok {
		t.Error("Resolve với pattern có khoảng trắng hai đầu không phân giải được")
	}
	upper := ""
	for _, c := range id {
		if c >= 'a' && c <= 'z' {
			upper += string(c - ('a' - 'A'))
		} else {
			upper += string(c)
		}
	}
	if _, ok := r.Resolve(upper); !ok {
		t.Error("Resolve với pattern chữ hoa không phân giải được")
	}
}

// TestMergeModelsRebuildsIndex đảm bảo mô hình mới hợp nhất lúc chạy vẫn phân giải được
// (chỉ mục phải được dựng lại sau khi MergeModels đột biến r.models).
func TestMergeModelsRebuildsIndex(t *testing.T) {
	r := NewModelRegistry()
	before := len(r.models)
	merged := ModelEntry{
		Provider:       "acme",
		ID:             "nova-x1",
		Name:           "Acme Nova X1",
		ContextWindow:  123456,
		MaxTokens:      8192,
		InputCostPer1M: 1.5,
	}
	r.MergeModels([]ModelEntry{merged})

	e, ok := r.Resolve("acme/nova-x1")
	if !ok {
		t.Fatalf("Resolve(%q) sau MergeModels không phân giải được — chỉ mục chưa được dựng lại", "acme/nova-x1")
	}
	if e.ContextWindow != 123456 {
		t.Errorf("Resolve sau MergeModels trả ContextWindow = %d, muốn 123456", e.ContextWindow)
	}
	// Chuỗi con theo Name mới cũng phải phân giải được sau khi hợp nhất.
	if _, ok := r.Resolve("Acme Nova"); !ok {
		t.Error("Resolve(\"Acme Nova\") sau MergeModels không phân giải được")
	}
	// MergeModels không được làm mất mục baseline.
	if len(r.models) != before+1 {
		t.Errorf("len(models) = %d, muốn %d", len(r.models), before+1)
	}
}
