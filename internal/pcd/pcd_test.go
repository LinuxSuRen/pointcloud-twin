package pcd_test

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"testing"

	"github.com/LinuxSuRen/pointcloud-twin/internal/apperr"
	"github.com/LinuxSuRen/pointcloud-twin/internal/pcd"
	"github.com/LinuxSuRen/pointcloud-twin/internal/pcdfix"
)

func samplePoints() []pcd.Point {
	return []pcd.Point{
		{X: 1.5, Y: -2.25, Z: 3.125, Intensity: 100, R: 255, G: 128, B: 0, HasRGB: true, HasIntensity: true},
		{X: -0.5, Y: 10, Z: 4, Intensity: 65535, R: 0, G: 255, B: 255, HasRGB: true, HasIntensity: true},
		{X: 100.25, Y: 200.5, Z: -50.75, Intensity: 0, R: 1, G: 2, B: 3, HasRGB: true, HasIntensity: true},
	}
}

func writeTemp(t *testing.T, name string, write func() []byte) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), name)
	if err := os.WriteFile(path, write(), 0o644); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestLoadASCII(t *testing.T) {
	var buf bytes.Buffer
	if err := pcdfix.WriteASCII(&buf, samplePoints()); err != nil {
		t.Fatal(err)
	}
	path := writeTemp(t, "a.pcd", func() []byte { return buf.Bytes() })

	cloud, err := pcd.Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(cloud.Points) != 3 {
		t.Fatalf("点数 = %d，期望 3", len(cloud.Points))
	}
	p := cloud.Points[0]
	if p.X != 1.5 || p.Y != -2.25 || p.Z != 3.125 {
		t.Fatalf("坐标解析错误：%v", p)
	}
	if !p.HasRGB || p.R != 255 || p.G != 128 || p.B != 0 {
		t.Fatalf("颜色解析错误：%v", p)
	}
	if !p.HasIntensity || p.Intensity != 100 {
		t.Fatalf("强度解析错误：%v", p)
	}
}

func TestLoadBinary(t *testing.T) {
	var buf bytes.Buffer
	if err := pcdfix.WriteBinary(&buf, samplePoints()); err != nil {
		t.Fatal(err)
	}
	path := writeTemp(t, "b.pcd", func() []byte { return buf.Bytes() })

	cloud, err := pcd.Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(cloud.Points) != 3 {
		t.Fatalf("点数 = %d", len(cloud.Points))
	}
	p := cloud.Points[0]
	if p.X != 1.5 || p.Y != -2.25 || p.Z != 3.125 {
		t.Fatalf("坐标解析错误：%v %v %v", p.X, p.Y, p.Z)
	}
	if p.R != 255 || p.G != 128 || p.B != 0 {
		t.Fatalf("颜色解析错误：%v", p)
	}
	if cloud.Header.Data != "binary" {
		t.Fatalf("编码 = %s", cloud.Header.Data)
	}
}

func TestLoadBinaryNaN(t *testing.T) {
	pts := append(samplePoints(), pcd.Point{X: math.NaN(), Y: 0, Z: 0})
	var buf bytes.Buffer
	if err := pcdfix.WriteBinary(&buf, pts); err != nil {
		t.Fatal(err)
	}
	path := writeTemp(t, "nan.pcd", func() []byte { return buf.Bytes() })
	cloud, err := pcd.Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(cloud.Points) != 3 || cloud.Skipped != 1 {
		t.Fatalf("有效点 = %d，剔除 = %d", len(cloud.Points), cloud.Skipped)
	}
}

// lzfLiteralOnly 用「纯字面量」方式构造合法 LZF 流（每组 ≤32 字节）。
func lzfLiteralOnly(data []byte) []byte {
	var out []byte
	for i := 0; i < len(data); i += 32 {
		end := i + 32
		if end > len(data) {
			end = len(data)
		}
		run := data[i:end]
		out = append(out, byte(len(run)-1))
		out = append(out, run...)
	}
	return out
}

