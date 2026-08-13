package bootstrap

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/voocel/ainovel-cli/internal/errs"
)

const validGlobal = `{
  "provider": "openrouter",
  "model": "google/gemini-2.5-flash",
  "providers": { "openrouter": { "api_key": "sk-test-123456" } }
}`

// writeGlobal ghi cấu hình toàn cục vào HOME riêng biệt và trả về HOME đó.
func writeGlobal(t *testing.T, content string) string {
	t.Helper()
	home := t.TempDir()
	t.Setenv("HOME", home)
	dir := filepath.Join(home, ".ainovel")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if content != "" {
		if err := os.WriteFile(filepath.Join(dir, "config.json"), []byte(content), 0o644); err != nil {
			t.Fatalf("write global: %v", err)
		}
	}
	return home
}

// writeProjectConfig ghi cấu hình cấp dự án vào ./.ainovel/ trong thư mục làm việc hiện tại.
// Cần gọi t.Chdir đến thư mục đích trước khi gọi hàm này.
func writeProjectConfig(t *testing.T, content string) {
	t.Helper()
	if err := os.MkdirAll(".ainovel", 0o755); err != nil {
		t.Fatalf("mkdir .ainovel: %v", err)
	}
	if err := os.WriteFile(filepath.Join(".ainovel", "config.json"), []byte(content), 0o644); err != nil {
		t.Fatalf("write project: %v", err)
	}
}

// Nguyên nhân gốc 3: ./.ainovel/config.json cấp dự án tồn tại nhưng là JSON lỗi,
// phải báo lỗi, không được âm thầm bỏ qua và rút về cấu hình toàn cục.
func TestLoadConfig_CorruptProjectFailsLoud(t *testing.T) {
	writeGlobal(t, validGlobal)
	proj := t.TempDir()
	t.Chdir(proj)
	// Ví dụ chép tay thừa dấu phẩy cuối — lỗi JSON phổ biến nhất.
	writeProjectConfig(t, `{ "model": "x", }`)

	if _, err := LoadConfig(""); err == nil {
		t.Fatal("./.ainovel/config.json lỗi phải báo lỗi, nhưng đã bị âm thầm bỏ qua")
	}
}

// Toàn cục là nền có mức ưu tiên thấp nhất: file lỗi không được chặn ghi đè --config có mức ưu tiên cao hơn
// (kiểm tra hồi quy — phiên bản trước lầm lẫn khi áp fail-loud cho cả toàn cục,
// khiến người dùng có "toàn cục lỗi + --config hợp lệ" bị chặn bởi file không liên quan).
func TestLoadConfig_CorruptGlobalDoesNotBlockOverride(t *testing.T) {
	writeGlobal(t, `{ not json`)
	proj := t.TempDir()
	t.Chdir(proj)
	good := filepath.Join(proj, "good.json")
	if err := os.WriteFile(good, []byte(validGlobal), 0o644); err != nil {
		t.Fatalf("write override: %v", err)
	}

	cfg, err := LoadConfig(good)
	if err != nil {
		t.Fatalf("toàn cục lỗi không được chặn --config hợp lệ, nhận được: %v", err)
	}
	if cfg.Provider != "openrouter" {
		t.Errorf("phải dùng giá trị từ --config, nhận được provider=%q", cfg.Provider)
	}
}

// File không tồn tại là trường hợp bình thường (portable/lần đầu dùng), không được báo lỗi.
func TestLoadConfig_MissingFilesNoError(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home) // ~/.ainovel/config.json không tồn tại
	t.Chdir(t.TempDir())   // cũng không có ./.ainovel/config.json

	if _, err := LoadConfig(""); err != nil {
		t.Fatalf("file cấu hình thiếu không được báo lỗi, nhận được: %v", err)
	}
}

// Đường dẫn bình thường: toàn cục + cấp dự án được hợp nhất và có hiệu lực.
func TestLoadConfig_ValidMergeWorks(t *testing.T) {
	writeGlobal(t, validGlobal)
	proj := t.TempDir()
	t.Chdir(proj)
	writeProjectConfig(t, `{ "model": "google/gemini-2.5-pro" }`)

	cfg, err := LoadConfig("")
	if err != nil {
		t.Fatalf("cấu hình hợp lệ không được báo lỗi: %v", err)
	}
	if cfg.Provider != "openrouter" {
		t.Errorf("provider phải giữ giá trị toàn cục openrouter, nhận được %q", cfg.Provider)
	}
	if cfg.ModelName != "google/gemini-2.5-pro" {
		t.Errorf("model phải được ghi đè bởi cấp dự án, nhận được %q", cfg.ModelName)
	}
}

