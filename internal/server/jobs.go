package server

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/LinuxSuRen/pointcloud-twin/internal/apperr"
	"github.com/LinuxSuRen/pointcloud-twin/internal/crs"
	"github.com/LinuxSuRen/pointcloud-twin/internal/pipeline"
)

// JobState 是任务终态/运行态。
type JobState string

const (
	stateRunning   JobState = "running"
	stateSucceeded JobState = "succeeded"
	stateFailed    JobState = "failed"
	stateCancelled JobState = "cancelled"
)

func (s JobState) terminal() bool {
	return s == stateSucceeded || s == stateFailed || s == stateCancelled
}

// JobProgress 是任务进度快照（REST 与 SSE 同源同结构）。
type JobProgress struct {
	Stage   string  `json:"stage"`
	Percent float64 `json:"percent"`
	Message string  `json:"message"`
}

type jobError struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

type jobResult struct {
	TilesUrl   string `json:"tilesUrl"`
	DevicesUrl string `json:"devicesUrl,omitempty"`
	ReportUrl  string `json:"reportUrl"`
	HasDevices bool   `json:"hasDevices"`
}

// Job 是一次转换任务。
type Job struct {
	ID         string
	State      JobState
	Progress   JobProgress
	Error      *jobError
	Result     *jobResult
	CreatedAt  time.Time
	terminalAt time.Time

	dir    string
	ctx    context.Context
	cancel context.CancelFunc
	mu     sync.Mutex
}

// JobView 是对外的 JSON 视图（REST 与 SSE 同源同结构，不含内部字段）。
type JobView struct {
	ID        string      `json:"id"`
	State     JobState    `json:"state"`
	Progress  JobProgress `json:"progress"`
	Error     *jobError   `json:"error,omitempty"`
	Result    *jobResult  `json:"result,omitempty"`
	CreatedAt time.Time   `json:"createdAt"`
}

// panicHook 仅供测试注入：非 nil 时任务执行前触发 panic，验证 recover 隔离。
var panicHook func()

// JobManager 管理任务生命周期。
type JobManager struct {
	dataDir     string
	toolVersion string
	log         *slog.Logger
	recordTTL   time.Duration
	outTTL      time.Duration

	mu      sync.Mutex
	jobs    map[string]*Job
	running int
}

func newJobManager(dataDir, toolVersion string, log *slog.Logger) (*JobManager, error) {
	if err := os.MkdirAll(filepath.Join(dataDir, "jobs"), 0o755); err != nil {
		return nil, err
	}
	return &JobManager{
		dataDir:     dataDir,
		toolVersion: toolVersion,
		log:         log,
		recordTTL:   10 * time.Minute,
		outTTL:      24 * time.Hour,
		jobs:        map[string]*Job{},
	}, nil
}

// CreateJobRequest 是 POST /api/jobs 的请求体。
type CreateJobRequest struct {
	PCDUploadID     string `json:"pcdUploadId"`
	DevicesUploadID string `json:"devicesUploadId,omitempty"`
	Options         struct {
		CrsEnu        string  `json:"crsEnu,omitempty"`
		Voxel         float64 `json:"voxel,omitempty"`
		PointsPerTile int     `json:"pointsPerTile,omitempty"`
		Refine        string  `json:"refine,omitempty"`
		TilesVersion  string  `json:"tilesVersion,omitempty"`
	} `json:"options"`
}

