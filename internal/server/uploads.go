// Package server 提供 pc2twin serve 的 HTTP 服务：
// 分片续传上传、异步转换任务（SSE 进度）、转换产物托管。
//
// 设计遵循长任务标准形态：POST 建 202+jobId、GET 查询兜底、DELETE 取消；
// cancelled 与 failed 严格区分；任务执行 recover 隔离；过期清理。
package server

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/LinuxSuRen/pointcloud-twin/internal/apperr"
)

const (
	purposePCD     = "pcd"
	purposeDevices = "devices"
)

// uploadSession 是一次分片上传会话。
type uploadSession struct {
	ID         string
	Filename   string
	Purpose    string
	Size       int64
	ChunkSize  int64
	Received   map[int64]bool
	State      string // pending | completed
	Path       string
	CreatedAt  time.Time
	LastActive time.Time
	mu         sync.Mutex
}

func (s *uploadSession) totalChunks() int64 {
	if s.Size <= 0 {
		return 0
	}
	return (s.Size + s.ChunkSize - 1) / s.ChunkSize
}

// expectedChunkSize 返回第 index 片的期望字节数（末片按剩余计算）。
func (s *uploadSession) expectedChunkSize(index int64) int64 {
	if index == s.totalChunks()-1 {
		return s.Size - index*s.ChunkSize
	}
	return s.ChunkSize
}

// uploadManager 管理上传会话与暂存文件。
type uploadManager struct {
	dir        string
	chunkSize  int64
	maxSize    int64
	pendingTTL time.Duration
	mu         sync.RWMutex
	sessions   map[string]*uploadSession
}

func newUploadManager(dir string, chunkSize, maxSize int64) (*uploadManager, error) {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, err
	}
	return &uploadManager{
		dir:        dir,
		chunkSize:  chunkSize,
		maxSize:    maxSize,
		pendingTTL: 2 * time.Hour,
		sessions:   map[string]*uploadSession{},
	}, nil
}

func newID() string {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		return fmt.Sprintf("%d", time.Now().UnixNano())
	}
	return hex.EncodeToString(b)
}

func (m *uploadManager) get(id string) (*uploadSession, bool) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	s, ok := m.sessions[id]
	return s, ok
}

func (m *uploadManager) remove(id string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	delete(m.sessions, id)
}

