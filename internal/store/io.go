package store

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

// IO đóng gói các thao tác đọc/ghi hệ thống tệp, cung cấp khóa và ghi nguyên tử.
// Mỗi Store con giữ một instance IO riêng biệt, sở hữu sync.RWMutex của chính nó.
type IO struct {
	dir string
	mu  sync.RWMutex

	// buffered giữ các writer ghi nối tiếp bộ nhớ đệm theo đường dẫn tương đối,
	// dùng cho log quan sát (session jsonl, runtime queue/task log) để tránh
	// mở file + fsync mỗi dòng trên đường nóng vòng lặp LLM.
	buffered map[string]*bufferedLog
}

// bufferedLog là một file đang mở + bộ đệm ghi của nó.
type bufferedLog struct {
	f         *os.File
	bw        *bufio.Writer
	lastFlush time.Time
}

// Ngưỡng flush của đường ghi nối tiếp bộ nhớ đệm:
//   - bufferedFlushBytes: đạt 64KB dữ liệu trong bộ đệm, hoặc
//   - bufferedFlushInterval: đã 200ms kể từ lần flush trước.
//
// Hạn chót 200ms chỉ được KIỂM TRA LƯỜI trên lần ghi kế tiếp — không có goroutine nền,
// nên một dòng cuối sau khi ngừng ghi có thể nằm trong bộ đệm lâu hơn 200ms; độ bền của
// phần đuôi này do FlushBuffered ở đường thoát chuẩn (Host.Close / headless Run) bảo đảm.
//
// Bộ đệm bufio được cấp GẤP ĐÔI ngưỡng flush (128KB) để kiểm tra kích thước luôn kích
// hoạt TRƯỚC khi bufio tự flush giữa dòng: ở trạng thái nghỉ Buffered() < 64KB, nên mọi
// dòng ≤ 64KB đều nằm gọn trong phần bộ đệm còn trống (> 64KB) — flush chỉ xảy ra ở ranh
// giới dòng, không bao giờ để lộ bản ghi cụt trên đĩa trong vận hành bình thường.
// Chỉ dòng đơn > ~128KB mới có thể chạm flush nội bộ giữa dòng (đầu vào phi thực tế:
// session log đã nén placeholder, dòng lớn nhất cỡ vài KB).
const (
	bufferedFlushBytes    = 64 << 10
	bufferedFlushInterval = 200 * time.Millisecond

	// bufferedWriterBytes = 2× bufferedFlushBytes (xem phân tích ở trên).
	bufferedWriterBytes = bufferedFlushBytes * 2
)

func newIO(dir string) *IO {
	return &IO{dir: dir, buffered: make(map[string]*bufferedLog)}
}

func (io *IO) path(rel string) string {
	return filepath.Join(io.dir, rel)
}

func (io *IO) ReadFile(rel string) ([]byte, error) {
	io.mu.RLock()
	defer io.mu.RUnlock()
	return io.ReadFileUnlocked(rel)
}

func (io *IO) ReadFileUnlocked(rel string) ([]byte, error) {
	return os.ReadFile(io.path(rel))
}

func (io *IO) WriteFileUnlocked(rel string, data []byte) error {
	p := io.path(rel)
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(filepath.Dir(p), filepath.Base(p)+".tmp-*")
	if err != nil {
		return err
	}
	tmpPath := tmp.Name()
	defer func() { _ = os.Remove(tmpPath) }()

	if _, err := tmp.Write(data); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Chmod(0o644); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Sync(); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(tmpPath, p)
}

func (io *IO) ReadJSON(rel string, v any) error {
	io.mu.RLock()
	defer io.mu.RUnlock()
	return io.ReadJSONUnlocked(rel, v)
}

func (io *IO) ReadJSONUnlocked(rel string, v any) error {
	data, err := io.ReadFileUnlocked(rel)
	if err != nil {
		return err
	}
	return json.Unmarshal(data, v)
}

func (io *IO) WriteJSON(rel string, v any) error {
	io.mu.Lock()
	defer io.mu.Unlock()
	return io.WriteJSONUnlocked(rel, v)
}

func (io *IO) WriteJSONUnlocked(rel string, v any) error {
	data, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return err
	}
	return io.WriteFileUnlocked(rel, data)
}

func (io *IO) WriteMarkdown(rel string, content string) error {
	io.mu.Lock()
	defer io.mu.Unlock()
	return io.WriteFileUnlocked(rel, []byte(content))
}

func (io *IO) WriteMarkdownUnlocked(rel string, content string) error {
	return io.WriteFileUnlocked(rel, []byte(content))
}

func (io *IO) AppendLine(rel string, data []byte) error {
	io.mu.Lock()
	defer io.mu.Unlock()
	return io.AppendLineUnlocked(rel, data)
}

func (io *IO) AppendLineUnlocked(rel string, data []byte) error {
	p := io.path(rel)
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		return err
	}
	f, err := os.OpenFile(p, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
	if err != nil {
		return err
	}
	defer func() { _ = f.Close() }()
	if _, err = f.Write(data); err != nil {
		return err
	}
	return f.Sync()
}

