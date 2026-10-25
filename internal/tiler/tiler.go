package tiler

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"math"
	"os"
	"path/filepath"

	"github.com/LinuxSuRen/pointcloud-twin/internal/apperr"
	"github.com/LinuxSuRen/pointcloud-twin/internal/crs"
	"github.com/LinuxSuRen/pointcloud-twin/internal/geo"
	"github.com/LinuxSuRen/pointcloud-twin/internal/pcd"
)

// Options 控制 3D Tiles 生成。
type Options struct {
	OutputDir string
	// PointsPerTile 是单瓦片目标点数（近似值），默认 50000。
	PointsPerTile int
	// Refine 为 add 或 replace（大小写不敏感），默认 add。
	Refine string
	// Version 为 "1.1"（glb，默认）或 "1.0"（pnts）。
	Version string
	// Origin 是 ENU 原点；瓦片集根变换把局部 ENU 摆到该地理位置。
	Origin crs.ENUOrigin
	Log    *slog.Logger
}

// Stats 是瓦片集生成统计，供转换报告使用。
type Stats struct {
	Tiles              int     `json:"tiles"`
	Points             int     `json:"points"`
	RootGeometricError float64 `json:"rootGeometricError"`
	TreeDepth          int     `json:"treeDepth"`
}

const generator = "pointcloud-twin tiler"

// Build 生成完整 3D Tiles 目录（tileset.json + tiles/）。
func Build(cloud *pcd.Cloud, opt Options) (*Stats, error) {
	log := opt.Log
	if log == nil {
		log = slog.Default()
	}
	version := opt.Version
	if version == "" {
		version = "1.1"
	}
	if version != "1.1" && version != "1.0" {
		return nil, apperr.New(apperr.E6001, nil, "--tiles-version 仅支持 1.1 或 1.0，实际："+version)
	}
	refine := "add"
	if opt.Refine != "" {
		refine = opt.Refine
	}
	switch lower(refine) {
	case "add":
		refine = "ADD"
	case "replace":
		refine = "REPLACE"
	default:
		return nil, apperr.New(apperr.E6001, nil, "--refine 仅支持 add 或 replace，实际："+opt.Refine)
	}
	target := opt.PointsPerTile
	if target <= 0 {
		target = 50000
	}

	tilesDir := filepath.Join(opt.OutputDir, "tiles")
	if err := os.MkdirAll(tilesDir, 0o755); err != nil {
		return nil, apperr.New(apperr.E2002, err, opt.OutputDir)
	}

	points := cloud.Points
	indices := make([]int32, len(points))
	for i := range indices {
		indices[i] = int32(i)
	}
	log.Info("开始切分", "stage", "tile.octree", "points", len(points),
		"pointsPerTile", target, "refine", refine, "version", version)

	root := buildTree(points, indices, cloud.Min, cloud.Max, target, refine == "REPLACE", 0)
	b := &builder{
		points:  points,
		version: version,
		refine:  refine,
		outDir:  opt.OutputDir,
		log:     log,
	}
	rootTile, err := b.emit(root, root.center, true, opt.Origin, 1)
	if err != nil {
		return nil, err
	}

	ts := newTileset(version)
	ts.GeometricError = rootTile.GeometricError
	ts.Root = rootTile
	data, err := json.MarshalIndent(ts, "", "  ")
	if err != nil {
		return nil, apperr.New(apperr.E5001, err, "tileset.json 序列化失败")
	}
	if err := os.WriteFile(filepath.Join(opt.OutputDir, "tileset.json"), data, 0o644); err != nil {
		return nil, apperr.New(apperr.E5001, err, "tileset.json 写入失败")
	}

	stats := &Stats{
		Tiles:              b.tileCount,
		Points:             b.pointCount,
		RootGeometricError: rootTile.GeometricError,
		TreeDepth:          b.maxDepth,
	}
	log.Info("切分完成", "stage", "tile.octree",
		"tiles", stats.Tiles, "pointsWritten", stats.Points, "depth", stats.TreeDepth)
	return stats, nil
}

type builder struct {
	points     []pcd.Point
	version    string
	refine     string
	outDir     string
	log        *slog.Logger
	tileCount  int
	pointCount int
	maxDepth   int
}

