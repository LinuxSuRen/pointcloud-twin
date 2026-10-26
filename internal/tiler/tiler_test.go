package tiler

import (
	"bytes"
	"encoding/binary"
	"encoding/json"
	"math"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/LinuxSuRen/pointcloud-twin/internal/crs"
	"github.com/LinuxSuRen/pointcloud-twin/internal/pcd"
	"github.com/LinuxSuRen/pointcloud-twin/internal/planes"
)

func gridCloud(n int) *pcd.Cloud {
	// n×n×n 均匀网格点，颜色随坐标渐变
	var points []pcd.Point
	step := 2.0
	for i := 0; i < n; i++ {
		for j := 0; j < n; j++ {
			for k := 0; k < n; k++ {
				points = append(points, pcd.Point{
					X: float64(i) * step, Y: float64(j) * step, Z: float64(k) * step,
					R: uint8(i * 255 / n), G: uint8(j * 255 / n), B: uint8(k * 255 / n),
					HasRGB: true,
				})
			}
		}
	}
	c := &pcd.Cloud{Points: points}
	c.RecomputeBounds()
	return c
}

func TestEncodeGLB(t *testing.T) {
	pos := []float32{0, 0, 0, 1, 1, 1, -2, 0.5, 3}
	col := []uint8{255, 0, 0, 255, 0, 255, 0, 255, 0, 0, 255, 255}
	data, err := encodeGLB(pos, col)
	if err != nil {
		t.Fatal(err)
	}
	if binary.LittleEndian.Uint32(data[0:4]) != glbMagic {
		t.Fatal("magic 错误")
	}
	if binary.LittleEndian.Uint32(data[4:8]) != 2 {
		t.Fatal("version 错误")
	}
	total := int(binary.LittleEndian.Uint32(data[8:12]))
	if total != len(data) || total%4 != 0 {
		t.Fatalf("total = %d，len = %d", total, len(data))
	}
	jsonLen := int(binary.LittleEndian.Uint32(data[12:16]))
	if binary.LittleEndian.Uint32(data[16:20]) != chunkJSON {
		t.Fatal("JSON chunk 类型错误")
	}
	var doc struct {
		Nodes []struct {
			Matrix [16]float32 `json:"matrix"`
		} `json:"nodes"`
		Meshes []struct {
			Primitives []struct {
				Mode       int            `json:"mode"`
				Attributes map[string]int `json:"attributes"`
			} `json:"primitives"`
		} `json:"meshes"`
		Accessors []struct {
			Type          string    `json:"type"`
			ComponentType int       `json:"componentType"`
			Count         int       `json:"count"`
			Min           []float32 `json:"min"`
			Max           []float32 `json:"max"`
			Normalized    bool      `json:"normalized"`
		} `json:"accessors"`
		Buffers []struct {
			ByteLength int `json:"byteLength"`
		} `json:"buffers"`
	}
	if err := json.Unmarshal(bytes.TrimRight(data[20:20+jsonLen], " "), &doc); err != nil {
		t.Fatalf("JSON 解析失败：%v", err)
	}
	if len(doc.Nodes) == 0 || doc.Nodes[0].Matrix != zUpToYUpMatrix {
		t.Fatal("根节点矩阵错误")
	}
	if doc.Meshes[0].Primitives[0].Mode != 0 {
		t.Fatal("POINTS 模式错误")
	}
	if doc.Accessors[0].Type != "VEC3" || doc.Accessors[0].Count != 3 {
		t.Fatal("POSITION accessor 错误")
	}
	if doc.Accessors[0].Min[0] != -2 || doc.Accessors[0].Max[0] != 1 {
		t.Fatalf("min/max 错误：%v %v", doc.Accessors[0].Min, doc.Accessors[0].Max)
	}
	if !doc.Accessors[1].Normalized || doc.Accessors[1].ComponentType != 5121 {
		t.Fatal("COLOR_0 accessor 错误")
	}
	if doc.Buffers[0].ByteLength != len(pos)*4+len(col) {
		t.Fatal("buffer 长度错误")
	}
}