// env: cho phép tham chiếu biến môi trường trong giá trị chuỗi cấu hình
// (api_key/base_url/type/model/...); biến chưa đặt hoặc rỗng phải báo lỗi config.
func TestLoadConfig_EnvRefExpanded(t *testing.T) {
	writeGlobal(t, `{
  "provider": "openrouter",
  "model": "google/gemini-2.5-flash",
  "providers": {
    "openrouter": {
      "api_key": "env:TEST_AI_KEY",
      "base_url": "env:TEST_AI_BASE",
      "extra_body": { "temperature": 0.8, "user_id": "env:TEST_BODY_USER" },
      "extra": { "headers": { "X-API-Key": "env:TEST_HEADER_KEY" } }
    }
  }
}`)
	t.Setenv("TEST_AI_KEY", "sk-env-123456")
	t.Setenv("TEST_AI_BASE", "https://env.example.com/v1")
	t.Setenv("TEST_HEADER_KEY", "hdr-env-789")
	t.Setenv("TEST_BODY_USER", "user-env-42")
	proj := t.TempDir()
	t.Chdir(proj)

	cfg, err := LoadConfig("")
	if err != nil {
		t.Fatalf("cấu hình env hợp lệ không được báo lỗi: %v", err)
	}
	pc := cfg.Providers["openrouter"]
	if pc.APIKey != "sk-env-123456" {
		t.Errorf("api_key phải được phân giải từ biến môi trường, nhận được %q", pc.APIKey)
	}
	if pc.BaseURL != "https://env.example.com/v1" {
		t.Errorf("base_url phải được phân giải từ biến môi trường, nhận được %q", pc.BaseURL)
	}
	ref, ok := cfg.EnvRefs["providers.openrouter.api_key"]
	if !ok || ref.raw != "env:TEST_AI_KEY" || ref.resolved != "sk-env-123456" {
		t.Errorf("EnvRefs phải ghi chú tham chiếu api_key, nhận được %#v (ok=%v)", ref, ok)
	}
	// Giá trị chuỗi lồng trong extra/extra_body cũng được phân giải (khóa map không đổi)
	headers, ok := pc.Extra["headers"].(map[string]any)
	if !ok {
		t.Fatalf("extra.headers thiếu hoặc sai kiểu: %#v", pc.Extra["headers"])
	}
	if got := headers["X-API-Key"]; got != "hdr-env-789" {
		t.Errorf("extra.headers[X-API-Key] phải được phân giải từ biến môi trường, nhận được %#v", got)
	}
	if got := pc.ExtraBody["temperature"]; got != 0.8 {
		t.Errorf("giá trị không phải chuỗi phải giữ nguyên, extra_body[temperature] = %#v", got)
	}
	if got := pc.ExtraBody["user_id"]; got != "user-env-42" {
		t.Errorf("extra_body[user_id] phải được phân giải từ biến môi trường, nhận được %#v", got)
	}
	if _, ok := cfg.EnvRefs["providers.openrouter.extra.headers.X-API-Key"]; !ok {
		t.Error("EnvRefs phải ghi chú tham chiếu trong extra.headers")
	}
	if _, ok := cfg.EnvRefs["providers.openrouter.extra_body.user_id"]; !ok {
		t.Error("EnvRefs phải ghi chú tham chiếu trong extra_body")
	}
}

// env: trỏ tới biến chưa đặt hoặc rỗng phải báo lỗi kèm tên biến — không được
// âm thầm dùng giá trị rỗng (cùng nguyên tắc fail loud của issue #37).
func TestLoadConfig_EnvRefUnsetFailsLoud(t *testing.T) {
	writeGlobal(t, `{
  "provider": "openrouter",
  "model": "google/gemini-2.5-flash",
  "providers": { "openrouter": { "api_key": "env:TEST_AI_KEY_NOT_SET" } }
}`)
	t.Setenv("TEST_AI_KEY_NOT_SET", "") // rỗng tương đương chưa đặt
	proj := t.TempDir()
	t.Chdir(proj)

	_, err := LoadConfig("")
	if err == nil {
		t.Fatal("biến môi trường rỗng phải báo lỗi, không được âm thầm dùng giá trị rỗng")
	}
	if !errors.Is(err, errs.ErrConfig) {
		t.Errorf("phải bọc errs.ErrConfig, nhận được: %v", err)
	}
	if !contains(err.Error(), "TEST_AI_KEY_NOT_SET") {
		t.Errorf("lỗi phải nêu tên biến để truy vết, nhận được: %v", err)
	}
}

func TestLoadConfig_EnvRefEmptyNameFails(t *testing.T) {
	writeGlobal(t, `{
  "provider": "openrouter",
  "model": "google/gemini-2.5-flash",
  "providers": { "openrouter": { "api_key": "env:" } }
}`)
	proj := t.TempDir()
	t.Chdir(proj)

	if _, err := LoadConfig(""); err == nil {
		t.Fatal("env: thiếu tên biến phải báo lỗi")
	}
}

