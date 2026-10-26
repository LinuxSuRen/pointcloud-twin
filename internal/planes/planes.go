// Package planes 实现点云的平面拟合分割：迭代 RANSAC 抽取大平面，
// 用栅格占据 + 连通域 + 轮廓追踪提取边界多边形，耳切法三角化为面片。
// 目标场景是站区类人造环境（地面/集装箱面/墙面），输出的面片网格
// 与残余点云叠加，构成「实体面 + 点云」的混合底图。
package planes

import (
	"math"

	"github.com/LinuxSuRen/pointcloud-twin/internal/pcd"
)

// Plane 是检测出的一个平面连通片（局部 ENU 坐标，米）。
type Plane struct {
	// Normal 是单位法向（朝向由首个非零分量取正约定，配合双面材质渲染）。
	Normal [3]float64 `json:"normal"`
	// Centroid 是内点质心（已投影到平面上）。
	Centroid [3]float64 `json:"centroid"`
	// Color 是内点平均颜色。
	Color [3]uint8 `json:"color"`
	// Polygon 是平面外边界多边形（3D，位于平面上，逆时针）。
	Polygon [][3]float64 `json:"polygon"`
	// Inliers 是该平面的内点数。
	Inliers int `json:"inliers"`
}

// Triangles 把边界多边形三角化为三角形顶点组。
func (p Plane) Triangles() [][3][3]float64 {
	return Triangles3D(p.Normal, p.Polygon)
}

// Options 控制平面检测。
type Options struct {
	// Dist 是内点距离阈值（米），默认 0.03。
	Dist float64
	// MinPoints 是单个平面的最小内点数，默认 500。
	MinPoints int
	// MaxPlanes 是最多抽取平面数，0 表示不限。
	MaxPlanes int
	// Grid 是边界占据栅格分辨率（米），默认 0.1。
	Grid float64
	// Simplify 是边界多边形 DP 简化容差（米），默认 0.2。
	Simplify float64
	// Trials 是每个平面的 RANSAC 采样次数，默认 128。
	Trials int
	// Seed 是随机种子（固定种子保证结果可复现），默认 1。
	Seed int64
}

func (o *Options) fillDefaults() {
	if o.Dist <= 0 {
		o.Dist = 0.03
	}
	if o.MinPoints <= 0 {
		o.MinPoints = 500
	}
	if o.Grid <= 0 {
		o.Grid = 0.1
	}
	if o.Simplify <= 0 {
		o.Simplify = 0.2
	}
	if o.Trials <= 0 {
		o.Trials = 128
	}
	if o.Seed == 0 {
		o.Seed = 1
	}
}

// Detect 执行平面检测：返回平面列表与未归入任何平面的残余点。
// points 不会被修改。
func Detect(points []pcd.Point, opt Options) (found []Plane, residual []pcd.Point) {
	opt.fillDefaults()
	rng := newRng(opt.Seed)

	// 边界栅格自适应：有效分辨率不低于约 2 倍中位近邻间距，
	// 避免稀疏采样（间距 > 栅格）导致占据格互不相邻、连通域碎片化。
	opt.Grid = adaptGrid(points, opt, rng)

	remaining := make([]int, len(points))
	for i := range remaining {
		remaining[i] = i
	}

	for opt.MaxPlanes <= 0 || len(found) < opt.MaxPlanes {
		if len(remaining) < opt.MinPoints {
			break
		}
		normal, d, inliers := ransacPlane(points, remaining, opt, rng)
		if len(inliers) < opt.MinPoints {
			break
		}
		comps := extractPlanes(points, inliers, normal, d, opt)
		if len(comps) == 0 {
			// 几何上无法提取边界（如共线内点），放弃这批内点，避免死循环
			break
		}
		found = append(found, comps...)

		// 从剩余集合中移除已归入平面的内点
		consumed := make(map[int]struct{}, len(inliers))
		for _, idx := range inliers {
			consumed[idx] = struct{}{}
		}
		next := remaining[:0:0]
		for _, idx := range remaining {
			if _, ok := consumed[idx]; !ok {
				next = append(next, idx)
			}
		}
		remaining = next
	}

	residual = make([]pcd.Point, 0, len(remaining))
	for _, idx := range remaining {
		residual = append(residual, points[idx])
	}
	return found, residual
}

// Coverage 返回内点数占比（0~1）。
func Coverage(total int, ps []Plane) float64 {
	if total <= 0 {
		return 0
	}
	inliers := 0
	for i := range ps {
		inliers += ps[i].Inliers
	}
	return float64(inliers) / float64(total)
}

// CoveragePercent 返回内点占比百分数（0~100，保留两位；边缘共享点可能
// 被多个平面计入，超过 100 时封顶）。
func CoveragePercent(total int, ps []Plane) float64 {
	p := math.Round(Coverage(total, ps)*10000) / 100
	if p > 100 {
		p = 100
	}
	return p
}
