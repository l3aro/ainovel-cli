package bootstrap

import (
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/voocel/ainovel-cli/internal/errs"
)

const configDirName = ".ainovel"

// DefaultConfigPath trả về đường dẫn file cấu hình toàn cục ~/.ainovel/config.json.
func DefaultConfigPath() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	return filepath.Join(home, configDirName, "config.json")
}

// DefaultConfigDir trả về đường dẫn thư mục ~/.ainovel; trả về chuỗi rỗng nếu không lấy được thư mục home.
// Chỉ dùng để đọc/ghi các file không bắt buộc tồn tại (như cache model), không tự động tạo thư mục.
func DefaultConfigDir() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	return filepath.Join(home, configDirName)
}

// configDir trả về đường dẫn thư mục ~/.ainovel, tạo mới nếu chưa tồn tại.
func configDir() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("home dir: %w", err)
	}
	dir := filepath.Join(home, configDirName)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", fmt.Errorf("create config dir: %w", err)
	}
	return dir, nil
}

// projectConfigPath trả về đường dẫn tương đối của file cấu hình cấp dự án ./.ainovel/config.json.
// Thư mục dotdir cấp dự án phản chiếu ~/.ainovel/ toàn cục, dùng lại cùng configDirName; phân giải tương đối theo cwd.
func projectConfigPath() string {
	return filepath.Join(configDirName, "config.json")
}

// LoadConfig tải và hợp nhất cấu hình theo thứ tự ưu tiên:
//  1. ~/.ainovel/config.json (toàn cục)
//  2. ./.ainovel/config.json (ghi đè cấp dự án)
//  3. Đường dẫn chỉ định bởi flagPath (ưu tiên cao nhất)
func LoadConfig(flagPath string) (Config, error) {
	var cfg Config

	// 1. Cấu hình toàn cục. Đây là nền tảng ưu tiên thấp nhất; file lỗi sẽ giáng cấp thành
	//    cảnh báo thay vì chặn — có thể bị ghi đè bởi cấp dự án / --config;
	//    lỗi cứng sẽ chặn người dùng có "cấu hình toàn cục lỗi + --config hợp lệ",
	//    vi phạm ngữ nghĩa "tôi chỉ định rõ ràng cái này" của --config.
	if p := DefaultConfigPath(); p != "" {
		global, found, err := loadOptionalJSON(p)
		switch {
		case err != nil:
			slog.Warn("Cấu hình toàn cục phân tích thất bại, đã bỏ qua (có thể bị ghi đè bởi cấp dự án/--config)", "module", "config", "path", p, "err", err)
		case found:
			cfg = global
		}
	}

	// 2. Ghi đè cấp dự án. File lỗi sẽ fail loud: người dùng chủ động đặt cấu hình
	//    trong thư mục hiện tại, nuốt im lặng sẽ khiến "đã cấu hình nhưng không có hiệu lực"
	//    không thể truy vết (issue #37).
	project, found, err := loadOptionalJSON(projectConfigPath())
	if err != nil {
		return cfg, fmt.Errorf("cấu hình cấp dự án ./.ainovel/config.json phân tích thất bại (vui lòng kiểm tra cú pháp JSON): %w", err)
	}
	if found {
		cfg = mergeConfig(cfg, project)
	}

	// 3. Ghi đè từ CLI flag
	if flagPath != "" {
		override, err := loadJSONFile(flagPath)
		if err != nil {
			return cfg, fmt.Errorf("load config %s: %w", flagPath, err)
		}
		cfg = mergeConfig(cfg, override)
	}

	// Phân giải tham chiếu biến môi trường ("env:NAME") SAU khi hợp nhất: các cấp giữ
	// chuỗi thô cho tới đây, nên cấp dự án/--config ghi đè (hoặc tự dùng) env: độc lập
	// với cấp toàn cục; lỗi chỉ xảy ra khi tham chiếu thực sự có hiệu lực.
	if err := expandEnvRefs(&cfg); err != nil {
		return cfg, fmt.Errorf("cấu hình không hợp lệ: %w", err)
	}
	return cfg, nil
}

// loadOptionalJSON đọc một file cấu hình tùy chọn:
//   - File không tồn tại → (zero, false, nil), để bên gọi quyết định dùng giá trị mặc định/cấp trên
//   - File tồn tại nhưng phân tích thất bại → trả về lỗi (không còn nuốt im lặng — nếu không
//     cấu hình của người dùng "đã cấu hình nhưng không có hiệu lực" mà không thể truy vết,
//     đây chính là nguyên nhân gốc rễ của issue #37)
func loadOptionalJSON(path string) (Config, bool, error) {
	cfg, err := loadJSONFile(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return Config{}, false, nil
		}
		return Config{}, false, err
	}
	return cfg, true, nil
}

