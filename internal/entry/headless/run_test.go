package headless

import (
	"bytes"
	"io"
	"strings"
	"testing"
	"time"

	"github.com/voocel/ainovel-cli/internal/host"
)

// runLoopWithTimeout chạy fn trong goroutine và đợi tối đa 2 giây. Nếu quá hạn tức là
// vòng lặp đang spin trên kênh đã đóng (hồi quy) — test fail ngay thay vì treo cả suite.
func runLoopWithTimeout(t *testing.T, name string, fn func() error) error {
	t.Helper()
	type result struct {
		err error
	}
	ch := make(chan result, 1)
	go func() {
		ch <- result{err: fn()}
	}()
	select {
	case r := <-ch:
		return r.err
	case <-time.After(2 * time.Second):
		t.Fatalf("%s: vòng lặp không trả về trong 2s — spin trên kênh đã đóng", name)
		return nil
	}
}

// notifyWriter báo mỗi lần ghi qua channel, để test đồng bộ được thời điểm dữ liệu
// đã được rút cạn — quan sát việc ghi TRƯỚC khi đóng kênh payload.
type notifyWriter struct {
	ch chan string
}

func (w notifyWriter) Write(p []byte) (int, error) {
	w.ch <- string(p)
	return len(p), nil
}

func TestConsumeLoopNoSpinOnClosedChannels(t *testing.T) {
	events := make(chan host.Event)
	stream := make(chan string)
	done := make(chan struct{})
	close(events)
	close(stream)
	close(done)

	err := runLoopWithTimeout(t, "consumeLoop", func() error {
		return consumeLoop(events, stream, done, io.Discard, io.Discard, false)
	})
	if err != nil {
		t.Fatalf("consumeLoop với cả ba kênh đã đóng phải trả về nil, được %v", err)
	}
}

// Regression: dữ liệu trên kênh CÒN MỞ phải được rút cạn sau khi các kênh khác đóng.
// stream giữ nguyên mở khi consumeLoop chạy; test đợi thông báo ghi "abc" (nghĩa là
// đã rút cạn khi stream còn mở), rồi MỚI đóng stream để cho phép vòng lặp thoát.
func TestConsumeLoopDrainsOpenStreamAfterSiblingsClosed(t *testing.T) {
	events := make(chan host.Event)
	done := make(chan struct{})
	close(events)
	close(done)
	stream := make(chan string, 1)
	stream <- "abc"

	writes := make(chan string, 1)
	ret := make(chan error, 1)
	go func() {
		ret <- consumeLoop(events, stream, done, notifyWriter{ch: writes}, io.Discard, false)
	}()

	select {
	case got := <-writes:
		if got != "abc" {
			t.Fatalf("stream còn mở phải được rút cạn nguyên vẹn, được %q", got)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("consumeLoop không rút cạn stream còn mở trong 2s")
	}

	// Lúc này stream vẫn mở (chưa ai đóng); đóng để vòng lặp thoát.
	close(stream)
	select {
	case err := <-ret:
		if err != nil {
			t.Fatalf("consumeLoop phải trả về nil sau khi stream đóng, được %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("consumeLoop không trả về trong 2s sau khi stream đóng")
	}
}

func TestDrainPendingLoopNoSpinOnClosedChannels(t *testing.T) {
	events := make(chan host.Event)
	stream := make(chan string)
	close(events)
	close(stream)

	err := runLoopWithTimeout(t, "drainPendingLoop", func() error {
		return drainPendingLoop(events, stream, io.Discard, io.Discard, false)
	})
	if err != nil {
		t.Fatalf("drainPendingLoop với cả hai kênh đã đóng phải trả về nil, được %v", err)
	}
}

// Regression: sự kiện trên kênh CÒN MỞ phải được rút cạn sau khi stream đóng.
// drainPendingLoop trả về qua default khi không còn nhánh nào sẵn sàng, nên không cần
// đóng events để thoát — test gửi thử sau khi trả về để chứng minh events vẫn mở.
func TestDrainPendingLoopDrainsOpenEventsAfterStreamClosed(t *testing.T) {
	stream := make(chan string)
	close(stream)
	events := make(chan host.Event, 1)
	events <- host.Event{
		Time:     time.Date(2026, 8, 13, 10, 30, 0, 0, time.UTC),
		Category: "SYSTEM",
		Summary:  "sáng tác hoàn thành",
	}

	var errBuf bytes.Buffer
	err := runLoopWithTimeout(t, "drainPendingLoop", func() error {
		return drainPendingLoop(events, stream, io.Discard, &errBuf, false)
	})
	if err != nil {
		t.Fatalf("drainPendingLoop phải trả về nil, được %v", err)
	}
	if !strings.Contains(errBuf.String(), "sáng tác hoàn thành") {
		t.Fatalf("sự kiện trên kênh còn mở phải được rút cạn sau khi stream đóng, được %q", errBuf.String())
	}
	select {
	case events <- host.Event{Summary: "probe"}:
		close(events)
	default:
		t.Fatal("events phải còn mở khi drainPendingLoop trả về")
	}
}
