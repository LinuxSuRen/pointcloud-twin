package tiler

import (
	"context"
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
	"github.com/LinuxSuRen/pointcloud-twin/internal/planes"
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
	// Ctx 取消时在瓦片写出间隙终止，返回 ctx 的错误。
	Ctx context.Context
	// Progress 是可选回调：每写出一个瓦片上报 written/total。
	Progress func(written, total int)
	// Planes 是可选的平面分割结果：作为网格面片瓦片与点云树同层输出（仅 1.1）。
	Planes []planes.Plane
}

// Stats 是瓦片集生成统计，供转换报告使用。
type Stats struct {
	Tiles              int     `json:"tiles"`
	Points             int     `json:"points"`
	RootGeometricError float64 `json:"rootGeometricError"`
	TreeDepth          int     `json:"treeDepth"`
	Planes             int     `json:"planes"`
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

	// 平面面片瓦片（仅 3D Tiles 1.1 支持网格内容）
	meshPlanes := opt.Planes
	if version != "1.1" && len(meshPlanes) > 0 {
		log.Warn("平面分割结果仅在 --tiles-version 1.1 下生成瓦片，已跳过（planes.json 仍会输出）",
			"stage", "tile.planes", "planes", len(meshPlanes))
		meshPlanes = nil
	}
	if len(points) == 0 {
		if len(meshPlanes) == 0 {
			return nil, apperr.New(apperr.E1004, nil, "点云为空且无平面结果")
		}
		meshPlanes = keepValidPlanes(meshPlanes)
	}

	var root *tileNode
	totalTiles := len(meshPlanes)
	if len(points) > 0 {
		root = buildTree(points, indices, cloud.Min, cloud.Max, target, refine == "REPLACE", 0)
		totalTiles += countTiles(root)
	}
	b := &builder{
		points:  points,
		version: version,
		refine:  refine,
		outDir:  opt.OutputDir,
		log:     log,
		ctx:     opt.Ctx,
		onTile:  opt.Progress,
		total:   totalTiles,
	}

	var rootTile *tileJSON
	if len(meshPlanes) > 0 {
		// 场景根包裹：点云树（如有）+ 平面瓦片，统一挂到 ENU→ECEF 根变换下
		sceneMin, sceneMax := planesBounds(meshPlanes)
		if len(points) > 0 {
			sceneMin, sceneMax = unionBounds(cloud.Min, cloud.Max, meshPlanes)
		}
		sceneCenter := [3]float64{
			(sceneMin[0] + sceneMax[0]) / 2,
			(sceneMin[1] + sceneMax[1]) / 2,
			(sceneMin[2] + sceneMax[2]) / 2,
		}
		wrapper := &tileJSON{
			Transform: geo.Mat4Mul(geo.ENUToECEFMatrix(opt.Origin.Lon, opt.Origin.Lat, opt.Origin.H),
				geo.TranslateMatrix(sceneCenter[0], sceneCenter[1], sceneCenter[2])),
			Refine: b.refine,
		}
		var diag float64
		for i := 0; i < 3; i++ {
			d := sceneMax[i] - sceneMin[i]
			diag += d * d
		}
		wrapper.GeometricError = math.Sqrt(diag)
		var box [12]float64
		for i := 0; i < 3; i++ {
			box[3+i*3+i] = (sceneMax[i] - sceneMin[i]) / 2
		}
		wrapper.BoundingVolume.Box = &box
		if root != nil {
			pointRoot, err := b.emit(root, sceneCenter, false, opt.Origin, 1)
			if err != nil {
				return nil, err
			}
			wrapper.GeometricError = pointRoot.GeometricError
			wrapper.Children = append(wrapper.Children, pointRoot)
		}
		for i := range meshPlanes {
			pt, err := b.emitPlane(&meshPlanes[i], sceneCenter)
			if err != nil {
				return nil, err
			}
			if pt != nil {
				wrapper.Children = append(wrapper.Children, pt)
			}
		}
		rootTile = wrapper
	} else {
		var err error
		rootTile, err = b.emit(root, root.center, true, opt.Origin, 1)
		if err != nil {
			return nil, err
		}
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
		Planes:             len(meshPlanes),
	}
	log.Info("切分完成", "stage", "tile.octree",
		"tiles", stats.Tiles, "pointsWritten", stats.Points, "depth", stats.TreeDepth,
		"planes", stats.Planes)
	return stats, nil
}

// unionBounds 求点云包围盒与所有平面多边形包围盒的并。
func unionBounds(min, max [3]float64, ps []planes.Plane) ([3]float64, [3]float64) {
	for _, p := range ps {
		for _, v := range p.Polygon {
			for i := 0; i < 3; i++ {
				if v[i] < min[i] {
					min[i] = v[i]
				}
				if v[i] > max[i] {
					max[i] = v[i]
				}
			}
		}
	}
	return min, max
}

