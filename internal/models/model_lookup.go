package models

import "strings"

// SameModelID kiểm tra xem hai định danh mô hình có trỏ đến cùng một mô hình chuẩn không (bỏ qua hậu tố ngày, chữ hoa/thường, sự khác biệt dấu chấm/gạch ngang).
func SameModelID(a, b string) bool {
	return modelLookupMatches(normalizeModelLookupID(a), normalizeModelLookupID(b))
}

// lookupModelEntry tìm mục theo "provider/model" bằng chỉ mục đã tiền xử lý,
// trả về chỉ số của mục trong r.models. Phải gọi khi đang giữ r.mu (RLock hoặc Lock).
// providerName rỗng → bỏ qua bộ lọc provider.
func (r *ModelRegistry) lookupModelEntry(providerName, modelID string) (int, bool) {
	providerName = strings.ToLower(strings.TrimSpace(providerName))
	targetID := normalizeModelLookupID(modelID)
	for i := range r.idx {
		e := &r.idx[i]
		if providerName != "" && !strings.EqualFold(e.lowerProvider, providerName) {
			continue
		}
		if modelLookupMatches(e.normID, targetID) {
			return i, true
		}
	}
	return -1, false
}

func normalizeModelLookupID(modelID string) string {
	modelID = strings.ToLower(strings.TrimSpace(modelID))
	return strings.ReplaceAll(modelID, ".", "-")
}

// modelLookupMatches khớp chính xác hoặc khớp có hậu tố ngày.
// Ví dụ: "claude-sonnet-4" khớp với "claude-sonnet-4-20250514".
func modelLookupMatches(knownID, targetID string) bool {
	if knownID == targetID {
		return true
	}
	if strings.HasPrefix(targetID, knownID) && isDatedModelSuffix(targetID[len(knownID):]) {
		return true
	}
	if strings.HasPrefix(knownID, targetID) && isDatedModelSuffix(knownID[len(targetID):]) {
		return true
	}
	return false
}

// isDatedModelSuffix kiểm tra xem chuỗi có dạng "-20250514" không (dấu gạch ngang + 8 chữ số).
func isDatedModelSuffix(s string) bool {
	if len(s) != 9 || s[0] != '-' {
		return false
	}
	for _, c := range s[1:] {
		if c < '0' || c > '9' {
			return false
		}
	}
	return true
}

func hasDatedSuffix(id string) bool {
	if len(id) < 9 {
		return false
	}
	return isDatedModelSuffix(id[len(id)-9:])
}
