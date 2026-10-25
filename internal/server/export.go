package server

import (
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"

	"github.com/LinuxSuRen/pointcloud-twin/internal/export"
	"github.com/LinuxSuRen/pointcloud-twin/internal/pcd"
	"github.com/LinuxSuRen/pointcloud-twin/internal/pipeline"
	"github.com/LinuxSuRen/pointcloud-twin/internal/proxy"
)

// maxExportPoints 限制导出 GLB 的点数上限（单文件体积保护）。
const maxExportPoints = 8_000_000

// handleExportZip 打包任务产物为 zip 下载（3D Tiles 目录 + 配置 + 报告）。
func (s *Server) handleExportZip(w http.ResponseWriter, r *http.Request) {
	j, ok := s.jobs.get(r.PathValue("id"))
	if !ok {
		writeErr(w, http.StatusNotFound, "E7004", "任务不存在或已过期")
		return
	}
	outDir := filepath.Join(j.dir, "out")
	if fi, err := os.Stat(outDir); err != nil || !fi.IsDir() {
		writeErr(w, http.StatusNotFound, "E7004", "产物不存在")
		return
	}
	w.Header().Set("Content-Type", "application/zip")
	w.Header().Set("Content-Disposition",
		fmt.Sprintf("attachment; filename=pointcloud-twin-%s.zip", j.ID))
	if err := export.ZipDir(w, outDir); err != nil {
		// 响应头已写出，只能记录日志
		s.log.Error("zip 导出失败", "job", j.ID, "err", err.Error())
	}
}

// handleSaveDevices 接收查看器编辑后的 devices.json 写回任务产物（PUT）。
func (s *Server) handleSaveDevices(w http.ResponseWriter, r *http.Request) {
	j, ok := s.jobs.get(r.PathValue("id"))
	if !ok {
		writeErr(w, http.StatusNotFound, "E7004", "任务不存在或已过期")
		return
	}
	data, err := io.ReadAll(io.LimitReader(r.Body, 8<<20))
	if err != nil {
		writeErr(w, http.StatusBadRequest, "E7001", "读取请求体失败")
		return
	}
	cfg, err := proxy.LoadFromBytes(data)
	if err != nil {
		writeErr(w, http.StatusBadRequest, "E4001", "设备代理体配置解析失败")
		return
	}
	if err := cfg.Validate(); err != nil {
		writeErr(w, http.StatusBadRequest, "E4002", err.Error())
		return
	}
	dst := filepath.Join(j.dir, "out", "devices.json")
	if _, err := os.Stat(filepath.Join(j.dir, "out")); err != nil {
		writeErr(w, http.StatusNotFound, "E7004", "产物不存在")
		return
	}
	// 保留 origin（若前端未携带则从转换报告回填——报告是场景 ENU 原点的权威记录）
	if cfg.Origin == nil {
		if rep := readJobReport(s.log, filepath.Join(j.dir, "out", "report.json")); rep != nil {
			o := rep.CRS.Origin
			cfg.Origin = &o
		}
	}
	if err := proxy.Save(dst, cfg); err != nil {
		writeErr(w, http.StatusInternalServerError, "E2002", "devices.json 写入失败")
		return
	}
	s.log.Info("设备配置已写回", "job", j.ID, "devices", len(cfg.Devices))
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "devices": len(cfg.Devices)})
}

// readJobReport 读取任务转换报告（失败返回 nil，不阻断写回）。
func readJobReport(log *slog.Logger, path string) *pipeline.Report {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil
	}
	var rep pipeline.Report
	if err := json.Unmarshal(data, &rep); err != nil {
		log.Debug("report.json 解析失败", "err", err.Error())
		return nil
	}
	return &rep
}

// handleExportGLB 现场生成单文件 GLB（点云 + 设备代理体）下载。
func (s *Server) handleExportGLB(w http.ResponseWriter, r *http.Request) {
	j, ok := s.jobs.get(r.PathValue("id"))
	if !ok {
		writeErr(w, http.StatusNotFound, "E7004", "任务不存在或已过期")
		return
	}
	input := filepath.Join(j.dir, "input.pcd")
	if _, err := os.Stat(input); err != nil {
		writeErr(w, http.StatusNotFound, "E7004", "任务的输入点云已被清理")
		return
	}
	cloud, err := pcd.Load(input)
	if err != nil {
		writeErr(w, http.StatusBadRequest, "E1002", "任务输入点云读取失败")
		return
	}
	if len(cloud.Points) > maxExportPoints {
		writeErr(w, http.StatusRequestEntityTooLarge, "E7006",
			fmt.Sprintf("点数 %d 超过导出上限 %d，请导入时用降采样控制点数后再导出", len(cloud.Points), maxExportPoints))
		return
	}
	// 设备配置优先取产物中（标注编辑后）的版本，其次才是上传原件
	var devices []proxy.Device
	deviceSources := []string{
		filepath.Join(j.dir, "out", "devices.json"),
		filepath.Join(j.dir, "devices.json"),
	}
	for _, src := range deviceSources {
		if data, err := os.ReadFile(src); err == nil {
			if cfg, err := proxy.LoadFromBytes(data); err == nil {
				devices = cfg.Devices
				break
			}
		}
	}
	glb, err := export.BuildGLB(cloud, devices, export.GLBOptions{})
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "E9002", "GLB 生成失败")
		return
	}
	w.Header().Set("Content-Type", "model/gltf-binary")
	w.Header().Set("Content-Disposition",
		fmt.Sprintf("attachment; filename=pointcloud-twin-%s.glb", j.ID))
	w.Write(glb)
}
