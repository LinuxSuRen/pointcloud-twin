// Package pipeline 编排 PCD → LAS → 3D Tiles 的完整转换流水线。
package pipeline

import (
	"log/slog"
	"os"
	"path/filepath"
	"time"

	"github.com/LinuxSuRen/pointcloud-twin/internal/apperr"
	"github.com/LinuxSuRen/pointcloud-twin/internal/crs"
	"github.com/LinuxSuRen/pointcloud-twin/internal/downsample"
	"github.com/LinuxSuRen/pointcloud-twin/internal/las"
	"github.com/LinuxSuRen/pointcloud-twin/internal/pcd"
	"github.com/LinuxSuRen/pointcloud-twin/internal/proxy"
	"github.com/LinuxSuRen/pointcloud-twin/internal/tiler"
)

// ConvertOptions 是 convert 子命令的完整参数。
type ConvertOptions struct {
	Input         string
	OutputDir     string
	Origin        crs.ENUOrigin
	Voxel         float64
	PointsPerTile int
	Refine        string
	TilesVersion  string
	// DevicesFile 是可选的设备代理体配置；提供时校验并拷贝到输出目录。
	DevicesFile string
	// SkipLAS 为 true 时不写中间 LAS 文件。
	SkipLAS bool
	Log     *slog.Logger
	// ToolVersion 记录进转换报告。
	ToolVersion string
}

// Convert 执行完整转换：PCD →（可选降采样）→ LAS + 3D Tiles + 代理体配置 + 报告。
func Convert(opt ConvertOptions) error {
	log := opt.Log
	if log == nil {
		log = slog.Default()
	}
	started := time.Now()
	log.Info("转换开始", "stage", "convert",
		"input", opt.Input, "output", opt.OutputDir, "origin", opt.Origin.String())

	cloud, err := loadPCD(opt.Input, log)
	if err != nil {
		return err
	}
	inputPoints := len(cloud.Points)

	if opt.Voxel > 0 {
		t := time.Now()
		cloud.Points = downsample.Voxel(cloud.Points, opt.Voxel)
		cloud.RecomputeBounds()
		log.Info("体素降采样完成", "stage", "downsample",
			"before", inputPoints, "after", len(cloud.Points),
			"voxel", opt.Voxel, "dur", time.Since(t).Round(time.Millisecond).String())
		if len(cloud.Points) == 0 {
			return apperr.New(apperr.E1004, nil, "降采样后没有剩余点，请增大 --voxel")
		}
	}

	lasRel := ""
	if !opt.SkipLAS {
		lasPath := filepath.Join(opt.OutputDir, filepath.Base(opt.Input)+".las")
		t := time.Now()
		if err := las.Write(lasPath, cloud, las.WriteOptions{
			Origin: opt.Origin, GeneratingSoftware: opt.ToolVersion,
		}); err != nil {
			return err
		}
		lasRel = filepath.Base(lasPath)
		log.Info("LAS 写入完成", "stage", "las",
			"path", lasPath, "points", len(cloud.Points),
			"dur", time.Since(t).Round(time.Millisecond).String())
	}

	stats, err := tiler.Build(cloud, tiler.Options{
		OutputDir:     opt.OutputDir,
		PointsPerTile: opt.PointsPerTile,
		Refine:        opt.Refine,
		Version:       opt.TilesVersion,
		Origin:        opt.Origin,
		Log:           log,
	})
	if err != nil {
		return err
	}

	if opt.DevicesFile != "" {
		t := time.Now()
		cfg, err := proxy.Load(opt.DevicesFile)
		if err != nil {
			return err
		}
		if err := cfg.Validate(); err != nil {
			return err
		}
		for i := range cfg.Devices {
			cfg.Devices[i].Heading = proxy.NormalizeHeading(cfg.Devices[i].Heading)
		}
		org := opt.Origin
		cfg.Origin = &org
		if err := proxy.Save(filepath.Join(opt.OutputDir, "devices.json"), cfg); err != nil {
			return err
		}
		log.Info("设备代理体配置写入完成", "stage", "proxy",
			"devices", len(cfg.Devices),
			"dur", time.Since(t).Round(time.Millisecond).String())
	}

	rep := newReport(opt, cloud, stats, lasRel, inputPoints)
	if err := writeReport(filepath.Join(opt.OutputDir, "report.json"), rep); err != nil {
		return err
	}

	log.Info("转换完成", "stage", "convert",
		"points", len(cloud.Points), "tiles", stats.Tiles,
		"devicesProvided", opt.DevicesFile != "",
		"dur", time.Since(started).Round(time.Millisecond).String())
	return nil
}