func TestEncodePnts(t *testing.T) {
	pos := []float32{0, 0, 0, 1, 1, 1}
	col := []uint8{255, 0, 0, 0, 255, 0}
	data, err := encodePnts(pos, col)
	if err != nil {
		t.Fatal(err)
	}
	if binary.LittleEndian.Uint32(data[0:4]) != pntsMagic {
		t.Fatal("magic 错误")
	}
	total := int(binary.LittleEndian.Uint32(data[8:12]))
	if total%8 != 0 || total > len(data) || total+8 < len(data) {
		t.Fatalf("总长对齐错误：total=%d len=%d", total, len(data))
	}
	ftLen := int(binary.LittleEndian.Uint32(data[12:16]))
	if (28+ftLen)%8 != 0 {
		t.Fatalf("Feature Table 二进制未 8 字节对齐：%d", 28+ftLen)
	}
	var ft struct {
		PointsLength int `json:"POINTS_LENGTH"`
		Position     struct {
			ByteOffset int `json:"byteOffset"`
		} `json:"POSITION"`
		RGB struct {
			ByteOffset int `json:"byteOffset"`
		} `json:"RGB"`
	}
	if err := json.Unmarshal(bytes.TrimRight(data[28:28+ftLen], " "), &ft); err != nil {
		t.Fatal(err)
	}
	if ft.PointsLength != 2 {
		t.Fatalf("POINTS_LENGTH = %d", ft.PointsLength)
	}
	body := data[28+ftLen:]
	x := math.Float32frombits(binary.LittleEndian.Uint32(body[ft.Position.ByteOffset:]))
	if x != 0 {
		t.Fatalf("首点 X = %v", x)
	}
	r := body[ft.RGB.ByteOffset]
	if r != 255 {
		t.Fatalf("首点 R = %d", r)
	}
}

func collectContentPoints(n *tileNode, out *[]int32) {
	*out = append(*out, n.content...)
	for _, c := range n.children {
		collectContentPoints(c, out)
	}
}

func TestOctreeADDNoDuplication(t *testing.T) {
	cloud := gridCloud(6)
	indices := make([]int32, len(cloud.Points))
	for i := range indices {
		indices[i] = int32(i)
	}
	root := buildTree(cloud.Points, indices, cloud.Min, cloud.Max, 20, false, 0)
	var all []int32
	collectContentPoints(root, &all)
	if len(all) != len(cloud.Points) {
		t.Fatalf("ADD 模式总点数 = %d，期望 %d", len(all), len(cloud.Points))
	}
}

func TestOctreeREPLACELeavesCoverAll(t *testing.T) {
	cloud := gridCloud(6)
	indices := make([]int32, len(cloud.Points))
	for i := range indices {
		indices[i] = int32(i)
	}
	root := buildTree(cloud.Points, indices, cloud.Min, cloud.Max, 20, true, 0)
	leafPoints := 0
	var walk func(n *tileNode)
	walk = func(n *tileNode) {
		if len(n.children) == 0 {
			leafPoints += len(n.content)
		}
		for _, c := range n.children {
			walk(c)
		}
	}
	walk(root)
	if leafPoints != len(cloud.Points) {
		t.Fatalf("REPLACE 叶节点点数 = %d，期望 %d", leafPoints, len(cloud.Points))
	}
}

