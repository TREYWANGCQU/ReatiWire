// collaboration_tool_solution/team_collab/pkg/transfer/engine.go
package transfer

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sync"
	"time"
)

// ChunkSize 标准分块大小: 2MB (RFC 断点续传优化粒度)
const ChunkSize = 2 * 1024 * 1024

// TransferState 传输会话状态
type TransferState string

const (
	StatePending    TransferState = "PENDING"
	StateActive     TransferState = "TRANSFERRING"
	StatePaused     TransferState = "PAUSED"
	StateCompleted  TransferState = "COMPLETED"
	StateFailed     TransferState = "FAILED"
)

// FileManifest 文件分块清单与校验元数据
type FileManifest struct {
	FileID       string   `json:"file_id"`
	FileName     string   `json:"file_name"`
	TotalBytes   int64    `json:"total_bytes"`
	TotalChunks  int      `json:"total_chunks"`
	FullChecksum string   `json:"full_checksum"`
	ChunkHashes  []string `json:"chunk_hashes"`
}

// SessionStats 传输会话实时进度与指标
type SessionStats struct {
	FileID           string        `json:"file_id"`
	FileName         string        `json:"file_name"`
	TotalBytes       int64         `json:"total_bytes"`
	BytesTransferred int64         `json:"bytes_transferred"`
	ProgressPercent  float64       `json:"progress_percent"`
	CurrentChunk     int           `json:"current_chunk"`
	TotalChunks      int           `json:"total_chunks"`
	State            TransferState `json:"state"`
	SpeedBytesPerSec int64         `json:"speed_bytes_sec"`
	IsDirectP2P      bool          `json:"is_direct_p2p"` // 直连标记 (0 服务器带宽消耗)
	TargetPeerIP     string        `json:"target_peer_ip"`
}

// TransferEngine 点对点大文件分块断点续传引擎
type TransferEngine struct {
	mu           sync.RWMutex
	storageDir   string
	sessions     map[string]*SessionStats
	manifests    map[string]*FileManifest
	activeFiles  map[string]*os.File
}

// NewTransferEngine 创建文件传输引擎
func NewTransferEngine(storageDir string) (*TransferEngine, error) {
	if storageDir == "" {
		storageDir = filepath.Join(os.TempDir(), "reati_wire_transfers")
	}
	if err := os.MkdirAll(storageDir, 0755); err != nil {
		return nil, fmt.Errorf("failed to create storage dir: %w", err)
	}
	return &TransferEngine{
		storageDir:  storageDir,
		sessions:    make(map[string]*SessionStats),
		manifests:   make(map[string]*FileManifest),
		activeFiles: make(map[string]*os.File),
	}, nil
}

// PrepareUpload 扫描本地待发送文件并计算 2MB 分块清单
func (te *TransferEngine) PrepareUpload(filePath, targetPeerIP string) (*FileManifest, *SessionStats, error) {
	fi, err := os.Stat(filePath)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to stat file: %w", err)
	}
	if fi.IsDir() {
		return nil, nil, errors.New("directories cannot be sent directly")
	}

	file, err := os.Open(filePath)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to open file: %w", err)
	}
	defer file.Close()

	totalBytes := fi.Size()
	totalChunks := int((totalBytes + ChunkSize - 1) / ChunkSize)
	if totalChunks == 0 {
		totalChunks = 1
	}

	fullHasher := sha256.New()
	chunkHashes := make([]string, 0, totalChunks)

	buf := make([]byte, ChunkSize)
	for i := 0; i < totalChunks; i++ {
		n, rErr := file.Read(buf)
		if n > 0 {
			fullHasher.Write(buf[:n])
			chunkH := sha256.Sum256(buf[:n])
			chunkHashes = append(chunkHashes, hex.EncodeToString(chunkH[:]))
		}
		if rErr != nil && rErr != io.EOF {
			return nil, nil, fmt.Errorf("chunk hash error: %w", rErr)
		}
	}

	fullChecksum := hex.EncodeToString(fullHasher.Sum(nil))
	fileID := fmt.Sprintf("%s_%d", fullChecksum[:12], time.Now().Unix())

	manifest := &FileManifest{
		FileID:       fileID,
		FileName:     filepath.Base(filePath),
		TotalBytes:   totalBytes,
		TotalChunks:  totalChunks,
		FullChecksum: fullChecksum,
		ChunkHashes:  chunkHashes,
	}

	stats := &SessionStats{
		FileID:           fileID,
		FileName:         filepath.Base(filePath),
		TotalBytes:       totalBytes,
		BytesTransferred: 0,
		ProgressPercent:  0.0,
		CurrentChunk:     0,
		TotalChunks:      totalChunks,
		State:            StatePending,
		IsDirectP2P:      true, // 默认 P2P 直连
		TargetPeerIP:     targetPeerIP,
	}

	te.mu.Lock()
	te.manifests[fileID] = manifest
	te.sessions[fileID] = stats
	te.mu.Unlock()

	return manifest, stats, nil
}

// StoreIncomingChunk 接收端写入 2MB 分块并校验哈希
func (te *TransferEngine) StoreIncomingChunk(fileID string, chunkIndex int, data []byte) error {
	te.mu.Lock()
	manifest, exists := te.manifests[fileID]
	stats, hasStats := te.sessions[fileID]
	if !exists || !hasStats {
		te.mu.Unlock()
		return errors.New("unknown transfer session")
	}

	if chunkIndex < 0 || chunkIndex >= manifest.TotalChunks {
		te.mu.Unlock()
		return errors.New("chunk index out of bounds")
	}

	// 校验分块哈希
	expectedHash := manifest.ChunkHashes[chunkIndex]
	actualHash := sha256.Sum256(data)
	if hex.EncodeToString(actualHash[:]) != expectedHash {
		te.mu.Unlock()
		return fmt.Errorf("checksum mismatch on chunk %d", chunkIndex)
	}

	// 写入本地目标文件
	targetFilePath := filepath.Join(te.storageDir, manifest.FileName)
	f, fileOpen := te.activeFiles[fileID]
	if !fileOpen {
		var err error
		f, err = os.OpenFile(targetFilePath, os.O_CREATE|os.O_WRONLY, 0644)
		if err != nil {
			te.mu.Unlock()
			return fmt.Errorf("failed to open output file: %w", err)
		}
		te.activeFiles[fileID] = f
	}

	offset := int64(chunkIndex) * ChunkSize
	if _, err := f.WriteAt(data, offset); err != nil {
		te.mu.Unlock()
		return fmt.Errorf("failed to write chunk at offset %d: %w", offset, err)
	}

	// 更新实时统计
	stats.CurrentChunk = chunkIndex + 1
	stats.BytesTransferred += int64(len(data))
	if stats.TotalBytes > 0 {
		stats.ProgressPercent = (float64(stats.BytesTransferred) / float64(stats.TotalBytes)) * 100.0
	}
	stats.State = StateActive

	if stats.CurrentChunk >= stats.TotalChunks {
		stats.State = StateCompleted
		stats.ProgressPercent = 100.0
		_ = f.Close()
		delete(te.activeFiles, fileID)
	}
	te.mu.Unlock()

	return nil
}

// GetSessionStats 获取传输会话统计
func (te *TransferEngine) GetSessionStats(fileID string) *SessionStats {
	te.mu.RLock()
	defer te.mu.RUnlock()
	if s, ok := te.sessions[fileID]; ok {
		cp := *s
		return &cp
	}
	return nil
}