// LoadConfigFile đọc một file cấu hình JSON đơn lẻ, hỗ trợ chú thích dòng //.
// Không thực hiện hợp nhất, chỉ trả về cấu hình của file đó (kèm phân giải env:).
// Trả về lỗi nếu file không tồn tại.
func LoadConfigFile(path string) (Config, error) {
	cfg, err := loadJSONFile(path)
	if err != nil {
		return Config{}, err
	}
	if err := expandEnvRefs(&cfg); err != nil {
		return Config{}, err
	}
	return cfg, nil
}

// loadJSONFile đọc file cấu hình JSON, hỗ trợ chú thích dòng //.
// Trả về lỗi nếu file không tồn tại (để bên gọi quyết định có bỏ qua hay không).
func loadJSONFile(path string) (Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return Config{}, err
	}
	cleaned := stripJSONComments(data)
	var cfg Config
	if err := json.Unmarshal(cleaned, &cfg); err != nil {
		return Config{}, fmt.Errorf("parse %s: %w", path, err)
	}
	return cfg, nil
}

// mergeConfig hợp nhất overlay vào base. Các trường có giá trị khác zero sẽ ghi đè, map hợp nhất theo key.
func mergeConfig(base, overlay Config) Config {
	if overlay.Provider != "" {
		base.Provider = overlay.Provider
	}
	if overlay.ModelName != "" {
		base.ModelName = overlay.ModelName
	}
	if overlay.Style != "" {
		base.Style = overlay.Style
	}
	if overlay.ContextWindow > 0 {
		base.ContextWindow = overlay.ContextWindow
	}

	// Providers: key của overlay ghi đè key cùng tên trong base
	if len(overlay.Providers) > 0 {
		if base.Providers == nil {
			base.Providers = make(map[string]ProviderConfig)
		}
		for k, v := range overlay.Providers {
			existing := base.Providers[k]
			if v.Type != "" {
				existing.Type = v.Type
			}
			if v.APIKey != "" {
				existing.APIKey = v.APIKey
			}
			if v.BaseURL != "" {
				existing.BaseURL = v.BaseURL
			}
			if len(v.Models) > 0 {
				existing.Models = append([]string(nil), v.Models...)
			}
			if len(v.ExtraBody) > 0 {
				existing.ExtraBody = cloneMap(v.ExtraBody)
			}
			if len(v.Extra) > 0 {
				existing.Extra = cloneMap(v.Extra)
			}
			base.Providers[k] = existing
		}
	}

	// Roles: key của overlay ghi đè key cùng tên trong base
	if len(overlay.Roles) > 0 {
		if base.Roles == nil {
			base.Roles = make(map[string]RoleConfig)
		}
		for k, v := range overlay.Roles {
			existing := base.Roles[k]
			if v.Provider != "" {
				existing.Provider = v.Provider
			}
			if v.Model != "" {
				existing.Model = v.Model
			}
			if len(v.Fallbacks) > 0 {
				existing.Fallbacks = append([]ModelRef(nil), v.Fallbacks...)
			}
			base.Roles[k] = existing
		}
	}

	// Budget / Notify: ghi đè toàn bộ khối (ngân sách/cảnh báo cấp dự án là khai báo chính sách độc lập,
	// không nối từng trường với toàn cục)
	if overlay.Budget != (BudgetConfig{}) {
		base.Budget = overlay.Budget
	}
	if overlay.Notify.Enabled != nil || overlay.Notify.Command != "" || len(overlay.Notify.Events) > 0 {
		base.Notify = overlay.Notify
	}

	return base
}

func cloneMap(m map[string]any) map[string]any {
	if len(m) == 0 {
		return nil
	}
	c := make(map[string]any, len(m))
	for k, v := range m {
		c[k] = v
	}
	return c
}

// stripJSONComments loại bỏ các chú thích dòng // trong JSON, theo dõi trạng thái dấu ngoặc kép
// để tránh xóa nhầm nội dung bên trong chuỗi.
func stripJSONComments(data []byte) []byte {
	out := make([]byte, 0, len(data))
	inString := false
	escaped := false

	for i := 0; i < len(data); i++ {
		b := data[i]

		if escaped {
			out = append(out, b)
			escaped = false
			continue
		}

		if inString {
			out = append(out, b)
			if b == '\\' {
				escaped = true
			} else if b == '"' {
				inString = false
			}
			continue
		}

		// Không nằm trong chuỗi
		if b == '"' {
			inString = true
			out = append(out, b)
			continue
		}

		// Phát hiện chú thích //
		if b == '/' && i+1 < len(data) && data[i+1] == '/' {
			// Bỏ qua đến cuối dòng
			for i < len(data) && data[i] != '\n' {
				i++
			}
			if i < len(data) {
				out = append(out, '\n')
			}
			continue
		}

		out = append(out, b)
	}

	return out
}

