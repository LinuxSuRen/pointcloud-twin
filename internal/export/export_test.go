package export

import (
	"archive/zip"
	"bytes"
	"encoding/binary"
	"encoding/json"
	"math"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/LinuxSuRen/pointcloud-twin/internal/pcd"
	"github.com/LinuxSuRen/pointcloud-twin/internal/planes"
	"github.com/LinuxSuRen/pointcloud-twin/internal/proxy"
)

func sampleCloud(n int) *pcd.Cloud {
	points := make([]pcd.Point, 0, n)
	for i := 0; i < n; i++ {
		points = append(points, pcd.Point{
			X: float64(i % 10), Y: float64(i / 10 % 10), Z: float64(i / 100),
			R: 60, G: 120, B: 180, HasRGB: true,
		})
	}
	c := &pcd.Cloud{Points: points}
	c.RecomputeBounds()
	return c
}

func sampleDevices() []proxy.Device {
	return []proxy.Device{
		{DeviceID: "battery-cluster-01", DeviceType: "battery-cluster",
			Center: [3]float64{5, 5, 1.5}, Size: [3]float64{10, 2.5, 3}, Heading: 0},
		{DeviceID: "pcs-01", DeviceType: "pcs",
			Center: [3]float64{20, 5, 1.2}, Size: [3]float64{3, 2, 2.4}, Heading: 90},
	}
}

// parseGLB 解析 GLB 头与 JSON 块，供断言使用。
func parseGLB(t *testing.T, data []byte) (doc map[string]any, binLen int) {
	t.Helper()
	if binary.LittleEndian.Uint32(data[0:4]) != 0x46546C67 {
		t.Fatal("magic 错误")
	}
	total := int(binary.LittleEndian.Uint32(data[8:12]))
	if total != len(data) {
		t.Fatalf("total=%d len=%d", total, len(data))
	}
	jsonLen := int(binary.LittleEndian.Uint32(data[12:16]))
	var docOut map[string]any
	if err := json.Unmarshal(bytes.TrimRight(data[20:20+jsonLen], " "), &docOut); err != nil {
		t.Fatalf("JSON 解析失败：%v", err)
	}
	binLen = int(binary.LittleEndian.Uint32(data[20+jsonLen+4 : 20+jsonLen+8]))
	return docOut, binLen
}

func accessorsOf(doc map[string]any) []map[string]any {
	raw, _ := doc["accessors"].([]any)
	out := make([]map[string]any, 0, len(raw))
	for _, a := range raw {
		out = append(out, a.(map[string]any))
	}
	return out
}

func nodesOf(doc map[string]any) []map[string]any {
	raw, _ := doc["nodes"].([]any)
	out := make([]map[string]any, 0, len(raw))
	for _, a := range raw {
		out = append(out, a.(map[string]any))
	}
	return out
}

func TestBuildGLB(t *testing.T) {
	cloud := sampleCloud(150)
	data, err := BuildGLB(cloud, sampleDevices(), GLBOptions{})
	if err != nil {
		t.Fatal(err)
	}
	doc, _ := parseGLB(t, data)

	// 场景：root(z-up→y-up) → pointcloud + 2 设备
	nodes := nodesOf(doc)
	if len(nodes) != 4 {
		t.Fatalf("节点数 = %d，期望 root+点云+2 设备", len(nodes))
	}
	var root map[string]any
	for _, n := range nodes {
		if n["name"] == "root" {
			root = n
		}
	}
	if root == nil || len(root["matrix"].([]any)) != 16 {
		t.Fatal("根节点矩阵缺失")
	}
	names := map[string]bool{}
	for _, n := range nodes {
		names[n["name"].(string)] = true
	}
	if !names["pointcloud"] || !names["battery-cluster-01"] || !names["pcs-01"] {
		t.Fatalf("节点名异常：%v", names)
	}
	if len(root["children"].([]any)) != 3 {
		t.Fatal("根子节点数错误")
	}

	// 点云：POSITION/ COLOR_0 accessor 数量与 min/max
	accs := accessorsOf(doc)
	var posAcc map[string]any
	for _, a := range accs {
		if a["type"] == "VEC3" && a["count"].(float64) == 150 {
			posAcc = a
		}
	}
	if posAcc == nil {
		t.Fatal("缺少点云 POSITION accessor")
	}
	if mn := posAcc["min"].([]any); mn[0].(float64) != 0 || mn[2].(float64) != 0 {
		t.Fatalf("min 异常：%v", mn)
	}
	if mx := posAcc["max"].([]any); mx[0].(float64) != 9 {
		t.Fatalf("max 异常：%v", mx)
	}

	// 盒体网格：单位盒 24 顶点 / 36 索引，两台设备各自 TRS
	var boxIdxAcc map[string]any
	for _, a := range accs {
		if a["type"] == "SCALAR" && a["count"].(float64) == 36 {
			boxIdxAcc = a
		}
	}
	if boxIdxAcc == nil {
		t.Fatal("缺少盒体索引 accessor（36）")
	}
	var pcsNode map[string]any
	for _, n := range nodes {
		if n["name"] == "pcs-01" {
			pcsNode = n
		}
	}
	if pcsNode == nil {
		t.Fatal("缺少 pcs-01 节点")
	}
	scale := pcsNode["scale"].([]any)
	if scale[0].(float64) != 3 || scale[2].(float64) != 2.4 {
		t.Fatalf("pcs 尺寸缩放异常：%v", scale)
	}
	rot := pcsNode["rotation"].([]any)
	// heading 90° → 绕 Z 旋转 -90°：qz = sin(-45°) ≈ -0.7071
	if math.Abs(math.Abs(rot[2].(float64))-math.Sin(math.Pi/4)) > 1e-6 {
		t.Fatalf("pcs 朝向四元数异常：%v", rot)
	}
}