func TestBuildTileset11(t *testing.T) {
	cloud := gridCloud(5)
	out := t.TempDir()
	origin := crs.ENUOrigin{Lon: 121.47, Lat: 31.23, H: 10, Explicit: true}
	stats, err := Build(cloud, Options{
		OutputDir: out, PointsPerTile: 20, Refine: "add", Version: "1.1", Origin: origin,
	})
	if err != nil {
		t.Fatal(err)
	}
	if stats.Tiles == 0 || stats.Points != len(cloud.Points) {
		t.Fatalf("stats 异常：%+v", stats)
	}

	raw, err := os.ReadFile(filepath.Join(out, "tileset.json"))
	if err != nil {
		t.Fatal(err)
	}
	var ts struct {
		Asset struct {
			Version string `json:"version"`
		} `json:"asset"`
		GeometricError float64 `json:"geometricError"`
		Root           struct {
			Transform      []float64 `json:"transform"`
			BoundingVolume struct {
				Box []float64 `json:"box"`
			} `json:"boundingVolume"`
			Refine         string  `json:"refine"`
			GeometricError float64 `json:"geometricError"`
			Content        struct {
				URI string `json:"uri"`
			} `json:"content"`
			Children []json.RawMessage `json:"children"`
		} `json:"root"`
	}
	if err := json.Unmarshal(raw, &ts); err != nil {
		t.Fatal(err)
	}
	if ts.Asset.Version != "1.1" {
		t.Fatalf("asset.version = %s", ts.Asset.Version)
	}
	if ts.Root.Refine != "ADD" {
		t.Fatalf("refine = %s", ts.Root.Refine)
	}
	if len(ts.Root.BoundingVolume.Box) != 12 {
		t.Fatalf("box = %v", ts.Root.BoundingVolume.Box)
	}
	if len(ts.Root.Transform) != 16 || ts.Root.Transform[15] != 1 {
		t.Fatalf("根变换异常：%v", ts.Root.Transform)
	}

	// 根变换 × (点 - 根中心) 应等于 ENU→ECEF(原点) × 点（齐次比较）
	m := ts.Root.Transform
	center := [3]float64{(cloud.Min[0] + cloud.Max[0]) / 2, (cloud.Min[1] + cloud.Max[1]) / 2, (cloud.Min[2] + cloud.Max[2]) / 2}
	p := cloud.Points[0]
	local := [3]float64{p.X - center[0], p.Y - center[1], p.Z - center[2]}
	var got [3]float64
	for r := 0; r < 3; r++ {
		got[r] = m[0*4+r]*local[0] + m[1*4+r]*local[1] + m[2*4+r]*local[2] + m[3*4+r]
	}
	enu := ENUToECEFSimple(origin.Lon, origin.Lat, origin.H)
	var want [3]float64
	for r := 0; r < 3; r++ {
		want[r] = enu[0*4+r]*p.X + enu[1*4+r]*p.Y + enu[2*4+r]*p.Z + enu[3*4+r]
	}
	for r := 0; r < 3; r++ {
		if math.Abs(got[r]-want[r]) > 1e-6 {
			t.Fatalf("根变换数学错误：got=%v want=%v", got, want)
		}
	}

	// 根瓦片内容存在且可读
	if _, err := os.Stat(filepath.Join(out, filepath.FromSlash(ts.Root.Content.URI))); err != nil {
		t.Fatalf("根瓦片文件不存在：%v", err)
	}
}

func TestBuildTileset10(t *testing.T) {
	cloud := gridCloud(4)
	out := t.TempDir()
	stats, err := Build(cloud, Options{OutputDir: out, PointsPerTile: 10, Version: "1.0"})
	if err != nil {
		t.Fatal(err)
	}
	if stats.Tiles == 0 {
		t.Fatal("无瓦片")
	}
	entries, _ := os.ReadDir(filepath.Join(out, "tiles"))
	for _, e := range entries {
		if filepath.Ext(e.Name()) != ".pnts" {
			t.Fatalf("1.0 模式应输出 .pnts：%s", e.Name())
		}
	}
	raw, _ := os.ReadFile(filepath.Join(out, "tileset.json"))
	if !bytes.Contains(raw, []byte(`"version": "1.0"`)) {
		t.Fatalf("asset.version 应为 1.0：%s", raw)
	}
}