// planesBounds 计算平面集合自身的包围盒。
func planesBounds(ps []planes.Plane) ([3]float64, [3]float64) {
	min, max := ps[0].Polygon[0], ps[0].Polygon[0]
	for _, p := range ps {
		for _, v := range p.Polygon {
			for i := 0; i < 3; i++ {
				if v[i] < min[i] {
					min[i] = v[i]
				}
				if v[i] > max[i] {
					max[i] = v[i]
				}
			}
		}
	}
	return min, max
}

// keepValidPlanes 过滤无法构成面片的平面（多边形不足 3 点）。
func keepValidPlanes(ps []planes.Plane) []planes.Plane {
	out := ps[:0]
	for _, p := range ps {
		if len(p.Polygon) >= 3 {
			out = append(out, p)
		}
	}
	return out
}

// emitPlane 输出单个平面的面片瓦片。
func (b *builder) emitPlane(p *planes.Plane, sceneCenter [3]float64) (*tileJSON, error) {
	tris := planes.TrianglesIndexed(p.Normal, p.Polygon)
	if len(tris) == 0 || len(p.Polygon) < 3 {
		return nil, nil
	}
	var mn, mx [3]float64
	mn = p.Polygon[0]
	mx = p.Polygon[0]
	for _, v := range p.Polygon {
		for i := 0; i < 3; i++ {
			if v[i] < mn[i] {
				mn[i] = v[i]
			}
			if v[i] > mx[i] {
				mx[i] = v[i]
			}
		}
	}
	var center [3]float64
	for i := 0; i < 3; i++ {
		center[i] = (mn[i] + mx[i]) / 2
	}

	positions := make([]float32, 0, len(p.Polygon)*3)
	normals := make([]float32, 0, len(p.Polygon)*3)
	for _, v := range p.Polygon {
		positions = append(positions, float32(v[0]-center[0]), float32(v[1]-center[1]), float32(v[2]-center[2]))
		normals = append(normals, float32(p.Normal[0]), float32(p.Normal[1]), float32(p.Normal[2]))
	}
	indices := make([]uint32, 0, len(tris)*3)
	for _, t := range tris {
		indices = append(indices, uint32(t[0]), uint32(t[1]), uint32(t[2]))
	}

	name := fmt.Sprintf("tiles/t%04d.glb", b.tileCount)
	data, err := encodeMeshGLB(positions, normals, indices, p.Color[0], p.Color[1], p.Color[2])
	if err != nil {
		return nil, apperr.New(apperr.E5001, err, "平面瓦片编码失败："+name)
	}
	if err := os.WriteFile(filepath.Join(b.outDir, filepath.FromSlash(name)), data, 0o644); err != nil {
		return nil, apperr.New(apperr.E5001, err, "平面瓦片写入失败："+name)
	}
	b.log.Debug("写出平面瓦片", "tile", name, "inliers", p.Inliers, "vertices", len(p.Polygon))
	b.tileCount++
	if b.onTile != nil && b.total > 0 {
		b.onTile(b.tileCount, b.total)
	}

	tile := &tileJSON{
		Transform:      geo.TranslateMatrix(center[0]-sceneCenter[0], center[1]-sceneCenter[1], center[2]-sceneCenter[2]),
		GeometricError: 0,
	}
	var box [12]float64
	for i := 0; i < 3; i++ {
		box[3+i*3+i] = (mx[i] - mn[i]) / 2
	}
	tile.BoundingVolume.Box = &box
	tile.Content = &contentJSON{URI: name}
	return tile, nil
}

type builder struct {
	points     []pcd.Point
	version    string
	refine     string
	outDir     string
	log        *slog.Logger
	ctx        context.Context
	onTile     func(written, total int)
	tileCount  int
	pointCount int
	maxDepth   int
	total      int
}

// countTiles 统计携带内容的瓦片数，作为进度总量。
func countTiles(n *tileNode) int {
	c := 0
	if len(n.content) > 0 {
		c++
	}
	for _, ch := range n.children {
		c += countTiles(ch)
	}
	return c
}

// emit 递归写出瓦片并构造 tile JSON。parentCenter 为父瓦片中心（局部 ENU）；
// isRoot 表示这是根瓦片（transform = ENU→ECEF × 平移）。
func (b *builder) emit(node *tileNode, parentCenter [3]float64, isRoot bool, origin crs.ENUOrigin, depth int) (*tileJSON, error) {
	if b.ctx != nil {
		if err := b.ctx.Err(); err != nil {
			return nil, err
		}
	}
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
		if b.onTile != nil && b.total > 0 {
			b.onTile(b.tileCount, b.total)
		}
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
