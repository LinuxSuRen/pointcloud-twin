package server

import (
	"fmt"
	"net/http"
	"os"
	"path/filepath"

	"github.com/LinuxSuRen/pointcloud-twin/internal/export"
	"github.com/LinuxSuRen/pointcloud-twin/internal/pcd"
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
	var devices []proxy.Device
	if data, err := os.ReadFile(filepath.Join(j.dir, "devices.json")); err == nil {
		cfg, err := proxy.LoadFromBytes(data)
		if err == nil {
			devices = cfg.Devices
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
