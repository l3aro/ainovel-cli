package headless

import (
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/voocel/ainovel-cli/assets"
	"github.com/voocel/ainovel-cli/internal/bootstrap"
	"github.com/voocel/ainovel-cli/internal/diag"
	"github.com/voocel/ainovel-cli/internal/domain"
	"github.com/voocel/ainovel-cli/internal/entry/startup"
	"github.com/voocel/ainovel-cli/internal/host"
	"github.com/voocel/ainovel-cli/internal/logger"
	"github.com/voocel/ainovel-cli/internal/store"
)

type Options struct {
	Prompt string
	Stdin  io.Reader
	Stdout io.Writer
	Stderr io.Writer
}

// Run chạy nhân phiên làm việc ở chế độ không giao diện, tiêu thụ trực tiếp các sự kiện Engine và đầu ra streaming.
// Nếu sau này thêm các phương thức khởi động dùng chung như “tiếp tục viết tiểu thuyết có sẵn”,
// không nên đưa thẳng vào đây mà hãy đưa vào internal/entry/startup, rồi để entry headless gọi lại.
func Run(cfg bootstrap.Config, bundle assets.Bundle, opts Options) error {
	stdout := opts.Stdout
	if stdout == nil {
		stdout = os.Stdout
	}
	stderr := opts.Stderr
	if stderr == nil {
		stderr = os.Stderr
	}
	stdin := opts.Stdin
	if stdin == nil {
		stdin = os.Stdin
	}

	eng, err := host.New(cfg, bundle)
	if err != nil {
		return err
	}
	eng.AskUser().SetHandler(newTerminalAskUser(stdin, stderr).handle)
	cleanup := logger.SetupFile(eng.Dir(), "headless.log", false)
	defer cleanup()
	defer eng.Close()
	// Khi chạy xong hoặc trả về lỗi, xuất một bản chẩn đoán đã ẩn danh hóa để người dùng headless dễ báo issue.
	// (Các trường hợp bị kill từ bên ngoài không đi qua defer, vẫn cần dùng /diag thủ công trong TUI.)
	defer func() { _, _ = diag.Export(store.NewStore(eng.Dir())) }()
	// Flush log bộ nhớ đệm TRƯỚC bản chẩn đoán (defer chạy LIFO) để bản chẩn đoán thấy đủ
	// session/runtime log; bản thân eng.Close cũng flush nên thứ tự này chỉ thắt chặt thêm.
	defer eng.FlushLogs()

	prompt := strings.TrimSpace(opts.Prompt)
	if prompt != "" {
		plan, err := startup.PrepareQuick(startup.Request{
			Mode:        startup.ModeQuick,
			UserPrompt:  prompt,
			OutputDir:   eng.Dir(),
			Interactive: true,
		})
		if err != nil {
			return err
		}
		fmt.Fprintf(stderr, "headless khởi động: %s\n", eng.Dir())
		if err := eng.StartPrepared(plan.StartPrompt); err != nil {
			return err
		}
	} else {
		items, err := eng.ReplayQueue(0)
		if err != nil {
			return err
		}
		roundHasContent, err := replayQueue(items, stdout, stderr)
		if err != nil {
			return err
		}
		label, err := eng.Resume()
		if err != nil {
			return err
		}
		if label == "" {
			return fmt.Errorf("chế độ headless yêu cầu --prompt, hoặc thư mục đầu ra %q phải có phiên có thể khôi phục", eng.Dir())
		}
		fmt.Fprintf(stderr, "headless khôi phục: %s (%s)\n", eng.Dir(), label)
		return consume(eng, stdout, stderr, roundHasContent)
	}

	return consume(eng, stdout, stderr, false)
}

func consume(eng *host.Host, stdout, stderr io.Writer, roundHasContent bool) error {
	return consumeLoop(eng.Events(), eng.Stream(), eng.Done(), stdout, stderr, roundHasContent)
}

