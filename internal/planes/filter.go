package planes

import (
	"math"

	"github.com/LinuxSuRen/pointcloud-twin/internal/pcd"
)

// FilterResidual 依据平面结果过滤点云：落在任一平面 dist 阈值内且在边界
// 多边形之中的点视为已被面片取代，不进入导出模型。
func FilterResidual(points []pcd.Point, ps []Plane, dist float64) []pcd.Point {
	if len(ps) == 0 || len(points) == 0 {
		return points
	}
	type proj struct {
		p     *Plane
		u, v  [3]float64
		poly2 [][2]float64
	}
	planes2 := make([]proj, 0, len(ps))
	for i := range ps {
		p := &ps[i]
		u, v := planeBasis(p.Normal)
		poly2 := make([][2]float64, len(p.Polygon))
		for k, q := range p.Polygon {
			rel := [3]float64{q[0] - p.Centroid[0], q[1] - p.Centroid[1], q[2] - p.Centroid[2]}
			poly2[k] = [2]float64{dot(rel, u), dot(rel, v)}
		}
		ensureCCW(poly2)
		planes2 = append(planes2, proj{p: p, u: u, v: v, poly2: poly2})
	}

	out := make([]pcd.Point, 0, len(points))
	for i := range points {
		pt := points[i]
		covered := false
		for _, pl := range planes2 {
			rel := [3]float64{pt.X - pl.p.Centroid[0], pt.Y - pl.p.Centroid[1], pt.Z - pl.p.Centroid[2]}
			if math.Abs(dot(pl.p.Normal, rel)) > dist {
				continue
			}
			q := [2]float64{dot(rel, pl.u), dot(rel, pl.v)}
			if pointInPolygon(q, pl.poly2) {
				covered = true
				break
			}
		}
		if !covered {
			out = append(out, pt)
		}
	}
	return out
}

// pointInPolygon 射线法判断 2D 点是否在多边形内（边界视为在内）。
func pointInPolygon(p [2]float64, poly [][2]float64) bool {
	in := false
	n := len(poly)
	for i, j := 0, n-1; i < n; j, i = i, i+1 {
		xi, yi := poly[i][0], poly[i][1]
		xj, yj := poly[j][0], poly[j][1]
		if (yi > p[1]) != (yj > p[1]) {
			if p[0] < (xj-xi)*(p[1]-yi)/(yj-yi)+xi {
				in = !in
			}
		}
	}
	return in
}