// emit 递归写出瓦片并构造 tile JSON。parentCenter 为父瓦片中心（局部 ENU）；
// isRoot 表示这是根瓦片（transform = ENU→ECEF × 平移）。
func (b *builder) emit(node *tileNode, parentCenter [3]float64, isRoot bool, origin crs.ENUOrigin, depth int) (*tileJSON, error) {
	if depth > b.maxDepth {
		b.maxDepth = depth
	}
	var nodeCenter [3]float64
	for i := 0; i < 3; i++ {
		nodeCenter[i] = (node.bboxMin[i] + node.bboxMax[i]) / 2
	}

	tile := &tileJSON{}
	if isRoot {
		tile.Transform = geo.Mat4Mul(geo.ENUToECEFMatrix(origin.Lon, origin.Lat, origin.H), geo.TranslateMatrix(nodeCenter[0], nodeCenter[1], nodeCenter[2]))
		tile.Refine = b.refine
	} else {
		tile.Transform = geo.TranslateMatrix(nodeCenter[0]-parentCenter[0], nodeCenter[1]-parentCenter[1], nodeCenter[2]-parentCenter[2])
	}

	// 包围盒（相对瓦片中心，z-up）
	var box [12]float64
	for i := 0; i < 3; i++ {
		box[3+i*3+i] = (node.bboxMax[i] - node.bboxMin[i]) / 2
	}
	tile.BoundingVolume.Box = &box

	// 几何误差：叶节点为 0，内部节点用子树包围盒对角线
	isLeaf := len(node.children) == 0
	if isLeaf {
		tile.GeometricError = 0
	} else {
		var diag float64
		for i := 0; i < 3; i++ {
			d := node.bboxMax[i] - node.bboxMin[i]
			diag += d * d
		}
		tile.GeometricError = math.Sqrt(diag)
	}

	// 内容
	if len(node.content) > 0 {
		positions := make([]float32, 0, len(node.content)*3)
		var colorsRGBA []uint8
		var colorsRGB []uint8
		if b.version == "1.1" {
			colorsRGBA = make([]uint8, 0, len(node.content)*4)
		} else {
			colorsRGB = make([]uint8, 0, len(node.content)*3)
		}
		for _, idx := range node.content {
			p := b.points[idx]
			positions = append(positions,
				float32(p.X-nodeCenter[0]),
				float32(p.Y-nodeCenter[1]),
				float32(p.Z-nodeCenter[2]))
			r, g, bl := pointColor(p)
			if colorsRGBA != nil {
				colorsRGBA = append(colorsRGBA, r, g, bl, 255)
			} else {
				colorsRGB = append(colorsRGB, r, g, bl)
			}
		}

		name := fmt.Sprintf("tiles/t%04d", b.tileCount)
		var data []byte
		var err error
		if b.version == "1.1" {
			data, err = encodeGLB(positions, colorsRGBA)
			name += ".glb"
		} else {
			data, err = encodePnts(positions, colorsRGB)
			name += ".pnts"
		}
		if err != nil {
			return nil, apperr.New(apperr.E5001, err, "瓦片编码失败："+name)
		}
		if err := os.WriteFile(filepath.Join(b.outDir, filepath.ToSlash(name)), data, 0o644); err != nil {
			return nil, apperr.New(apperr.E5001, err, "瓦片写入失败："+name)
		}
		b.log.Debug("写出瓦片", "tile", name, "points", len(node.content), "depth", depth)
		tile.Content = &contentJSON{URI: name}
		b.tileCount++
		b.pointCount += len(node.content)
	}

	for _, child := range node.children {
		ct, err := b.emit(child, nodeCenter, false, origin, depth+1)
		if err != nil {
			return nil, err
		}
		tile.Children = append(tile.Children, ct)
	}
	return tile, nil
}

// pointColor 给没有颜色的点一个稳定可读的回退色。
func pointColor(p pcd.Point) (r, g, b uint8) {
	if p.HasRGB {
		return p.R, p.G, p.B
	}
	if p.HasIntensity {
		v := uint8(60 + float64(p.Intensity)/65535.0*180.0)
		return v, v, v
	}
	return 176, 176, 176
}

func lower(s string) string {
	out := []byte(s)
	for i := range out {
		if out[i] >= 'A' && out[i] <= 'Z' {
			out[i] += 'a' - 'A'
		}
	}
	return string(out)
}

func inf() float64 { return math.Inf(1) }
