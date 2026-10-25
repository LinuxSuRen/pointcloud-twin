// Package pcdfix 生成 PCD 测试夹具与示例数据（ASCII / binary 编码）。
// 供本仓库单元测试与示例数据生成使用。
package pcdfix

import (
	"bufio"
	"encoding/binary"
	"fmt"
	"io"
	"math"
	"os"

	"github.com/LinuxSuRen/pointcloud-twin/internal/pcd"
)

// WriteASCII 写出 DATA ascii 的 PCD 文件（x y z rgb intensity 字段）。
func WriteASCII(w io.Writer, points []pcd.Point) error {
	bw := bufio.NewWriter(w)
	fmt.Fprintln(bw, "# .PCD v0.7 - Point Cloud Data file format")
	fmt.Fprintln(bw, "VERSION 0.7")
	fmt.Fprintln(bw, "FIELDS x y z rgb intensity")
	fmt.Fprintln(bw, "SIZE 4 4 4 4 4")
	fmt.Fprintln(bw, "TYPE F F F F F")
	fmt.Fprintln(bw, "COUNT 1 1 1 1 1")
	fmt.Fprintf(bw, "WIDTH %d\n", len(points))
	fmt.Fprintln(bw, "HEIGHT 1")
	fmt.Fprintln(bw, "VIEWPOINT 0 0 0 1 0 0 0")
	fmt.Fprintf(bw, "POINTS %d\n", len(points))
	fmt.Fprintln(bw, "DATA ascii")
	for _, p := range points {
		var rgb float32
		if p.HasRGB {
			packed := uint32(p.R)<<16 | uint32(p.G)<<8 | uint32(p.B)
			rgb = math.Float32frombits(packed)
		}
		fmt.Fprintf(bw, "%g %g %g %g %d\n", p.X, p.Y, p.Z, rgb, p.Intensity)
	}
	return bw.Flush()
}

// WriteBinary 写出 DATA binary 的 PCD 文件（x y z rgb intensity 字段，float32）。
func WriteBinary(w io.Writer, points []pcd.Point) error {
	bw := bufio.NewWriter(w)
	fmt.Fprintln(bw, "# .PCD v0.7 - Point Cloud Data file format")
	fmt.Fprintln(bw, "VERSION 0.7")
	fmt.Fprintln(bw, "FIELDS x y z rgb intensity")
	fmt.Fprintln(bw, "SIZE 4 4 4 4 4")
	fmt.Fprintln(bw, "TYPE F F F F F")
	fmt.Fprintln(bw, "COUNT 1 1 1 1 1")
	fmt.Fprintf(bw, "WIDTH %d\n", len(points))
	fmt.Fprintln(bw, "HEIGHT 1")
	fmt.Fprintln(bw, "VIEWPOINT 0 0 0 1 0 0 0")
	fmt.Fprintf(bw, "POINTS %d\n", len(points))
	fmt.Fprintln(bw, "DATA binary")
	rec := make([]byte, 20)
	for _, p := range points {
		var packed uint32
		if p.HasRGB {
			packed = uint32(p.R)<<16 | uint32(p.G)<<8 | uint32(p.B)
		}
		binary.LittleEndian.PutUint32(rec[0:4], math.Float32bits(float32(p.X)))
		binary.LittleEndian.PutUint32(rec[4:8], math.Float32bits(float32(p.Y)))
		binary.LittleEndian.PutUint32(rec[8:12], math.Float32bits(float32(p.Z)))
		binary.LittleEndian.PutUint32(rec[12:16], packed)
		binary.LittleEndian.PutUint32(rec[16:20], uint32(p.Intensity))
		if _, err := bw.Write(rec); err != nil {
			return err
		}
	}
	return bw.Flush()
}

// WriteFileBinary 便捷封装：写 binary PCD 到路径。
func WriteFileBinary(path string, points []pcd.Point) error {
	f, err := os.Create(path)
	if err != nil {
		return err
	}
	defer f.Close()
	return WriteBinary(f, points)
}
