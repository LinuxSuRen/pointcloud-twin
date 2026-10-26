package pipeline

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"testing"

	"github.com/LinuxSuRen/pointcloud-twin/internal/crs"
	"github.com/LinuxSuRen/pointcloud-twin/internal/pcd"
	"github.com/LinuxSuRen/pointcloud-twin/internal/pcdfix"
	"github.com/LinuxSuRen/pointcloud-twin/internal/planes"
	"github.com/LinuxSuRen/pointcloud-twin/internal/proxy"
)

func quietLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

func fixturePCD(t *testing.T, dir string) string {
	t.Helper()
	points := make([]pcd.Point, 0, 8000)
	// 模拟三排「集装箱」+ 地面
	for row := 0; row < 3; row++ {
		for i := 0; i < 40; i++ {
			for j := 0; j < 20; j++ {
				for k := 0; k < 3; k++ {
					points = append(points, pcd.Point{
						X: float64(i) * 0.5,
						Y: float64(row)*8 + float64(j)*0.4,
						Z: float64(k) * 1.2,
						R: uint8(80 + row*40), G: 120, B: 160,
						HasRGB: true, Intensity: 1000, HasIntensity: true,
					})
				}
			}
		}
	}
	path := filepath.Join(dir, "site.pcd")
	if err := pcdfix.WriteFileBinary(path, points); err != nil {
		t.Fatal(err)
	}
	return path
}

func fixtureDevices(t *testing.T, dir string) string {
	t.Helper()
	cfg := &proxy.Config{
		Devices: []proxy.Device{
			{DeviceID: "battery-cluster-01", DeviceType: "battery-cluster",
				Center: [3]float64{10, 4, 1.8}, Size: [3]float64{10, 3, 3.6}, Heading: 0},
			{DeviceID: "pcs-01", DeviceType: "pcs",
				Center: [3]float64{10, 12, 1.2}, Size: [3]float64{8, 2, 2.4}, Heading: 0},
		},
	}
	path := filepath.Join(dir, "devices.json")
	data, _ := json.MarshalIndent(cfg, "", "  ")
	if err := os.WriteFile(path, data, 0o644); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestConvertEndToEnd(t *testing.T) {
	dir := t.TempDir()
	input := fixturePCD(t, dir)
	devices := fixtureDevices(t, dir)
	out := filepath.Join(dir, "out")

	opt := ConvertOptions{
		Input:         input,
		OutputDir:     out,
		Origin:        crs.ENUOrigin{Lon: 121.47, Lat: 31.23, H: 10, Explicit: true},
		PointsPerTile: 2000,
		Refine:        "add",
		TilesVersion:  "1.1",
		DevicesFile:   devices,
		Log:           quietLogger(),
		ToolVersion:   "test",
	}
	if err := Convert(context.Background(), opt); err != nil {
		t.Fatal(err)
	}

	for _, name := range []string{"tileset.json", "report.json", "devices.json", "site.pcd.las"} {
		if _, err := os.Stat(filepath.Join(out, name)); err != nil {
			t.Fatalf("缺少输出文件 %s：%v", name, err)
		}
	}
	entries, err := os.ReadDir(filepath.Join(out, "tiles"))
	if err != nil || len(entries) == 0 {
		t.Fatalf("瓦片目录异常：%v", err)
	}

	// 报告内容
	raw, _ := os.ReadFile(filepath.Join(out, "report.json"))
	var rep Report
	if err := json.Unmarshal(raw, &rep); err != nil {
		t.Fatal(err)
	}
	if rep.Points.Input == 0 || rep.Points.Tiled != rep.Points.Input {
		t.Fatalf("报告点数异常：%+v", rep.Points)
	}
	if rep.CRS.Origin.Lon != 121.47 || !rep.CRS.Origin.Explicit {
		t.Fatalf("报告 CRS 异常：%+v", rep.CRS)
	}
	if rep.Tiles.Count == 0 {
		t.Fatalf("报告瓦片数异常：%+v", rep.Tiles)
	}

	// 输出 devices.json 带上了 ENU 原点
	draw, _ := os.ReadFile(filepath.Join(out, "devices.json"))
	var dc proxy.Config
	if err := json.Unmarshal(draw, &dc); err != nil {
		t.Fatal(err)
	}
	if dc.Origin == nil || dc.Origin.Lat != 31.23 {
		t.Fatalf("输出 devices.json 缺少 ENU 原点：%+v", dc.Origin)
	}
}

func TestConvertSkipLASAndVoxel(t *testing.T) {
	dir := t.TempDir()
	input := fixturePCD(t, dir)
	out := filepath.Join(dir, "out2")
	opt := ConvertOptions{
		Input: input, OutputDir: out, Voxel: 0.5,
		PointsPerTile: 5000, SkipLAS: true, Log: quietLogger(),
	}
	if err := Convert(context.Background(), opt); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(out, "site.pcd.las")); !os.IsNotExist(err) {
		t.Fatal("skip-las 时不应写 LAS")
	}
	rep := readReport(t, out)
	if rep.Voxel != 0.5 || rep.Points.Tiled >= rep.Points.Input {
		t.Fatalf("降采样报告异常：%+v", rep.Points)
	}
}

