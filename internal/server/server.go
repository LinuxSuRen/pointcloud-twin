package server

import (
	"log/slog"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

// Options 控制 serve 行为。
type Options struct {
	DataDir     string
	Addr        string
	ChunkSize   int64 // 分片大小（字节）
	MaxUpload   int64 // 单文件上限（字节）
	ToolVersion string
	Log         *slog.Logger
}

// Server 是 pc2twin 的 HTTP 服务。
type Server struct {
	opt     Options
	log     *slog.Logger
	uploads *uploadManager
	jobs    *JobManager
	mux     *http.ServeMux
	stop    chan struct{}
}

// New 构造服务（负责目录初始化、路由注册、清理协程）。
func New(opt Options) (*Server, error) {
	log := opt.Log
	if log == nil {
		log = slog.Default()
	}
	uploads, err := newUploadManager(filepath.Join(opt.DataDir, "uploads"), opt.ChunkSize, opt.MaxUpload)
	if err != nil {
		return nil, err
	}
	jobs, err := newJobManager(opt.DataDir, opt.ToolVersion, log)
	if err != nil {
		return nil, err
	}
	s := &Server{opt: opt, log: log, uploads: uploads, jobs: jobs, mux: http.NewServeMux(), stop: make(chan struct{})}
	s.routes()
	go s.cleanupLoop()
	return s, nil
}

// Handler 暴露 http.Handler（测试与自定义服务器复用）。
func (s *Server) Handler() http.Handler {
	return s.withLogging(s.mux)
}

// ListenAndServe 阻塞启动。
func (s *Server) ListenAndServe() error {
	ln, err := net.Listen("tcp", s.opt.Addr)
	if err != nil {
		return err
	}
	srv := &http.Server{Handler: s.Handler()}
	s.log.Info("pc2twin serve 已启动", "addr", "http://"+ln.Addr().String(),
		"dataDir", s.opt.DataDir)
	return srv.Serve(ln)
}

// Close 停止清理协程。
func (s *Server) Close() {
	close(s.stop)
}

func (s *Server) routes() {
	s.mux.HandleFunc("POST /api/uploads/init", s.uploads.handleInit)
	s.mux.HandleFunc("PUT /api/uploads/{id}/chunks/{index}", func(w http.ResponseWriter, r *http.Request) {
		s.uploads.handleChunk(w, r, r.PathValue("id"), r.PathValue("index"))
	})
	s.mux.HandleFunc("GET /api/uploads/{id}", func(w http.ResponseWriter, r *http.Request) {
		s.uploads.handleStatus(w, r, r.PathValue("id"))
	})
	s.mux.HandleFunc("POST /api/uploads/{id}/complete", func(w http.ResponseWriter, r *http.Request) {
		s.uploads.handleComplete(w, r, r.PathValue("id"))
	})
	s.mux.HandleFunc("DELETE /api/uploads/{id}", func(w http.ResponseWriter, r *http.Request) {
		s.uploads.handleDelete(w, r, r.PathValue("id"))
	})

	s.mux.HandleFunc("POST /api/jobs", s.jobs.handleCreate(s.uploads))
	s.mux.HandleFunc("GET /api/jobs/{id}", func(w http.ResponseWriter, r *http.Request) {
		s.jobs.handleGet(w, r, r.PathValue("id"))
	})
	s.mux.HandleFunc("DELETE /api/jobs/{id}", func(w http.ResponseWriter, r *http.Request) {
		s.jobs.handleCancel(w, r, r.PathValue("id"))
	})
	s.mux.HandleFunc("GET /api/jobs/{id}/events", func(w http.ResponseWriter, r *http.Request) {
		s.jobs.handleEvents(w, r, r.PathValue("id"))
	})
	s.mux.HandleFunc("GET /api/jobs/{id}/out/{path...}", s.handleJobOut)
	s.mux.HandleFunc("GET /api/jobs/{id}/export", s.handleExportZip)
	s.mux.HandleFunc("GET /api/jobs/{id}/export/glb", s.handleExportGLB)
}

// handleJobOut 托管转换产物（tileset.json / tiles / devices.json / report.json）。
func (s *Server) handleJobOut(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	j, ok := s.jobs.get(id)
	if !ok {
		writeErr(w, http.StatusNotFound, "E7004", "任务不存在或已过期")
		return
	}
	rel := filepath.ToSlash(r.PathValue("path"))
	rel = strings.TrimPrefix(rel, "/")
	if rel == "" || strings.Contains(rel, "..") {
		writeErr(w, http.StatusBadRequest, "E7001", "路径无效")
		return
	}
	full := filepath.Join(j.dir, "out", filepath.FromSlash(rel))
	if !strings.HasPrefix(full, filepath.Join(j.dir, "out")) {
		writeErr(w, http.StatusBadRequest, "E7001", "路径无效")
		return
	}
	fi, err := os.Stat(full)
	if err != nil || fi.IsDir() {
		writeErr(w, http.StatusNotFound, "E7004", "产物不存在")
		return
	}
	w.Header().Set("Content-Type", contentTypeOf(rel))
	http.ServeFile(w, r, full)
}

func contentTypeOf(name string) string {
	switch strings.ToLower(filepath.Ext(name)) {
	case ".json":
		return "application/json"
	case ".glb":
		return "model/gltf-binary"
	case ".pnts":
		return "application/octet-stream"
	case ".las":
		return "application/octet-stream"
	default:
		return "application/octet-stream"
	}
}

func (s *Server) withLogging(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		next.ServeHTTP(w, r)
		s.log.Debug("http", "method", r.Method, "path", r.URL.Path,
			"dur", time.Since(start).Round(time.Millisecond).String())
	})
}

func (s *Server) cleanupLoop() {
	ticker := time.NewTicker(time.Minute)
	defer ticker.Stop()
	for {
		select {
		case <-s.stop:
			return
		case <-ticker.C:
			now := time.Now()
			s.uploads.cleanup(now)
			s.jobs.cleanup(now)
		}
	}
}

// ParseSizeMB 把 MB 数转换为字节（serve 命令行用）。
func ParseSizeMB(mb string) (int64, error) {
	v, err := strconv.ParseFloat(mb, 64)
	if err != nil || v <= 0 {
		return 0, err
	}
	return int64(v * (1 << 20)), nil
}