// env: chỉ được phân giải sau khi hợp nhất các cấp: cấp dự án ghi đè bằng giá trị cụ thể
// thì thắng, kể cả khi tham chiếu env: của cấp toàn cục trỏ tới biến không tồn tại.
func TestLoadConfig_EnvRefOverriddenByLiteral(t *testing.T) {
	writeGlobal(t, `{
  "provider": "openrouter",
  "model": "google/gemini-2.5-flash",
  "providers": { "openrouter": { "api_key": "env:TEST_AI_KEY_NOT_SET" } }
}`)
	proj := t.TempDir()
	t.Chdir(proj)
	writeProjectConfig(t, `{ "providers": { "openrouter": { "api_key": "sk-project-456" } } }`)

	cfg, err := LoadConfig("")
	if err != nil {
		t.Fatalf("ghi đè bằng giá trị cụ thể phải qua được env-ref toàn cục lỗi: %v", err)
	}
	if got := cfg.Providers["openrouter"].APIKey; got != "sk-project-456" {
		t.Errorf("api_key phải là giá trị cấp dự án, nhận được %q", got)
	}
}

// SaveConfig phải khôi phục dạng "env:NAME" (không ghi bí mật đã phân giải ra đĩa),
// giữ nguyên giá trị trường đã bị đổi từ lúc tải (ví dụ /model), và không làm đổi
// cấu hình đang chạy của bên gọi.
func TestSaveConfig_RestoresEnvRefs(t *testing.T) {
	writeGlobal(t, `{
  "provider": "openrouter",
  "model": "google/gemini-2.5-flash",
  "providers": {
    "openrouter": {
      "api_key": "env:TEST_AI_KEY",
      "extra_body": { "user_id": "env:TEST_BODY_USER" },
      "extra": { "headers": { "X-API-Key": "env:TEST_HEADER_KEY" } }
    }
  }
}`)
	t.Setenv("TEST_AI_KEY", "sk-env-123456")
	t.Setenv("TEST_HEADER_KEY", "hdr-env-789")
	t.Setenv("TEST_BODY_USER", "user-env-42")
	proj := t.TempDir()
	t.Chdir(proj)

	cfg, err := LoadConfig("")
	if err != nil {
		t.Fatalf("tải cấu hình env: %v", err)
	}
	cfg.ModelName = "google/gemini-3.0-pro" // mô phỏng /model đổi model
	path := filepath.Join(proj, "saved.json")
	if err := SaveConfig(path, cfg); err != nil {
		t.Fatalf("SaveConfig: %v", err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("đọc file đã lưu: %v", err)
	}
	if !contains(string(data), `"api_key": "env:TEST_AI_KEY"`) {
		t.Errorf("file đã lưu phải giữ dạng env: (không rò rỉ bí mật), nội dung: %s", data)
	}
	if !contains(string(data), `"X-API-Key": "env:TEST_HEADER_KEY"`) {
		t.Errorf("file đã lưu phải giữ dạng env: cho extra.headers, nội dung: %s", data)
	}
	if !contains(string(data), `"user_id": "env:TEST_BODY_USER"`) {
		t.Errorf("file đã lưu phải giữ dạng env: cho extra_body, nội dung: %s", data)
	}
	if !contains(string(data), `"model": "google/gemini-3.0-pro"`) {
		t.Errorf("model mới phải được lưu nguyên vẹn, nội dung: %s", data)
	}
	if strings.Contains(string(data), "sk-env-123456") || strings.Contains(string(data), "hdr-env-789") || strings.Contains(string(data), "user-env-42") {
		t.Errorf("bí mật đã phân giải không được ghi xuống đĩa: %s", data)
	}
	// Cấu hình đang chạy của bên gọi không được đổi
	if got := cfg.Providers["openrouter"].APIKey; got != "sk-env-123456" {
		t.Errorf("SaveConfig không được đổi api_key của cấu hình bên gọi, nhận được %q", got)
	}
	headers, ok := cfg.Providers["openrouter"].Extra["headers"].(map[string]any)
	if !ok || headers["X-API-Key"] != "hdr-env-789" {
		t.Errorf("SaveConfig không được đổi extra.headers của cấu hình bên gọi, nhận được %#v", cfg.Providers["openrouter"].Extra["headers"])
	}
	if _, ok := cfg.EnvRefs["providers.openrouter.api_key"]; !ok {
		t.Error("SaveConfig không được xóa EnvRefs của cấu hình bên gọi")
	}
}

func TestMergeConfig_ProviderExtraFields(t *testing.T) {
	base := Config{
		Provider:  "openrouter",
		ModelName: "google/gemini-2.5-flash",
		Providers: map[string]ProviderConfig{
			"openrouter": {
				APIKey: "sk-test-123456",
				ExtraBody: map[string]any{
					"temperature": 0.8,
				},
				Extra: map[string]any{
					"user_agent": "base-client/1.0",
				},
			},
		},
	}
	overlay := Config{
		Providers: map[string]ProviderConfig{
			"openrouter": {
				BaseURL: "https://proxy.example.com/v1",
				ExtraBody: map[string]any{
					"min_p": 0.05,
				},
				Extra: map[string]any{
					"user_agent": "override-client/1.0",
					"headers": map[string]any{
						"X-Custom-Client": "ainovel",
					},
				},
			},
		},
	}

	cfg := mergeConfig(base, overlay)
	pc := cfg.Providers["openrouter"]
	if pc.APIKey != "sk-test-123456" {
		t.Fatalf("APIKey = %q, want inherited key", pc.APIKey)
	}
	if pc.BaseURL != "https://proxy.example.com/v1" {
		t.Fatalf("BaseURL = %q, want overlay URL", pc.BaseURL)
	}
	if _, ok := pc.ExtraBody["temperature"]; ok {
		t.Fatalf("ExtraBody should be replaced by overlay, got %#v", pc.ExtraBody)
	}
	if got := pc.ExtraBody["min_p"]; got != 0.05 {
		t.Fatalf("ExtraBody[min_p] = %#v, want 0.05", got)
	}
	if got := pc.Extra["user_agent"]; got != "override-client/1.0" {
		t.Fatalf("Extra[user_agent] = %#v, want override-client/1.0", got)
	}
	headers, ok := pc.Extra["headers"].(map[string]any)
	if !ok {
		t.Fatalf("Extra[headers] missing or invalid: %#v", pc.Extra["headers"])
	}
	if got := headers["X-Custom-Client"]; got != "ainovel" {
		t.Fatalf("Extra.headers[X-Custom-Client] = %#v, want ainovel", got)
	}
}

// Nguyên nhân gốc 2 (tái hiện cốt lõi issue #37): cấp dự án ghi đè provider nhưng không khai báo
// thông tin xác thực providers tương ứng — ValidateBase phải báo lỗi config
// (thay vì cho qua rồi crash ở tầng sâu hơn).
func TestValidateBase_ProviderOverrideWithoutCredentials(t *testing.T) {
	cfg := Config{
		Provider:  "mimo",
		ModelName: "mimo-v2.5-pro",
		Providers: map[string]ProviderConfig{
			"openrouter": {APIKey: "sk-test-123456"},
		},
	}
	cfg.FillDefaults()
	err := cfg.ValidateBase()
	if err == nil {
		t.Fatal("provider thiếu thông tin xác thực phải báo lỗi")
	}
	if !errors.Is(err, errs.ErrConfig) {
		t.Errorf("phải bọc errs.ErrConfig, nhận được: %v", err)
	}
}

// File ví dụ nội trang (config.example.jsonc qua go:embed) phải tự nhất quán:
// sau khi bỏ comment phải là JSON hợp lệ, con trỏ provider cấp cao nhất không được treo lơ lửng,
// và phải giải thích rõ tư duy "con trỏ" — đây là mẫu người dùng sẽ chép, nếu chính nó lỗi sẽ gây hại.
func TestExampleConfigIsValidAndSelfConsistent(t *testing.T) {
	if exampleConfig == "" {
		t.Fatal("go:embed chưa có hiệu lực, exampleConfig rỗng")
	}
	var cfg Config
	if err := json.Unmarshal(stripJSONComments([]byte(exampleConfig)), &cfg); err != nil {
		t.Fatalf("file ví dụ nội trang sau khi bỏ comment không phải JSON hợp lệ (người dùng chép là gặp họa): %v", err)
	}
	if cfg.Provider == "" || cfg.ModelName == "" {
		t.Fatal("file ví dụ phải cung cấp provider/model mặc định")
	}
	if _, ok := cfg.Providers[cfg.Provider]; !ok {
		t.Errorf("provider cấp cao nhất %q trong ví dụ không trỏ đến mục trong providers — mẫu con trỏ chính nó bị treo lơ lửng", cfg.Provider)
	}
	if !contains(exampleConfig, "con trỏ") {
		t.Error("file ví dụ phải giải thích rõ \"provider là con trỏ\" — tránh để bẫy nhận thức của #37 tái diễn")
	}
}

func TestWriteStartupError(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	path := WriteStartupError("boom: provider not configured")
	if path == "" {
		t.Fatal("phải trả về đường dẫn file đã ghi xuống đĩa")
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("đọc last-error.log: %v", err)
	}
	if want := "boom: provider not configured"; !contains(string(data), want) {
		t.Errorf("log phải chứa %q, thực tế: %s", want, data)
	}
}

func contains(s, sub string) bool {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}