// PCD2LASOptions 是 pcd2las 子命令参数。
type PCD2LASOptions struct {
	Input  string
	Output string
	Origin crs.ENUOrigin
	Voxel  float64
	Log    *slog.Logger
	// ToolVersion 记录进 LAS 生成软件字段。
	ToolVersion string
}

// PCD2LAS 执行 PCD → LAS 格式桥接。
func PCD2LAS(opt PCD2LASOptions) error {
	log := opt.Log
	if log == nil {
		log = slog.Default()
	}
	started := time.Now()
	log.Info("转换开始", "stage", "pcd2las", "input", opt.Input, "output", opt.Output,
		"origin", opt.Origin.String())

	cloud, err := loadPCD(opt.Input, log)
	if err != nil {
		return err
	}
	if opt.Voxel > 0 {
		t := time.Now()
		cloud.Points = downsample.Voxel(cloud.Points, opt.Voxel)
		cloud.RecomputeBounds()
		if len(cloud.Points) == 0 {
			return apperr.New(apperr.E1004, nil, "降采样后没有剩余点，请增大 --voxel")
		}
		log.Info("体素降采样完成", "stage", "downsample",
			"after", len(cloud.Points), "voxel", opt.Voxel,
			"dur", time.Since(t).Round(time.Millisecond).String())
	}
	if err := las.Write(opt.Output, cloud, las.WriteOptions{
		Origin: opt.Origin, GeneratingSoftware: opt.ToolVersion,
	}); err != nil {
		return err
	}
	log.Info("转换完成", "stage", "pcd2las",
		"points", len(cloud.Points),
		"dur", time.Since(started).Round(time.Millisecond).String())
	return nil
}

func loadPCD(path string, log *slog.Logger) (*pcd.Cloud, error) {
	t := time.Now()
	if _, err := os.Stat(path); err != nil {
		return nil, apperr.New(apperr.E1001, err, path)
	}
	cloud, err := pcd.Load(path)
	if err != nil {
		log.Debug("PCD 解析失败详情", "stage", "pcd.load", "err", err.Error())
		return nil, err
	}
	log.Info("PCD 读取完成", "stage", "pcd.load",
		"points", len(cloud.Points), "skipped", cloud.Skipped,
		"encoding", cloud.Header.Data,
		"minX", cloud.Min[0], "maxX", cloud.Max[0],
		"minY", cloud.Min[1], "maxY", cloud.Max[1],
		"minZ", cloud.Min[2], "maxZ", cloud.Max[2],
		"dur", time.Since(t).Round(time.Millisecond).String())
	return cloud, nil
}

func newReport(opt ConvertOptions, cloud *pcd.Cloud, stats *tiler.Stats, lasRel string, inputPoints int) *Report {
	rep := &Report{
		Tool:        "pointcloud-twin",
		Version:     opt.ToolVersion,
		Input:       opt.Input,
		GeneratedAt: time.Now().UTC().Format(time.RFC3339),
		Voxel:       opt.Voxel,
	}
	rep.Points.Input = inputPoints
	rep.Points.Tiled = stats.Points
	rep.Points.Skipped = cloud.Skipped
	if lasRel != "" {
		rep.LasPath = lasRel
	}
	rep.CRS.Type = "enu"
	rep.CRS.Origin = opt.Origin
	rep.Bounds.Min = cloud.Min
	rep.Bounds.Max = cloud.Max
	rep.Tiles.Count = stats.Tiles
	rep.Tiles.Version = opt.TilesVersion
	rep.Tiles.Refine = opt.Refine
	rep.Tiles.PointsPerTile = opt.PointsPerTile
	rep.Tiles.RootGeometricError = stats.RootGeometricError
	rep.Tiles.TreeDepth = stats.TreeDepth
	return rep
}