// TestConvertWithPlanes 验证平面分割阶段产物与报告。
func TestConvertWithPlanes(t *testing.T) {
	dir := t.TempDir()
	input := fixturePCD(t, dir)
	out := filepath.Join(dir, "out-planes")
	opt := ConvertOptions{
		Input: input, OutputDir: out,
		PointsPerTile: 5000,
		PlaneSpec:     "dist=0.05,min=300,grid=0.2,tol=0.3",
		Log:           quietLogger(),
	}
	if err := Convert(context.Background(), opt); err != nil {
		t.Fatal(err)
	}
	// planes.json 存在且带平面
	dist, ps, err := planes.Load(filepath.Join(out, "planes.json"))
	if err != nil {
		t.Fatal(err)
	}
	if dist != 0.05 || len(ps) == 0 {
		t.Fatalf("planes.json 异常：dist=%v n=%d", dist, len(ps))
	}
	rep := readReport(t, out)
	if rep.Planes == nil || !rep.Planes.Enabled || rep.Planes.Count == 0 || rep.Planes.Coverage <= 0 {
		t.Fatalf("报告平面段异常：%+v", rep.Planes)
	}
	if rep.Tiles.Planes != len(ps) {
		t.Fatalf("瓦片平面数 %d 与检测数 %d 不符", rep.Tiles.Planes, len(ps))
	}
	// 残余点进入点瓦片：tiled < input（fixture 以平面为主）
	if rep.Points.Tiled >= rep.Points.Input {
		t.Fatalf("平面分割后残余点应少于输入：%+v", rep.Points)
	}
}

func TestConvertInputMissing(t *testing.T) {
	dir := t.TempDir()
	err := Convert(context.Background(), ConvertOptions{
		Input: filepath.Join(dir, "nope.pcd"), OutputDir: dir, Log: quietLogger(),
	})
	if err == nil {
		t.Fatal("应报错")
	}
}

// TestConvertCancelled 验证 ctx 取消在阶段边界生效并原样返回取消错误。
func TestConvertCancelled(t *testing.T) {
	dir := t.TempDir()
	input := fixturePCD(t, dir)
	ctx, cancel := context.WithCancel(context.Background())
	cancel() // 进入前即取消
	err := Convert(ctx, ConvertOptions{
		Input: input, OutputDir: filepath.Join(dir, "out"), Log: quietLogger(),
	})
	if err != context.Canceled {
		t.Fatalf("应返回 context.Canceled，实际：%v", err)
	}
}

func TestPCD2LAS(t *testing.T) {
	dir := t.TempDir()
	input := fixturePCD(t, dir)
	out := filepath.Join(dir, "site.las")
	if err := PCD2LAS(PCD2LASOptions{
		Input: input, Output: out,
		Origin: crs.ENUOrigin{Lon: 0, Lat: 0}, Log: quietLogger(),
	}); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(out)
	if err != nil {
		t.Fatal(err)
	}
	if string(data[0:4]) != "LASF" {
		t.Fatal("LAS magic 错误")
	}
}

func readReport(t *testing.T, dir string) *Report {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join(dir, "report.json"))
	if err != nil {
		t.Fatal(err)
	}
	var rep Report
	if err := json.Unmarshal(raw, &rep); err != nil {
		t.Fatal(err)
	}
	return &rep
}