// WriteStartupError ghi nối tiếp lỗi nghiêm trọng trong giai đoạn khởi động vào ~/.ainovel/last-error.log,
// và trả về đường dẫn file đó (best-effort, trả về chuỗi rỗng nếu thất bại). Khi khởi động bằng
// cách nhấp đúp, cửa sổ console sẽ đóng ngay khi tiến trình kết thúc khiến lỗi thoáng hiện rồi biến mất,
// ghi xuống đĩa là cách duy nhất để người dùng truy vết sau đó.
func WriteStartupError(msg string) string {
	dir := DefaultConfigDir()
	if dir == "" {
		return ""
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return ""
	}
	path := filepath.Join(dir, "last-error.log")
	f, err := os.OpenFile(path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		return ""
	}
	defer f.Close()
	if _, err := fmt.Fprintf(f, "[%s] %s\n", time.Now().Format(time.RFC3339), msg); err != nil {
		return ""
	}
	return path
}

// SaveConfig ghi cấu hình vào đường dẫn chỉ định (định dạng JSON, căn lề đẹp).
func SaveConfig(path string, cfg Config) error {
	// Khôi phục các trường đã phân giải từ biến môi trường về dạng "env:NAME" trước khi
	// ghi, để không rò rỉ bí mật đã phân giải xuống đĩa; trường bị đổi từ lúc tải
	// (ví dụ qua /model) thì giữ nguyên giá trị mới. Thao tác trên bản sao để không
	// đổi cấu hình đang chạy của bên gọi.
	if len(cfg.EnvRefs) > 0 {
		cfg = cloneConfigForSave(cfg)
		walkConfigStrings(&cfg, func(path, value string) (string, bool) {
			ref, ok := cfg.EnvRefs[path]
			if !ok || value != ref.resolved {
				return "", false
			}
			return ref.raw, true
		})
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	data, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, data, 0o644)
}

// envRefPrefix là tiền tố tham chiếu biến môi trường trong giá trị chuỗi cấu hình:
// "env:OPENROUTER_API_KEY" sẽ được thay bằng giá trị của biến OPENROUTER_API_KEY tại
// thời điểm tải. Áp dụng cho mọi giá trị chuỗi của Config — trường có kiểu (api_key/
// base_url/type, model/provider, roles, notify.command, ...) và cả giá trị chuỗi lồng
// trong extra_body/extra (ví dụ extra.headers) — không áp dụng cho khóa map.
const envRefPrefix = "env:"

// walkConfigStrings duyệt mọi giá trị chuỗi của Config và áp dụng rewrite.
// rewrite nhận (đường dẫn trường, giá trị hiện tại) và trả về (giá trị mới, true)
// nếu muốn thay. Đường dẫn trường dạng "providers.openrouter.api_key",
// "roles.writer.model", "providers.openrouter.extra.headers.X-Api-Key", ...
func walkConfigStrings(cfg *Config, rewrite func(path, value string) (string, bool)) {
	apply := func(path, value string, dst *string) {
		if v, ok := rewrite(path, value); ok {
			*dst = v
		}
	}
	apply("provider", cfg.Provider, &cfg.Provider)
	apply("model", cfg.ModelName, &cfg.ModelName)
	apply("thinking", cfg.Thinking, &cfg.Thinking)
	apply("style", cfg.Style, &cfg.Style)
	apply("notify.command", cfg.Notify.Command, &cfg.Notify.Command)
	for i, ev := range cfg.Notify.Events {
		apply(fmt.Sprintf("notify.events[%d]", i), ev, &cfg.Notify.Events[i])
	}
	for name, pc := range cfg.Providers {
		apply(fmt.Sprintf("providers.%s.type", name), pc.Type, &pc.Type)
		apply(fmt.Sprintf("providers.%s.api_key", name), pc.APIKey, &pc.APIKey)
		apply(fmt.Sprintf("providers.%s.base_url", name), pc.BaseURL, &pc.BaseURL)
		for i, m := range pc.Models {
			apply(fmt.Sprintf("providers.%s.models[%d]", name, i), m, &pc.Models[i])
		}
		walkMapStrings(pc.ExtraBody, fmt.Sprintf("providers.%s.extra_body", name), rewrite)
		walkMapStrings(pc.Extra, fmt.Sprintf("providers.%s.extra", name), rewrite)
		cfg.Providers[name] = pc
	}
	for role, rc := range cfg.Roles {
		apply(fmt.Sprintf("roles.%s.provider", role), rc.Provider, &rc.Provider)
		apply(fmt.Sprintf("roles.%s.model", role), rc.Model, &rc.Model)
		apply(fmt.Sprintf("roles.%s.thinking", role), rc.Thinking, &rc.Thinking)
		for i, fb := range rc.Fallbacks {
			apply(fmt.Sprintf("roles.%s.fallbacks[%d].provider", role, i), fb.Provider, &fb.Provider)
			apply(fmt.Sprintf("roles.%s.fallbacks[%d].model", role, i), fb.Model, &fb.Model)
			rc.Fallbacks[i] = fb
		}
		cfg.Roles[role] = rc
	}
}

