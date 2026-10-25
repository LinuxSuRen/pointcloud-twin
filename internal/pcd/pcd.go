// Package pcd 实现按 PCL 点云（PCD）文件规范读取点云数据。
//
// 支持 DATA 为 ascii、binary、binary_compressed 三种编码；
// 至少解析 x/y/z 坐标，可选解析 intensity 与 rgb/rgba（或分离的 r/g/b）字段。
// 规范参考：https://pointclouds.org/documentation/tutorials/pcd_file_format.html
package pcd

import (
	"fmt"
	"math"

	"github.com/LinuxSuRen/pointcloud-twin/internal/apperr"
)

// Point 是源坐标系下的单个点。颜色与强度为可选信息。
type Point struct {
	X, Y, Z float64
	// Intensity 是反射强度，归一化到 0~65535。
	Intensity uint16
	// R/G/B 是颜色分量，HasRGB 为 true 时有效。
	R, G, B      uint8
	HasRGB       bool
	HasIntensity bool
}

// Header 是 PCD 文件头解析结果。
type Header struct {
	Version string
	Fields  []string
	Types   []byte // 'F' | 'U' | 'I'
	Sizes   []int
	Counts  []int
	Width   int
	Height  int
	Points  int
	Data    string // ascii | binary | binary_compressed

	// 派生信息
	fieldIndex map[string]int
	pointSize  int // 单个点记录的字节数（binary 用）
	dataOffset int // header 结束位置（字节）
}

// HasField 返回是否包含指定字段。
func (h *Header) HasField(name string) bool {
	_, ok := h.fieldIndex[name]
	return ok
}

// Cloud 是加载后的完整点云（首版按设计限定千万点级，全量驻留内存）。
type Cloud struct {
	Header Header
	Points []Point
	// Min/Max 是有效点的包围盒（NaN 点已剔除）。
	Min [3]float64
	Max [3]float64
	// Skipped 是被剔除的无效点数量（NaN 坐标等）。
	Skipped int
}

// Load 从文件读取 PCD 点云。
func Load(path string) (*Cloud, error) {
	r, err := openReader(path)
	if err != nil {
		return nil, err
	}
	defer r.close()

	if err := r.parseHeader(); err != nil {
		return nil, err
	}
	h := &r.header
	for _, axis := range []string{"x", "y", "z"} {
		if !h.HasField(axis) {
			return nil, apperr.New(apperr.E1003, nil,
				fmt.Sprintf("缺少字段 %s，实际字段：%v", axis, h.Fields))
		}
	}
	if h.Points <= 0 {
		return nil, apperr.New(apperr.E1004, nil, "POINTS 为 0")
	}

	var (
		cloud *Cloud
		perr  error
	)
	switch h.Data {
	case "ascii":
		cloud, perr = r.readASCII()
	case "binary":
		cloud, perr = r.readBinary()
	case "binary_compressed":
		cloud, perr = r.readBinaryCompressed()
	default:
		perr = apperr.New(apperr.E1002, nil, "DATA 编码不支持："+h.Data)
	}
	if perr != nil {
		return nil, perr
	}

	cloud.Header = *h
	if len(cloud.Points) == 0 {
		return nil, apperr.New(apperr.E1004, nil, "没有有效点")
	}
	cloud.computeBounds()
	return cloud, nil
}

func (c *Cloud) computeBounds() {
	min, max := [3]float64{math.Inf(1), math.Inf(1), math.Inf(1)}, [3]float64{math.Inf(-1), math.Inf(-1), math.Inf(-1)}
	for _, p := range c.Points {
		v := [3]float64{p.X, p.Y, p.Z}
		for i := 0; i < 3; i++ {
			if v[i] < min[i] {
				min[i] = v[i]
			}
			if v[i] > max[i] {
				max[i] = v[i]
			}
		}
	}
	c.Min, c.Max = min, max
}

// RecomputeBounds 在修改 Points（如降采样）后重算包围盒。
func (c *Cloud) RecomputeBounds() { c.computeBounds() }