func TestBuildWithPlanes(t *testing.T) {
	cloud := gridCloud(4)
	out := t.TempDir()
	// 一块 10×10 米、z=0.5 的地平面
	pl := planes.Plane{
		Normal:   [3]float64{0, 0, 1},
		Centroid: [3]float64{4, 4, 0.5},
		Color:    [3]uint8{200, 120, 60},
		Polygon: [][3]float64{
			{-1, -1, 0.5}, {9, -1, 0.5}, {9, 9, 0.5}, {-1, 9, 0.5},
		},
		Inliers: 10000,
	}
	stats, err := Build(cloud, Options{
		OutputDir: out, PointsPerTile: 10, Version: "1.1", Planes: []planes.Plane{pl},
	})
	if err != nil {
		t.Fatal(err)
	}
	if stats.Planes != 1 {
		t.Fatalf("平面瓦片数 = %d", stats.Planes)
	}

	raw, err := os.ReadFile(filepath.Join(out, "tileset.json"))
	if err != nil {
		t.Fatal(err)
	}
	var ts struct {
		Root struct {
			Refine   string `json:"refine"`
			Children []struct {
				Transform []float64 `json:"transform"`
				Content   *struct {
					URI string `json:"uri"`
				} `json:"content"`
			} `json:"children"`
		} `json:"root"`
	}
	if err := json.Unmarshal(raw, &ts); err != nil {
		t.Fatal(err)
	}
	if ts.Root.Refine != "ADD" || len(ts.Root.Children) < 2 {
		t.Fatalf("场景根应包裹点云树 + 平面瓦片：refine=%s children=%d", ts.Root.Refine, len(ts.Root.Children))
	}

	// 找到平面瓦片（最后一个子节点）并解析网格
	planeTile := ts.Root.Children[len(ts.Root.Children)-1]
	if planeTile.Content == nil || !strings.HasSuffix(planeTile.Content.URI, ".glb") {
		t.Fatalf("平面瓦片内容异常：%+v", planeTile.Content)
	}
	data, err := os.ReadFile(filepath.Join(out, filepath.FromSlash(planeTile.Content.URI)))
	if err != nil {
		t.Fatal(err)
	}
	if binary.LittleEndian.Uint32(data[0:4]) != glbMagic {
		t.Fatal("平面瓦片 GLB 魔数错误")
	}
	jsonLen := int(binary.LittleEndian.Uint32(data[12:16]))
	var gd struct {
		Meshes []struct {
			Primitives []struct {
				Mode    int `json:"mode"`
				Indices int `json:"indices"`
			} `json:"primitives"`
		} `json:"meshes"`
		Accessors []struct {
			Type  string `json:"type"`
			Count int    `json:"count"`
		} `json:"accessors"`
	}
	if err := json.Unmarshal(bytes.TrimRight(data[20:20+jsonLen], " "), &gd); err != nil {
		t.Fatal(err)
	}
	if gd.Meshes[0].Primitives[0].Mode != 4 {
		t.Fatal("平面瓦片应为 TRIANGLES")
	}
	if gd.Accessors[0].Type != "VEC3" || gd.Accessors[0].Count != 4 {
		t.Fatalf("POSITION 应为 4 顶点：%+v", gd.Accessors[0])
	}
	if gd.Accessors[2].Type != "SCALAR" || gd.Accessors[2].Count != 6 {
		t.Fatalf("四边形应三角化为 6 索引：%+v", gd.Accessors[2])
	}
}

func TestBuildInvalidParams(t *testing.T) {
	cloud := gridCloud(2)
	if _, err := Build(cloud, Options{OutputDir: t.TempDir(), Version: "2.0"}); err == nil {
		t.Fatal("version=2.0 应报错")
	}
	if _, err := Build(cloud, Options{OutputDir: t.TempDir(), Refine: "nope"}); err == nil {
		t.Fatal("refine=nope 应报错")
	}
}