func TestLoadBinaryCompressed(t *testing.T) {
	pts := samplePoints()
	// 构造 binary_compressed 文件：字段优先布局 + LZF（字面量编码）
	pointCount := len(pts)
	pointSize := 20
	uncompressed := make([]byte, pointSize*pointCount)
	// 字段 i 的偏移 = i * 4 * pointCount
	fields := []struct {
		off  int
		fill func(p pcd.Point) uint32
	}{
		{0, func(p pcd.Point) uint32 { return math.Float32bits(float32(p.X)) }},
		{4 * pointCount, func(p pcd.Point) uint32 { return math.Float32bits(float32(p.Y)) }},
		{8 * pointCount, func(p pcd.Point) uint32 { return math.Float32bits(float32(p.Z)) }},
		{12 * pointCount, func(p pcd.Point) uint32 {
			return uint32(p.R)<<16 | uint32(p.G)<<8 | uint32(p.B)
		}},
		{16 * pointCount, func(p pcd.Point) uint32 { return uint32(p.Intensity) }},
	}
	for i, p := range pts {
		for _, f := range fields {
			binary.LittleEndian.PutUint32(uncompressed[f.off+i*4:], f.fill(p))
		}
	}
	compressed := lzfLiteralOnly(uncompressed)

	var buf bytes.Buffer
	buf.WriteString("# .PCD v0.7\n")
	buf.WriteString("VERSION 0.7\n")
	buf.WriteString("FIELDS x y z rgb intensity\n")
	buf.WriteString("SIZE 4 4 4 4 4\n")
	buf.WriteString("TYPE F F F F F\n")
	buf.WriteString("COUNT 1 1 1 1 1\n")
	fmt.Fprintf(&buf, "WIDTH %d\n", pointCount)
	buf.WriteString("HEIGHT 1\n")
	buf.WriteString("VIEWPOINT 0 0 0 1 0 0 0\n")
	fmt.Fprintf(&buf, "POINTS %d\n", pointCount)
	buf.WriteString("DATA binary_compressed\n")
	var sz [8]byte
	binary.LittleEndian.PutUint32(sz[0:4], uint32(len(compressed)))
	binary.LittleEndian.PutUint32(sz[4:8], uint32(len(uncompressed)))
	buf.Write(sz[:])
	buf.Write(compressed)

	path := writeTemp(t, "c.pcd", func() []byte { return buf.Bytes() })
	cloud, err := pcd.Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(cloud.Points) != 3 {
		t.Fatalf("点数 = %d", len(cloud.Points))
	}
	p := cloud.Points[0]
	if p.X != 1.5 || p.Y != -2.25 || p.Z != 3.125 || p.R != 255 || p.G != 128 || p.B != 0 {
		t.Fatalf("解析错误：%v", p)
	}
}

func TestLoadErrors(t *testing.T) {
	// 不存在的文件
	if _, err := pcd.Load(filepath.Join(t.TempDir(), "nope.pcd")); err == nil {
		t.Fatal("期望错误")
	} else if code, ok := apperr.From(err); !ok || code != apperr.E1001 {
		t.Fatalf("错误码 = %v", code)
	}

	// 缺少 z 字段
	var buf bytes.Buffer
	buf.WriteString("VERSION 0.7\nFIELDS x y\nSIZE 4 4\nTYPE F F\nCOUNT 1 1\nWIDTH 1\nHEIGHT 1\nPOINTS 1\nDATA ascii\n1 2\n")
	path := writeTemp(t, "bad.pcd", func() []byte { return buf.Bytes() })
	if _, err := pcd.Load(path); err == nil {
		t.Fatal("期望错误")
	} else if code, ok := apperr.From(err); !ok || code != apperr.E1003 {
		t.Fatalf("错误码 = %v", code)
	}
}

func TestBounds(t *testing.T) {
	var buf bytes.Buffer
	if err := pcdfix.WriteASCII(&buf, samplePoints()); err != nil {
		t.Fatal(err)
	}
	path := writeTemp(t, "bnd.pcd", func() []byte { return buf.Bytes() })
	cloud, err := pcd.Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if cloud.Min[0] != -0.5 || cloud.Max[0] != 100.25 {
		t.Fatalf("包围盒错误：%v %v", cloud.Min, cloud.Max)
	}
}