// walkMapStrings đệ quy qua mọi giá trị chuỗi trong map tự do (extra_body/extra) và
// áp dụng rewrite; đường dẫn dạng "providers.<tên>.extra.headers.X-Key" / "…[i]".
func walkMapStrings(m map[string]any, base string, rewrite func(path, value string) (string, bool)) {
	for k, v := range m {
		path := base + "." + k
		switch val := v.(type) {
		case string:
			if nv, ok := rewrite(path, val); ok {
				m[k] = nv
			}
		case map[string]any:
			walkMapStrings(val, path, rewrite)
		case []any:
			walkAnySlice(path, val, rewrite)
		}
	}
}

func walkAnySlice(base string, items []any, rewrite func(path, value string) (string, bool)) {
	for i, item := range items {
		path := fmt.Sprintf("%s[%d]", base, i)
		switch val := item.(type) {
		case string:
			if nv, ok := rewrite(path, val); ok {
				items[i] = nv
			}
		case map[string]any:
			walkMapStrings(val, path, rewrite)
		case []any:
			walkAnySlice(path, val, rewrite)
		}
	}
}

// expandEnvRefs thay mọi giá trị chuỗi dạng "env:NAME" bằng giá trị biến môi trường NAME
// và ghi chú vào cfg.EnvRefs để SaveConfig khôi phục sau này. Biến chưa đặt hoặc rỗng →
// lỗi kèm đường dẫn trường và tên biến (fail loud — nhất quán với nguyên tắc "cấu hình
// nhìn như có hiệu lực nhưng thực ra không" của issue #37).
func expandEnvRefs(cfg *Config) error {
	if cfg.EnvRefs == nil {
		cfg.EnvRefs = make(map[string]envRef)
	}
	var firstErr error
	walkConfigStrings(cfg, func(path, value string) (string, bool) {
		if !strings.HasPrefix(value, envRefPrefix) {
			return "", false
		}
		name := strings.TrimPrefix(value, envRefPrefix)
		if name == "" {
			firstErr = fmt.Errorf("trường %q: chuỗi %q thiếu tên biến sau env:: %w", path, value, errs.ErrConfig)
			return "", false
		}
		resolved, ok := os.LookupEnv(name)
		if !ok || resolved == "" {
			firstErr = fmt.Errorf("trường %q: biến môi trường %s chưa được đặt hoặc rỗng: %w", path, name, errs.ErrConfig)
			return "", false
		}
		cfg.EnvRefs[path] = envRef{raw: value, resolved: resolved}
		return resolved, true
	})
	return firstErr
}

// cloneConfigForSave tạo bản sao có map/slice riêng của mọi trường Config bị thay bởi
// walkConfigStrings, để SaveConfig khôi phục env-ref mà không đổi cấu hình đang chạy
// của bên gọi.
func cloneConfigForSave(cfg Config) Config {
	if len(cfg.Providers) > 0 {
		clone := make(map[string]ProviderConfig, len(cfg.Providers))
		for k, v := range cfg.Providers {
			v.Models = append([]string(nil), v.Models...)
			v.ExtraBody = cloneMapDeep(v.ExtraBody)
			v.Extra = cloneMapDeep(v.Extra)
			clone[k] = v
		}
		cfg.Providers = clone
	}
	if len(cfg.Roles) > 0 {
		clone := make(map[string]RoleConfig, len(cfg.Roles))
		for k, v := range cfg.Roles {
			v.Fallbacks = append([]ModelRef(nil), v.Fallbacks...)
			clone[k] = v
		}
		cfg.Roles = clone
	}
	if len(cfg.Notify.Events) > 0 {
		cfg.Notify.Events = append([]string(nil), cfg.Notify.Events...)
	}
	return cfg
}

// cloneMapDeep tạo bản sao sâu của map tự do (extra_body/extra): map lồng và slice
// bên trong cũng được sao chép, để thao tác trên bản sao không chạm cấu trúc gốc.
func cloneMapDeep(m map[string]any) map[string]any {
	if len(m) == 0 {
		return nil
	}
	c := make(map[string]any, len(m))
	for k, v := range m {
		c[k] = cloneAny(v)
	}
	return c
}

func cloneAny(v any) any {
	switch val := v.(type) {
	case map[string]any:
		c := make(map[string]any, len(val))
		for k, item := range val {
			c[k] = cloneAny(item)
		}
		return c
	case []any:
		c := make([]any, len(val))
		for i, item := range val {
			c[i] = cloneAny(item)
		}
		return c
	default:
		return v
	}
}
