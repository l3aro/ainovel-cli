package store

import (
	"bufio"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"sync/atomic"
	"time"

	"github.com/voocel/ainovel-cli/internal/domain"
)

const checkpointsFile = "meta/checkpoints.jsonl"

// CheckpointStore quản lý việc ghi thêm và truy vấn các điểm khôi phục ở cấp step.
// Định dạng trên đĩa: meta/checkpoints.jsonl, chỉ ghi thêm; truy vấn qua bản sao trên bộ nhớ.
// Bất biến: cache là bản sao của checkpoints.jsonl, được duy trì tập trung bởi Append/Reset.
// Đồng thời: cache được bảo vệ bởi io.mu, ghi dùng Lock, đọc dùng RLock.
type CheckpointStore struct {
	io        *IO
	seqGen    atomic.Int64
	cache     []domain.Checkpoint
	digestIdx map[checkpointKey]int
}

// checkpointKey định danh duy nhất (scope, step, digest) để tra cứu idempotent O(1).
// Scope là struct tương đương (so sánh được bằng ==) nên dùng struct làm khóa.
// Giá trị là chỉ mục tương ứng trong cache.
type checkpointKey struct {
	scope  domain.Scope
	step   string
	digest string
}

// normalizeScope chuẩn hóa Scope về dạng khóa theo đúng ngữ nghĩa của Scope.Matches:
// với loại không phải chapter/arc/volume (vd. global), Matches bỏ qua mọi trường số,
// nên khóa cũng phải bỏ qua chúng để giữ nguyên hành vi idempotent cũ.
func normalizeScope(s domain.Scope) domain.Scope {
	switch s.Kind {
	case domain.ScopeChapter, domain.ScopeArc, domain.ScopeVolume:
		return s
	default:
		s.Chapter, s.Volume, s.Arc = 0, 0, 0
		return s
	}
}

func checkpointKeyFor(cp domain.Checkpoint) checkpointKey {
	return checkpointKey{scope: normalizeScope(cp.Scope), step: cp.Step, digest: cp.Digest}
}

// NewCheckpointStore tạo kho lưu trữ điểm khôi phục, tải toàn bộ điểm khôi phục hiện có từ đĩa vào cache một lần.
func NewCheckpointStore(io *IO) *CheckpointStore {
	cs := &CheckpointStore{io: io}
	cs.loadFromDisk()
	return cs
}

// loadFromDisk đọc toàn bộ jsonl từ đĩa vào cache và khôi phục seqGen.
func (cs *CheckpointStore) loadFromDisk() {
	cs.io.mu.Lock()
	defer cs.io.mu.Unlock()

	cs.cache = readCheckpointsFile(cs.io.path(checkpointsFile))
	var maxSeq int64
	for _, cp := range cs.cache {
		if cp.Seq > maxSeq {
			maxSeq = cp.Seq
		}
	}
	cs.seqGen.Store(maxSeq)
	cs.rebuildDigestIndexLocked()
}

// rebuildDigestIndexLocked xây lại digestIdx từ cache. Chỉ gọi khi đang giữ io.mu (Lock ghi).
// Nếu cache có nhiều bản ghi trùng khóa, bản sau (mới nhất) thắng — khớp với
// quét ngược từ cuối của hành vi cũ.
func (cs *CheckpointStore) rebuildDigestIndexLocked() {
	cs.digestIdx = make(map[checkpointKey]int, len(cs.cache))
	for i, cp := range cs.cache {
		if cp.Digest == "" {
			continue
		}
		cs.digestIdx[checkpointKeyFor(cp)] = i
	}
}

// Append ghi thêm một điểm khôi phục.
// Idempotent: nếu Scope + Step + Digest giống hệt đã tồn tại thì bỏ qua ghi, trả về bản ghi hiện có.
func (cs *CheckpointStore) Append(scope domain.Scope, step, artifact, digest string) (*domain.Checkpoint, error) {
	cs.io.mu.Lock()
	defer cs.io.mu.Unlock()

	if digest != "" {
		if idx, ok := cs.digestIdx[checkpointKey{scope: normalizeScope(scope), step: step, digest: digest}]; ok {
			cp := cs.cache[idx]
			return &cp, nil
		}
	}

	// Chỉ tăng seq sau khi ghi thành công, tránh để lại khoảng trống số thứ tự vĩnh viễn khi ghi thất bại.
	// Đang giữ write lock của io.mu, không có tranh chấp đồng thời giữa Load và Store.
	seq := cs.seqGen.Load() + 1
	cp := domain.Checkpoint{
		Seq:        seq,
		Scope:      scope,
		Step:       step,
		Artifact:   artifact,
		Digest:     digest,
		OccurredAt: time.Now(),
	}

	data, err := json.Marshal(cp)
	if err != nil {
		return nil, err
	}
	data = append(data, '\n')
	if err := cs.io.AppendLineUnlocked(checkpointsFile, data); err != nil {
		return nil, err
	}
	cs.seqGen.Store(seq)
	cs.cache = append(cs.cache, cp)
	if digest != "" {
		cs.digestIdx[checkpointKeyFor(cp)] = len(cs.cache) - 1
	}
	return &cp, nil
}

