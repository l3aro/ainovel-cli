package store

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/voocel/ainovel-cli/internal/domain"
)

const runtimeQueuePath = "meta/runtime/queue.jsonl"

// RuntimeStore quản lý hàng đợi runtime thống nhất và log theo từng task.
type RuntimeStore struct {
	io *IO

	mu         sync.Mutex
	seqLoaded  bool
	nextSeqNum int64
}

func NewRuntimeStore(io *IO) *RuntimeStore {
	return &RuntimeStore{io: io}
}

// AppendQueue thêm một bản ghi vào hàng đợi runtime và tự động gán số thứ tự tăng dần.
func (s *RuntimeStore) AppendQueue(item domain.RuntimeQueueItem) (domain.RuntimeQueueItem, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if err := s.ensureSeqLoadedLocked(); err != nil {
		return item, err
	}
	s.nextSeqNum++
	item.Seq = s.nextSeqNum
	if item.Time.IsZero() {
		item.Time = time.Now()
	}
	if err := s.appendJSONLine(runtimeQueuePath, item); err != nil {
		return item, err
	}
	return item, nil
}

// LoadQueue đọc toàn bộ các mục trong hàng đợi runtime đã được lưu trữ.
// Flush phòng thủ trước khi đọc: queue ghi fsync (không bộ nhớ đệm) nên không
// bắt buộc, nhưng giữ để mọi writer bộ nhớ đệm trên IO này (task log) đã xuống đĩa.
func (s *RuntimeStore) LoadQueue() ([]domain.RuntimeQueueItem, error) {
	if err := s.io.FlushBuffered(); err != nil {
		return nil, err
	}
	return loadJSONLines[domain.RuntimeQueueItem](s.io, runtimeQueuePath)
}

// LoadQueueAfter trả về các mục trong hàng đợi có số thứ tự lớn hơn afterSeq.
func (s *RuntimeStore) LoadQueueAfter(afterSeq int64) ([]domain.RuntimeQueueItem, error) {
	items, err := s.LoadQueue()
	if err != nil || afterSeq <= 0 {
		return items, err
	}
	filtered := items[:0]
	for _, item := range items {
		if item.Seq > afterSeq {
			filtered = append(filtered, item)
		}
	}
	return append([]domain.RuntimeQueueItem(nil), filtered...), nil
}

// AppendTaskLog thêm một mục log vào nhật ký chạy của một task cụ thể.
// Ghi bộ nhớ đệm (log quan sát — xem appendJSONLineBuffered).
func (s *RuntimeStore) AppendTaskLog(taskID string, entry domain.RuntimeTaskLogEntry) error {
	taskID = strings.TrimSpace(taskID)
	if taskID == "" {
		return nil
	}
	if entry.Time.IsZero() {
		entry.Time = time.Now()
	}
	if entry.TaskID == "" {
		entry.TaskID = taskID
	}
	return s.appendJSONLineBuffered(taskLogPath(taskID), entry)
}

// LoadTaskLog đọc toàn bộ nhật ký chạy của một task cụ thể.
// Flush bộ nhớ đệm trước khi đọc để thấy các mục vừa ghi qua AppendLineBuffered.
func (s *RuntimeStore) LoadTaskLog(taskID string) ([]domain.RuntimeTaskLogEntry, error) {
	taskID = strings.TrimSpace(taskID)
	if taskID == "" {
		return nil, nil
	}
	if err := s.io.FlushBuffered(); err != nil {
		return nil, err
	}
	return loadJSONLines[domain.RuntimeTaskLogEntry](s.io, taskLogPath(taskID))
}

func taskLogPath(taskID string) string {
	return filepath.Join("meta", "runtime", "tasks", taskID+".log")
}

// Reset xóa sạch hàng đợi runtime và toàn bộ log task.
func (s *RuntimeStore) Reset() error {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.seqLoaded = false
	s.nextSeqNum = 0

	var errs []string
	if err := os.Remove(filepath.Join(s.io.dir, runtimeQueuePath)); err != nil && !os.IsNotExist(err) {
		errs = append(errs, err.Error())
	}
	if err := os.RemoveAll(filepath.Join(s.io.dir, "meta", "runtime", "tasks")); err != nil {
		errs = append(errs, err.Error())
	}
	// Bỏ writer bộ nhớ đệm của các đường dẫn vừa xóa: nếu giữ lại, lần ghi sau sẽ
	// tiếp tục vào inode đã bị unlink và file mới không bao giờ được tạo trên đĩa.
	s.io.DropBuffered(runtimeQueuePath)
	s.io.DropBufferedPrefix("meta/runtime/tasks")
	if err := os.MkdirAll(filepath.Join(s.io.dir, "meta", "runtime", "tasks"), 0o755); err != nil {
		errs = append(errs, err.Error())
	}
	if len(errs) > 0 {
		sort.Strings(errs)
		return fmt.Errorf("reset runtime store: %s", strings.Join(errs, "; "))
	}
	return nil
}

func (s *RuntimeStore) ensureSeqLoadedLocked() error {
	if s.seqLoaded {
		return nil
	}
	// Flush phòng thủ trước khi đọc: queue ghi fsync (không bộ nhớ đệm) nên không
	// bắt buộc, nhưng giữ để bảo đảm mọi writer bộ nhớ đệm trên IO này (task log)
	// đã xuống đĩa — đọc cùng tiến trình phải thấy dữ liệu vừa ghi.
	if err := s.io.FlushBuffered(); err != nil {
		return err
	}
	items, err := loadJSONLines[domain.RuntimeQueueItem](s.io, runtimeQueuePath)
	if err != nil {
		return err
	}
	if len(items) > 0 {
		s.nextSeqNum = items[len(items)-1].Seq
	}
	s.seqLoaded = true
	return nil
}

// appendJSONLine ghi nối tiếp một mục vào queue runtime với fsync mỗi dòng.
// queue.jsonl là dữ liệu khôi phục: ReplayQueue / ensureSeqLoadedLocked / LoadQueueAfter
// dùng nó để tái lập trạng thái runtime và dãy số thứ tự, nên mất đuôi khi crash có thể
// làm lệch trạng thái hồi phục và trùng seq — KHÔNG dùng đường bộ nhớ đệm.
func (s *RuntimeStore) appendJSONLine(rel string, value any) error {
	data, err := json.Marshal(value)
	if err != nil {
		return err
	}
	data = append(data, '\n')
	return s.io.AppendLine(rel, data)
}

// appendJSONLineBuffered ghi nối tiếp một mục log task qua đường bộ nhớ đệm.
// Task log chỉ là log quan sát; đuôi log khi crash được bỏ qua như đã chấp nhận,
// độ bền lúc thoát bình thường do FlushLogs bảo đảm.
func (s *RuntimeStore) appendJSONLineBuffered(rel string, value any) error {
	data, err := json.Marshal(value)
	if err != nil {
		return err
	}
	data = append(data, '\n')
	return s.io.AppendLineBuffered(rel, data)
}

func loadJSONLines[T any](io *IO, rel string) ([]T, error) {
	data, err := io.ReadFile(rel)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	scanner := bufio.NewScanner(bytes.NewReader(data))
	buf := make([]byte, 0, 64*1024)
	scanner.Buffer(buf, 8*1024*1024)
	var out []T
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}
		var item T
		if err := json.Unmarshal([]byte(line), &item); err != nil {
			return nil, fmt.Errorf("parse %s: %w", rel, err)
		}
		out = append(out, item)
	}
	if err := scanner.Err(); err != nil {
		return nil, err
	}
	return out, nil
}