// Consume 把已完成上传的暂存文件移动到目标路径并注销会话（消费即清理）。
func (m *uploadManager) Consume(id, dstDir, dstName string) (string, error) {
	s, ok := m.get(id)
	if !ok {
		return "", apperr.New(apperr.E4001, nil, "上传会话不存在或已过期："+id)
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.State != "completed" {
		return "", apperr.New(apperr.E4001, nil, "上传尚未完成，不能使用")
	}
	if err := os.MkdirAll(dstDir, 0o755); err != nil {
		return "", err
	}
	dst := filepath.Join(dstDir, dstName)
	if err := os.Rename(s.Path, dst); err != nil {
		return "", err
	}
	m.remove(id)
	return dst, nil
}

// cleanup 删除超期的 pending 会话与暂存文件。
func (m *uploadManager) cleanup(now time.Time) {
	m.mu.Lock()
	var expired []*uploadSession
	for id, s := range m.sessions {
		if now.Sub(s.LastActive) > m.pendingTTL {
			expired = append(expired, s)
			delete(m.sessions, id)
		}
	}
	m.mu.Unlock()
	for _, s := range expired {
		os.Remove(s.Path)
	}
}

// ---- HTTP handlers ----

type errorBody struct {
	Error struct {
		Code    string `json:"code"`
		Message string `json:"message"`
	} `json:"error"`
}

func writeErr(w http.ResponseWriter, status int, code, message string) {
	var b errorBody
	b.Error.Code = code
	b.Error.Message = message
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(b)
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(v)
}

type initRequest struct {
	Filename string `json:"filename"`
	Size     int64  `json:"size"`
	Purpose  string `json:"purpose"`
}

func (m *uploadManager) handleInit(w http.ResponseWriter, r *http.Request) {
	var req initRequest
	if err := json.NewDecoder(io.LimitReader(r.Body, 1<<20)).Decode(&req); err != nil {
		writeErr(w, http.StatusBadRequest, "E7001", "请求体不是合法 JSON")
		return
	}
	if req.Purpose != purposePCD && req.Purpose != purposeDevices {
		writeErr(w, http.StatusBadRequest, "E7001", "purpose 仅支持 pcd 或 devices")
		return
	}
	name := filepath.Base(strings.TrimSpace(req.Filename))
	if name == "" || name == "." || name == ".." || strings.ContainsAny(name, `/\`) {
		writeErr(w, http.StatusBadRequest, "E7001", "filename 无效")
		return
	}
	ext := strings.ToLower(filepath.Ext(name))
	if req.Purpose == purposePCD && ext != ".pcd" {
		writeErr(w, http.StatusBadRequest, "E7001", "PCD 上传的文件扩展名应为 .pcd")
		return
	}
	if req.Purpose == purposeDevices && ext != ".json" {
		writeErr(w, http.StatusBadRequest, "E7001", "设备配置上传的文件扩展名应为 .json")
		return
	}
	if req.Size <= 0 || req.Size > m.maxSize {
		writeErr(w, http.StatusBadRequest, "E7001",
			fmt.Sprintf("文件大小需在 1 字节 ~ %d MB 之间", m.maxSize>>20))
		return
	}

	s := &uploadSession{
		ID:         newID(),
		Filename:   name,
		Purpose:    req.Purpose,
		Size:       req.Size,
		ChunkSize:  m.chunkSize,
		Received:   map[int64]bool{},
		State:      "pending",
		Path:       filepath.Join(m.dir, newID()+".part"),
		CreatedAt:  time.Now(),
		LastActive: time.Now(),
	}
	// 稀疏预分配目标文件：分片直接 WriteAt 直写，无「落盘+拼接」步骤
	f, err := os.OpenFile(s.Path, os.O_CREATE|os.O_RDWR, 0o644)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "E7002", "创建暂存文件失败")
		return
	}
	if err := f.Truncate(s.Size); err != nil {
		f.Close()
		os.Remove(s.Path)
		writeErr(w, http.StatusInternalServerError, "E7002", "预分配暂存文件失败")
		return
	}
	f.Close()

	m.mu.Lock()
	m.sessions[s.ID] = s
	m.mu.Unlock()

	writeJSON(w, http.StatusAccepted, map[string]any{
		"uploadId":  s.ID,
		"chunkSize": s.ChunkSize,
	})
}

// handleChunk 处理 PUT /api/uploads/{id}/chunks/{index}（raw body 直写）。
func (m *uploadManager) handleChunk(w http.ResponseWriter, r *http.Request, id, indexStr string) {
	s, ok := m.get(id)
	if !ok {
		writeErr(w, http.StatusNotFound, "E7003", "上传会话不存在或已过期")
		return
	}
	index, err := strconv.ParseInt(indexStr, 10, 64)
	if err != nil || index < 0 || index >= s.totalChunks() {
		writeErr(w, http.StatusBadRequest, "E7001", "分片序号无效")
		return
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	if s.State != "pending" {
		writeErr(w, http.StatusConflict, "E7003", "上传已定稿，不能再传分片")
		return
	}
	expected := s.expectedChunkSize(index)
	// 分片边界精确校验：读满期望后多出的字节视为请求粘连，直接报错
	buf := make([]byte, expected)
	n, err := io.ReadFull(io.LimitReader(r.Body, expected+1), buf)
	if err != nil && err != io.EOF && err != io.ErrUnexpectedEOF {
		writeErr(w, http.StatusBadRequest, "E7001", "读取分片失败")
		return
	}
	if int64(n) != expected {
		writeErr(w, http.StatusBadRequest, "E7001",
			fmt.Sprintf("分片大小不符：期望 %d 字节，实际 %d", expected, n))
		return
	}
	// 多余字节检测（LimitReader expected+1 后仍能读到数据说明粘连）
	var probe [1]byte
	if m2, _ := r.Body.Read(probe[:]); m2 > 0 {
		writeErr(w, http.StatusBadRequest, "E7001", "分片携带了多余字节（疑似请求粘连）")
		return
	}

	f, err := os.OpenFile(s.Path, os.O_RDWR, 0o644)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "E7002", "打开暂存文件失败")
		return
	}
	defer f.Close()
	if _, err := f.WriteAt(buf, index*s.ChunkSize); err != nil {
		writeErr(w, http.StatusInternalServerError, "E7002", "写入分片失败")
		return
	}
	s.Received[index] = true // 幂等：重传覆盖写 + 不重复计数
	s.LastActive = time.Now()

	writeJSON(w, http.StatusOK, map[string]any{"received": len(s.Received)})
}

type statusBody struct {
	Size           int64   `json:"size"`
	ChunkSize      int64   `json:"chunkSize"`
	TotalChunks    int64   `json:"totalChunks"`
	ReceivedChunks []int64 `json:"receivedChunks"`
	State          string  `json:"state"`
}

func (m *uploadManager) handleStatus(w http.ResponseWriter, r *http.Request, id string) {
	s, ok := m.get(id)
	if !ok {
		writeErr(w, http.StatusNotFound, "E7003", "上传会话不存在或已过期")
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	chunks := make([]int64, 0, len(s.Received))
	for i := range s.Received {
		chunks = append(chunks, i)
	}
	sort.Slice(chunks, func(i, j int) bool { return chunks[i] < chunks[j] })
	writeJSON(w, http.StatusOK, statusBody{
		Size: s.Size, ChunkSize: s.ChunkSize, TotalChunks: s.totalChunks(),
		ReceivedChunks: chunks, State: s.State,
	})
}

type completeRequest struct {
	SHA256 string `json:"sha256"`
}

func (m *uploadManager) handleComplete(w http.ResponseWriter, r *http.Request, id string) {
	s, ok := m.get(id)
	if !ok {
		writeErr(w, http.StatusNotFound, "E7003", "上传会话不存在或已过期")
		return
	}
	var req completeRequest
	if err := json.NewDecoder(io.LimitReader(r.Body, 1<<20)).Decode(&req); err != nil {
		writeErr(w, http.StatusBadRequest, "E7001", "请求体不是合法 JSON")
		return
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	if s.State == "completed" {
		writeJSON(w, http.StatusOK, map[string]any{"ok": true})
		return
	}
	if int64(len(s.Received)) != s.totalChunks() {
		missing := s.missingChunks()
		writeErr(w, http.StatusBadRequest, "E7003",
			fmt.Sprintf("分片不齐全，缺少 %d 片，首块缺号 %d", len(missing), firstOr(missing, -1)))
		return
	}
	if fi, err := os.Stat(s.Path); err != nil || fi.Size() != s.Size {
		writeErr(w, http.StatusBadRequest, "E7003", "文件大小校验失败")
		return
	}
	if req.SHA256 != "" {
		sum, err := fileSHA256(s.Path)
		if err != nil || sum != strings.ToLower(req.SHA256) {
			writeErr(w, http.StatusBadRequest, "E7003", "SHA-256 校验失败，文件可能损坏，请重新上传")
			return
		}
	}
	s.State = "completed"
	s.LastActive = time.Now()
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

func (m *uploadManager) handleDelete(w http.ResponseWriter, r *http.Request, id string) {
	s, ok := m.get(id)
	if !ok {
		writeErr(w, http.StatusNotFound, "E7003", "上传会话不存在或已过期")
		return
	}
	os.Remove(s.Path)
	m.remove(id)
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

func (s *uploadSession) missingChunks() []int64 {
	var missing []int64
	for i := int64(0); i < s.totalChunks(); i++ {
		if !s.Received[i] {
			missing = append(missing, i)
		}
	}
	return missing
}

func firstOr(xs []int64, fallback int64) int64 {
	if len(xs) > 0 {
		return xs[0]
	}
	return fallback
}

func fileSHA256(path string) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer f.Close()
	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return "", err
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}