// AppendArtifact tính toán chữ ký nội dung của sản phẩm rồi ghi thêm điểm khôi phục.
func (cs *CheckpointStore) AppendArtifact(scope domain.Scope, step, artifact string) (*domain.Checkpoint, error) {
	if artifact == "" {
		return cs.Append(scope, step, "", "")
	}
	data, err := cs.io.ReadFile(artifact)
	if err != nil {
		return nil, fmt.Errorf("digest artifact %s: %w", artifact, err)
	}
	sum := sha256.Sum256(data)
	return cs.Append(scope, step, artifact, "sha256:"+hex.EncodeToString(sum[:]))
}

// Latest trả về điểm khôi phục mới nhất của scope được chỉ định.
func (cs *CheckpointStore) Latest(scope domain.Scope) *domain.Checkpoint {
	cs.io.mu.RLock()
	defer cs.io.mu.RUnlock()
	for i := len(cs.cache) - 1; i >= 0; i-- {
		if cs.cache[i].Scope.Matches(scope) {
			cp := cs.cache[i]
			return &cp
		}
	}
	return nil
}

// LatestByStep trả về điểm khôi phục mới nhất của scope + step được chỉ định.
func (cs *CheckpointStore) LatestByStep(scope domain.Scope, step string) *domain.Checkpoint {
	cs.io.mu.RLock()
	defer cs.io.mu.RUnlock()
	for i := len(cs.cache) - 1; i >= 0; i-- {
		cp := cs.cache[i]
		if cp.Scope.Matches(scope) && cp.Step == step {
			return &cp
		}
	}
	return nil
}

// LatestGlobal trả về điểm khôi phục mới nhất toàn cục (không phân biệt scope).
func (cs *CheckpointStore) LatestGlobal() *domain.Checkpoint {
	cs.io.mu.RLock()
	defer cs.io.mu.RUnlock()
	if len(cs.cache) == 0 {
		return nil
	}
	cp := cs.cache[len(cs.cache)-1]
	return &cp
}

// All trả về bản sao danh sách toàn bộ điểm khôi phục (sắp xếp theo seq tăng dần).
func (cs *CheckpointStore) All() []domain.Checkpoint {
	cs.io.mu.RLock()
	defer cs.io.mu.RUnlock()
	if len(cs.cache) == 0 {
		return nil
	}
	out := make([]domain.Checkpoint, len(cs.cache))
	copy(out, cs.cache)
	return out
}

// Reset xóa toàn bộ file điểm khôi phục và cache. Chỉ dùng khi tạo tiểu thuyết mới.
// Xóa file trước rồi mới xóa bộ nhớ: nếu xóa file thất bại thì giữ nguyên cache và seqGen, tránh lệch trạng thái bộ nhớ và đĩa.
func (cs *CheckpointStore) Reset() error {
	cs.io.mu.Lock()
	defer cs.io.mu.Unlock()
	if err := cs.io.RemoveFileUnlocked(checkpointsFile); err != nil {
		return err
	}
	cs.seqGen.Store(0)
	cs.cache = nil
	// Khởi tạo lại map rỗng (không để nil) vì Append sau Reset sẽ ghi vào digestIdx.
	cs.digestIdx = make(map[checkpointKey]int)
	return nil
}

// readCheckpointsFile phân tích jsonl; bỏ qua các dòng sai định dạng để chịu lỗi cắt ngắn ở cuối file.
func readCheckpointsFile(path string) []domain.Checkpoint {
	f, err := os.Open(path)
	if err != nil {
		return nil
	}
	defer func() { _ = f.Close() }()

	var result []domain.Checkpoint
	scanner := bufio.NewScanner(f)
	scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	for scanner.Scan() {
		line := scanner.Bytes()
		if len(line) == 0 {
			continue
		}
		var cp domain.Checkpoint
		if json.Unmarshal(line, &cp) == nil {
			result = append(result, cp)
		}
	}
	return result
}