// Create 校验上传、移动暂存文件并启动任务；并发超限时返回 409。
func (m *JobManager) Create(uploads *uploadManager, req CreateJobRequest) (*Job, int, error) {
	m.mu.Lock()
	if m.running >= 1 {
		m.mu.Unlock()
		return nil, http.StatusConflict, fmt.Errorf("已有转换任务在运行，请稍后再试")
	}
	m.running++
	m.mu.Unlock()

	rollback := func() {
		m.mu.Lock()
		m.running--
		m.mu.Unlock()
	}

	pcdSession, ok := uploads.get(req.PCDUploadID)
	if !ok || pcdSession.Purpose != purposePCD {
		rollback()
		return nil, http.StatusBadRequest, fmt.Errorf("PCD 上传不存在或 purpose 不符")
	}
	if pcdSession.State != "completed" {
		rollback()
		return nil, http.StatusBadRequest, fmt.Errorf("PCD 上传尚未完成")
	}
	var devicesSession *uploadSession
	if req.DevicesUploadID != "" {
		ds, ok := uploads.get(req.DevicesUploadID)
		if !ok || ds.Purpose != purposeDevices {
			rollback()
			return nil, http.StatusBadRequest, fmt.Errorf("设备配置上传不存在或 purpose 不符")
		}
		if ds.State != "completed" {
			rollback()
			return nil, http.StatusBadRequest, fmt.Errorf("设备配置上传尚未完成")
		}
		devicesSession = ds
	}

	job := &Job{ID: newID(), State: stateRunning, CreatedAt: time.Now()}
	job.dir = filepath.Join(m.dataDir, "jobs", job.ID)
	job.ctx, job.cancel = context.WithCancel(context.Background())
	job.Progress = JobProgress{Stage: "load", Percent: 0, Message: "任务已创建"}

	pcdPath, err := uploads.Consume(req.PCDUploadID, job.dir, "input.pcd")
	if err != nil {
		rollback()
		return nil, http.StatusBadRequest, fmt.Errorf("PCD 文件交接失败，请重新上传")
	}
	devicesPath := ""
	if devicesSession != nil {
		p, err := uploads.Consume(req.DevicesUploadID, job.dir, "devices.json")
		if err != nil {
			rollback()
			return nil, http.StatusBadRequest, fmt.Errorf("设备配置交接失败，请重新上传")
		}
		devicesPath = p
	}

	m.mu.Lock()
	m.jobs[job.ID] = job
	m.mu.Unlock()

	go m.run(job, req, pcdPath, devicesPath)
	return job, http.StatusAccepted, nil
}

// run 在独立 goroutine 中执行转换；recover 隔离 panic，一个任务崩溃不影响服务。
func (m *JobManager) run(job *Job, req CreateJobRequest, pcdPath, devicesPath string) {
	finish := func(state JobState, code, message string, result *jobResult) {
		job.mu.Lock()
		job.State = state
		job.terminalAt = time.Now()
		if code != "" {
			job.Error = &jobError{Code: code, Message: message}
		}
		job.Result = result
		job.mu.Unlock()
		m.mu.Lock()
		m.running--
		m.mu.Unlock()
	}

	defer func() {
		if r := recover(); r != nil {
			m.log.Error("任务 panic 已隔离", "job", job.ID, "panic", fmt.Sprint(r))
			finish(stateFailed, "E9001", "任务内部错误，请查看服务端日志", nil)
		}
	}()

	if panicHook != nil {
		panicHook()
	}

	origin := crs.ENUOrigin{}
	if req.Options.CrsEnu != "" {
		o, err := crs.ParseENUOrigin(req.Options.CrsEnu)
		if err != nil {
			finish(stateFailed, string(apperr.E3001), "ENU 原点参数格式无效："+req.Options.CrsEnu, nil)
			return
		}
		origin = o
	}

	outDir := filepath.Join(job.dir, "out")
	opt := pipeline.ConvertOptions{
		Input:         pcdPath,
		OutputDir:     outDir,
		Origin:        origin,
		Voxel:         req.Options.Voxel,
		PointsPerTile: req.Options.PointsPerTile,
		Refine:        req.Options.Refine,
		TilesVersion:  req.Options.TilesVersion,
		DevicesFile:   devicesPath,
		Log:           m.log,
		ToolVersion:   m.toolVersion,
		Progress: func(stage string, percent float64, message string) {
			job.mu.Lock()
			job.Progress = JobProgress{Stage: stage, Percent: percent, Message: message}
			job.mu.Unlock()
		},
	}

	err := pipeline.Convert(job.ctx, opt)
	switch {
	case err == nil:
		result := &jobResult{
			TilesUrl:   fmt.Sprintf("/api/jobs/%s/out/tileset.json", job.ID),
			ReportUrl:  fmt.Sprintf("/api/jobs/%s/out/report.json", job.ID),
			HasDevices: devicesPath != "",
		}
		if devicesPath != "" {
			result.DevicesUrl = fmt.Sprintf("/api/jobs/%s/out/devices.json", job.ID)
		}
		finish(stateSucceeded, "", "", result)
	case isCancelled(err):
		finish(stateCancelled, "", "任务已取消", nil)
	default:
		code, message := friendlyError(err)
		finish(stateFailed, code, message, nil)
	}
}

func isCancelled(err error) bool {
	return errors.Is(err, context.Canceled)
}