// AppendLineBuffered ghi nối tiếp một dòng vào file log với bộ nhớ đệm trong tiến trình.
// Khác AppendLineUnlocked (fsync mỗi dòng — bền vững theo hợp đồng), đường này chỉ
// buf.Flush() đẩy xuống kernel, không fsync.
// Flush được kiểm tra lười ngay trên lần ghi này khi (a) bộ đệm đạt 64KB hoặc
// (b) đã 200ms kể từ lần flush trước. File được mở một lần (kèm MkdirAll) và giữ
// mở giữa các lần ghi.
//
// CHÍNH SÁCH DÙNG: đường bộ nhớ đệm CHỈ dành cho log quan sát (session jsonl,
// task log runtime) — mất đuôi khi crash là chấp nhận được, độ bền lúc thoát bình
// thường do FlushBuffered (Host.Close / headless Run) bảo đảm. Dữ liệu liên quan
// khôi phục (queue runtime, checkpoints) PHẢI giữ fsync mỗi dòng qua AppendLineUnlocked.
func (io *IO) AppendLineBuffered(rel string, data []byte) error {
	io.mu.Lock()
	defer io.mu.Unlock()

	b, err := io.bufferedLocked(rel)
	if err != nil {
		return err
	}
	if _, err := b.bw.Write(data); err != nil {
		return err
	}
	if b.bw.Buffered() >= bufferedFlushBytes || time.Since(b.lastFlush) >= bufferedFlushInterval {
		if err := b.bw.Flush(); err != nil {
			return err
		}
		b.lastFlush = time.Now()
	}
	return nil
}

// bufferedLocked lấy (mở lười nếu chưa có) writer bộ nhớ đệm của đường dẫn.
// Bắt buộc đã giữ io.mu.
func (io *IO) bufferedLocked(rel string) (*bufferedLog, error) {
	if b, ok := io.buffered[rel]; ok {
		return b, nil
	}
	p := io.path(rel)
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		return nil, err
	}
	f, err := os.OpenFile(p, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
	if err != nil {
		return nil, err
	}
	b := &bufferedLog{f: f, bw: bufio.NewWriterSize(f, bufferedWriterBytes), lastFlush: time.Now()}
	io.buffered[rel] = b
	return b, nil
}

// FlushBuffered đẩy toàn bộ dữ liệu bộ nhớ đệm của mọi đường dẫn trong IO này xuống
// kernel (chỉ buf.Flush, không fsync). Gọi ở đường thoát chuẩn và trước mọi lần đọc
// lại cùng tiến trình để bảo đảm thấy dữ liệu vừa ghi.
func (io *IO) FlushBuffered() error {
	io.mu.Lock()
	defer io.mu.Unlock()
	var firstErr error
	for _, b := range io.buffered {
		if err := b.bw.Flush(); err != nil {
			if firstErr == nil {
				firstErr = err
			}
			continue // giữ nguyên lastFlush để lần ghi kế tiếp thử lại sớm
		}
		b.lastFlush = time.Now()
	}
	return firstErr
}

// DropBuffered đóng và loại bỏ writer bộ nhớ đệm của đường dẫn chỉ định.
// Dùng khi file bị xóa bên ngoài IO (vd: RuntimeStore.Reset) để lần ghi sau
// mở file mới từ đầu, thay vì ghi tiếp vào inode đã bị unlink.
func (io *IO) DropBuffered(rel string) {
	io.mu.Lock()
	defer io.mu.Unlock()
	io.dropBufferedLocked(rel)
}

// DropBufferedPrefix đóng và loại bỏ mọi writer bộ nhớ đệm có đường dẫn nằm dưới
// prefix (so khớp theo ranh giới thư mục). Dùng khi xóa cả thư mục (vd: meta/runtime/tasks).
func (io *IO) DropBufferedPrefix(prefix string) {
	io.mu.Lock()
	defer io.mu.Unlock()
	prefix = filepath.ToSlash(strings.TrimSuffix(filepath.ToSlash(prefix), "/")) + "/"
	for rel, b := range io.buffered {
		if strings.HasPrefix(rel, prefix) {
			_ = b.f.Close()
			delete(io.buffered, rel)
		}
	}
}

// dropBufferedLocked là biến thể đã giữ io.mu.
func (io *IO) dropBufferedLocked(rel string) {
	if b, ok := io.buffered[rel]; ok {
		_ = b.f.Close()
		delete(io.buffered, rel)
	}
}

func (io *IO) RemoveFile(rel string) error {
	io.mu.Lock()
	defer io.mu.Unlock()
	return io.RemoveFileUnlocked(rel)
}

func (io *IO) RemoveFileUnlocked(rel string) error {
	io.dropBufferedLocked(rel)
	err := os.Remove(io.path(rel))
	if os.IsNotExist(err) {
		return nil
	}
	return err
}

func (io *IO) WithWriteLock(fn func() error) error {
	io.mu.Lock()
	defer io.mu.Unlock()
	return fn()
}

// EnsureDirs tạo các thư mục con được chỉ định.
func (io *IO) EnsureDirs(dirs []string) error {
	for _, d := range dirs {
		if err := os.MkdirAll(filepath.Join(io.dir, d), 0o755); err != nil {
			return fmt.Errorf("create dir %s: %w", d, err)
		}
	}
	return nil
}