// consumeLoop tiêu thụ sự kiện/stream cho đến khi kênh Done báo kết thúc hoặc cả ba kênh đều đóng.
// Nhận kênh trực tiếp (không qua Host) để test độc lập mà không cần dựng Host.
//
// Khi một kênh đóng (Host.Close đóng done+events+streamCh cùng lúc), kênh đó được gán nil:
// trong select, kênh nil chặn vĩnh viễn nên nhánh tương ứng bị vô hiệu hóa — tránh vòng lặp
// quay spin 100% CPU trên kênh đóng luôn sẵn sàng, đồng thời vẫn rút cạn các kênh còn mở.
// Khi cả ba kênh đều nil, select không còn nhánh nào sẵn sàng (không có default để không
// trả về sớm lúc engine đang chạy nhưng chưa có dữ liệu) nên phải kiểm tra tường minh.
func consumeLoop(events <-chan host.Event, stream <-chan string, done <-chan struct{}, stdout, stderr io.Writer, roundHasContent bool) error {
	for {
		select {
		case ev, ok := <-events:
			if !ok {
				events = nil
				if events == nil && stream == nil && done == nil {
					return nil
				}
				continue
			}
			writeEvent(stderr, ev)
		case delta, ok := <-stream:
			if !ok {
				stream = nil
				if events == nil && stream == nil && done == nil {
					return nil
				}
				continue
			}
			if delta == host.StreamClearSentinel {
				if roundHasContent {
					if _, err := io.WriteString(stdout, "\n\n"); err != nil {
						return err
					}
					roundHasContent = false
				}
				continue
			}
			if delta == "" {
				continue
			}
			if _, err := io.WriteString(stdout, delta); err != nil {
				return err
			}
			roundHasContent = true
		case _, ok := <-done:
			if !ok {
				done = nil
				if events == nil && stream == nil && done == nil {
					return nil
				}
				continue
			}
			return drainPendingLoop(events, stream, stdout, stderr, roundHasContent)
		}
	}
}

func drainPending(eng *host.Host, stdout, stderr io.Writer, roundHasContent bool) error {
	return drainPendingLoop(eng.Events(), eng.Stream(), stdout, stderr, roundHasContent)
}

// drainPendingLoop rút cạn nốt sự kiện/stream còn đọng rồi trả về. Cùng quy tắc chống spin
// như consumeLoop: kênh đóng được gán nil để vô hiệu hóa nhánh select. Khi cả hai kênh đều nil,
// select rơi vào default và trả về nil như trước đây — default chỉ chạy khi không còn nhánh
// nào sẵn sàng, nên với kênh còn mở (đang chờ dữ liệu) hành vi không đổi.
func drainPendingLoop(events <-chan host.Event, stream <-chan string, stdout, stderr io.Writer, roundHasContent bool) error {
	for {
		select {
		case ev, ok := <-events:
			if !ok {
				events = nil
				continue
			}
			writeEvent(stderr, ev)
		case delta, ok := <-stream:
			if !ok {
				stream = nil
				continue
			}
			if delta == host.StreamClearSentinel {
				if roundHasContent {
					if _, err := io.WriteString(stdout, "\n\n"); err != nil {
						return err
					}
					roundHasContent = false
				}
				continue
			}
			if delta != "" {
				if _, err := io.WriteString(stdout, delta); err != nil {
					return err
				}
				roundHasContent = true
			}
		default:
			if roundHasContent {
				if _, err := io.WriteString(stdout, "\n"); err != nil {
					return err
				}
			}
			return nil
		}
	}
}

func writeEvent(w io.Writer, ev host.Event) {
	if w == nil || strings.TrimSpace(ev.Summary) == "" {
		return
	}
	ts := ev.Time.Format("15:04:05")
	if ts == "00:00:00" {
		ts = "--:--:--"
	}
	fmt.Fprintf(w, "[%s] [%s] %s\n", ts, ev.Category, ev.Summary)
}

func replayQueue(items []domain.RuntimeQueueItem, stdout, stderr io.Writer) (bool, error) {
	var roundHasContent bool
	for _, item := range items {
		switch item.Kind {
		case domain.RuntimeQueueUIEvent:
			writeEvent(stderr, host.Event{
				Time:     item.Time,
				Category: item.Category,
				Summary:  item.Summary,
			})
		case domain.RuntimeQueueStreamClear:
			if roundHasContent {
				if _, err := io.WriteString(stdout, "\n\n"); err != nil {
					return roundHasContent, err
				}
				roundHasContent = false
			}
		case domain.RuntimeQueueStreamDelta:
			text := host.ReplayDeltaText(item)
			if text == "" {
				continue
			}
			if _, err := io.WriteString(stdout, text); err != nil {
				return roundHasContent, err
			}
			roundHasContent = true
		}
	}
	return roundHasContent, nil
}