// friendlyError 把底层错误映射为稳定错误码 + 通俗文案。
func friendlyError(err error) (code, message string) {
	if c, ok := apperr.From(err); ok {
		return string(c), err.Error()
	}
	return "E9002", "转换失败，请检查输入文件后重试"
}

func (m *JobManager) get(id string) (*Job, bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	j, ok := m.jobs[id]
	return j, ok
}

// Snapshot 返回任务的只读视图（REST 与 SSE 同源）。
func (j *Job) Snapshot() JobView {
	j.mu.Lock()
	defer j.mu.Unlock()
	view := JobView{ID: j.ID, State: j.State, Progress: j.Progress, CreatedAt: j.CreatedAt}
	if j.Error != nil {
		e := *j.Error
		view.Error = &e
	}
	if j.Result != nil {
		r := *j.Result
		view.Result = &r
	}
	return view
}

// Cancel 请求取消任务（幂等；终态任务为 no-op）。
func (m *JobManager) Cancel(id string) bool {
	j, ok := m.get(id)
	if !ok {
		return false
	}
	j.mu.Lock()
	terminal := j.State.terminal()
	j.mu.Unlock()
	if !terminal {
		j.cancel()
	}
	return true
}

// cleanup 清理过期任务记录与产物目录。
func (m *JobManager) cleanup(now time.Time) {
	m.mu.Lock()
	var done []*Job
	for id, j := range m.jobs {
		j.mu.Lock()
		terminal := j.State.terminal()
		age := now.Sub(j.terminalAt)
		j.mu.Unlock()
		if terminal && age > m.recordTTL {
			delete(m.jobs, id)
		}
		if terminal && age > m.outTTL {
			done = append(done, j)
		}
	}
	m.mu.Unlock()
	for _, j := range done {
		os.RemoveAll(j.dir)
	}
}

// ---- HTTP handlers ----

func (m *JobManager) handleCreate(uploads *uploadManager) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var req CreateJobRequest
		if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<20)).Decode(&req); err != nil {
			writeErr(w, http.StatusBadRequest, "E7001", "请求体不是合法 JSON")
			return
		}
		job, status, err := m.Create(uploads, req)
		if err != nil {
			writeErr(w, status, "E7001", err.Error())
			return
		}
		writeJSON(w, http.StatusAccepted, job.Snapshot())
	}
}

func (m *JobManager) handleGet(w http.ResponseWriter, r *http.Request, id string) {
	j, ok := m.get(id)
	if !ok {
		writeErr(w, http.StatusNotFound, "E7004", "任务不存在或已过期")
		return
	}
	writeJSON(w, http.StatusOK, j.Snapshot())
}

func (m *JobManager) handleCancel(w http.ResponseWriter, r *http.Request, id string) {
	if !m.Cancel(id) {
		writeErr(w, http.StatusNotFound, "E7004", "任务不存在或已过期")
		return
	}
	writeJSON(w, http.StatusAccepted, map[string]any{"cancelling": true})
}

// handleEvents 是 SSE 进度流：连接即推快照、变化才写帧、终态即关。
func (m *JobManager) handleEvents(w http.ResponseWriter, r *http.Request, id string) {
	j, ok := m.get(id)
	if !ok {
		writeErr(w, http.StatusNotFound, "E7004", "任务不存在或已过期")
		return
	}
	flusher, ok := w.(http.Flusher)
	if !ok {
		writeErr(w, http.StatusInternalServerError, "E7005", "当前连接不支持流式响应")
		return
	}
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	// 经反向代理时禁止缓冲，否则事件到不了客户端
	w.Header().Set("X-Accel-Buffering", "no")

	send := func(snap JobView) error {
		data, err := json.Marshal(snap)
		if err != nil {
			return err
		}
		if _, err := fmt.Fprintf(w, "event: job\ndata: %s\n\n", data); err != nil {
			return err
		}
		flusher.Flush()
		return nil
	}

	snap := j.Snapshot()
	if err := send(snap); err != nil {
		return
	}
	if snap.State.terminal() {
		return
	}

	ticker := time.NewTicker(500 * time.Millisecond)
	defer ticker.Stop()
	for {
		select {
		case <-r.Context().Done():
			// 客户端断开，退出（EventSource 会自动重连）
			return
		case <-ticker.C:
			cur := j.Snapshot()
			if cur.State != snap.State || cur.Progress != snap.Progress {
				snap = cur
				if err := send(snap); err != nil {
					return
				}
			}
			if snap.State.terminal() {
				// 终态推完即关，连接不挂死
				return
			}
		}
	}
}