func TestBuildGLBVoxel(t *testing.T) {
	// 500 个点挤在 5m×5m×1m 内，体素 1.0 应大幅收敛
	points := make([]pcd.Point, 0, 500)
	for i := 0; i < 500; i++ {
		points = append(points, pcd.Point{
			X: float64(i) * 0.01, Y: float64(i%7) * 0.3, Z: float64(i%3) * 0.2,
		})
	}
	cloud := &pcd.Cloud{Points: points}
	cloud.RecomputeBounds()
	data, err := BuildGLB(cloud, nil, GLBOptions{Voxel: 1.0})
	if err != nil {
		t.Fatal(err)
	}
	doc, _ := parseGLB(t, data)
	accs := accessorsOf(doc)
	maxCount := 0.0
	for _, a := range accs {
		if c, ok := a["count"].(float64); ok && c > maxCount {
			maxCount = c
		}
	}
	if maxCount >= 500 {
		t.Fatalf("降采样未生效：max accessor count = %v", maxCount)
	}
}

func TestBuildGLBEmptyDevices(t *testing.T) {
	data, err := BuildGLB(sampleCloud(3), nil, GLBOptions{})
	if err != nil {
		t.Fatal(err)
	}
	doc, _ := parseGLB(t, data)
	if len(nodesOf(doc)) != 2 {
		t.Fatal("应只有 root + 点云两个节点")
	}
}

func TestBuildGLBWithPlanes(t *testing.T) {
	cloud := sampleCloud(150)
	// 一块 8×6 米地面 + 一块立面
	ground := planes.Plane{
		Normal: [3]float64{0, 0, 1}, Centroid: [3]float64{4, 4, 0}, Color: [3]uint8{90, 100, 80},
		Polygon: [][3]float64{{0, 0, 0}, {8, 0, 0}, {8, 6, 0}, {0, 6, 0}},
		Inliers: 1000,
	}
	wall := planes.Plane{
		Normal: [3]float64{1, 0, 0}, Centroid: [3]float64{8, 3, 1.5}, Color: [3]uint8{120, 130, 140},
		Polygon: [][3]float64{{8, 0, 0}, {8, 6, 0}, {8, 6, 3}, {8, 0, 3}},
		Inliers: 500,
	}
	data, err := BuildGLB(cloud, nil, GLBOptions{Planes: []planes.Plane{ground, wall}, PlaneDist: 0.05})
	if err != nil {
		t.Fatal(err)
	}
	doc, _ := parseGLB(t, data)
	nodes := nodesOf(doc)
	var planeNodes int
	for _, n := range nodes {
		name, _ := n["name"].(string)
		if strings.HasPrefix(name, "plane-") {
			planeNodes++
		}
	}
	if planeNodes != 2 {
		t.Fatalf("应有 2 个平面节点，实际 %d", planeNodes)
	}
	// 残余过滤：地面上的点（150 个样例点 z∈{0..1} 且都在 8×6 地面范围内…按几何统计）
	data2, _ := BuildGLB(cloud, nil, GLBOptions{Planes: []planes.Plane{ground}, PlaneDist: 0.05})
	doc2, _ := parseGLB(t, data2)
	var posCount float64
	for _, a := range accessorsOf(doc2) {
		if a["type"] == "VEC3" && a["count"].(float64) > posCount && a["count"].(float64) < 140 {
			posCount = a["count"].(float64)
		}
	}
	// z=0 的点被地面面片取代：60 个点中至少过滤掉一部分
	if posCount >= 150 {
		t.Fatalf("平面覆盖的点应被过滤：点云 accessor count = %v", posCount)
	}
}

func TestFilterResidual(t *testing.T) {
	ground := planes.Plane{
		Normal: [3]float64{0, 0, 1}, Centroid: [3]float64{0, 0, 0}, Color: [3]uint8{1, 2, 3},
		Polygon: [][3]float64{{-5, -5, 0}, {5, -5, 0}, {5, 5, 0}, {-5, 5, 0}},
	}
	pts := []pcd.Point{
		{X: 0, Y: 0, Z: 0.01}, // 地面上 → 过滤
		{X: 0, Y: 0, Z: 0.2},  // 距地面远 → 保留
		{X: 0, Y: 0, Z: 10},   // 高处 → 保留
		{X: 9, Y: 0, Z: 0},    // 平面外 → 保留
	}
	out := planes.FilterResidual(pts, []planes.Plane{ground}, 0.05)
	if len(out) != 3 {
		t.Fatalf("应保留 3 点，实际 %d", len(out))
	}
}

func TestZipDir(t *testing.T) {
	dir := t.TempDir()
	os.MkdirAll(filepath.Join(dir, "tiles"), 0o755)
	os.WriteFile(filepath.Join(dir, "tileset.json"), []byte("{}"), 0o644)
	os.WriteFile(filepath.Join(dir, "tiles", "t0.glb"), []byte("glTF...."), 0o644)
	os.WriteFile(filepath.Join(dir, "devices.json"), []byte("{}"), 0o644)

	var buf bytes.Buffer
	if err := ZipDir(&buf, dir); err != nil {
		t.Fatal(err)
	}
	zr, err := zip.NewReader(bytes.NewReader(buf.Bytes()), int64(buf.Len()))
	if err != nil {
		t.Fatal(err)
	}
	names := map[string]bool{}
	for _, f := range zr.File {
		names[f.Name] = true
		if strings.Contains(f.Name, "\\") {
			t.Fatalf("zip 内路径应为 / 分隔：%s", f.Name)
		}
	}
	for _, want := range []string{"tileset.json", "tiles/t0.glb", "devices.json"} {
		if !names[want] {
			t.Fatalf("zip 缺少 %s：%v", want, names)
		}
	}
}
