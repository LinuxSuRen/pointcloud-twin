package planes

import (
	"math"
	"math/rand"
	"sort"

	"github.com/LinuxSuRen/pointcloud-twin/internal/pcd"
)

// rng 封装确定性随机源。
type rng struct{ r *rand.Rand }

func newRng(seed int64) *rng { return &rng{r: rand.New(rand.NewSource(seed))} }

// evalCap 是单次试验计票时的抽样上限（大点云先粗评再精算）。
const evalCap = 100_000

// neighborCell 是邻域采样的空间哈希格边长（米）。
const neighborCell = 1.0

// spatialHash 把剩余点按三维格分桶，用于邻域采样。
type spatialHash struct {
	cell float64
	bins map[[3]int][]int
}

func buildHash(points []pcd.Point, remaining []int, cell float64) *spatialHash {
	h := &spatialHash{cell: cell, bins: make(map[[3]int][]int, len(remaining))}
	for _, idx := range remaining {
		c := [3]int{
			int(math.Floor(points[idx].X / cell)),
			int(math.Floor(points[idx].Y / cell)),
			int(math.Floor(points[idx].Z / cell)),
		}
		h.bins[c] = append(h.bins[c], idx)
	}
	return h
}

// neighborsOf 返回 idx 所在格及其 26 邻格中的点索引。
func (h *spatialHash) neighborsOf(points []pcd.Point, idx int) []int {
	cx := int(math.Floor(points[idx].X / h.cell))
	cy := int(math.Floor(points[idx].Y / h.cell))
	cz := int(math.Floor(points[idx].Z / h.cell))
	var out []int
	for dx := -1; dx <= 1; dx++ {
		for dy := -1; dy <= 1; dy++ {
			for dz := -1; dz <= 1; dz++ {
				out = append(out, h.bins[[3]int{cx + dx, cy + dy, cz + dz}]...)
			}
		}
	}
	return out
}

// adaptGrid 依据采样密度自适应边界栅格：取 ≥2.2 倍中位近邻间距与用户值的较大者。
func adaptGrid(points []pcd.Point, opt Options, rng *rng) float64 {
	if len(points) < 4 {
		return opt.Grid
	}
	all := make([]int, len(points))
	for i := range all {
		all[i] = i
	}
	h := buildHash(points, all, neighborCell)
	n := 500
	if len(points) < n {
		n = len(points)
	}
	var dists []float64
	for i := 0; i < n; i++ {
		idx := rng.r.Intn(len(points))
		nb := h.neighborsOf(points, idx)
		best := math.Inf(1)
		for _, j := range nb {
			if j == idx {
				continue
			}
			dx := points[idx].X - points[j].X
			dy := points[idx].Y - points[j].Y
			dz := points[idx].Z - points[j].Z
			if d := math.Sqrt(dx*dx + dy*dy + dz*dz); d > 1e-9 && d < best {
				best = d
			}
		}
		if !math.IsInf(best, 1) {
			dists = append(dists, best)
		}
	}
	if len(dists) == 0 {
		return opt.Grid
	}
	sort.Float64s(dists)
	median := dists[len(dists)/2]
	g := 2.2 * median
	if g < opt.Grid {
		g = opt.Grid
	}
	return g
}

// ransacPlane 在 remaining 索引集合上找一个平面，返回法向、d 与内点索引。
// 采样策略：第一点随机，第二三点从其空间邻域取（大幅提升命中共面三点概率）。
func ransacPlane(points []pcd.Point, remaining []int, opt Options, rng *rng) ([3]float64, float64, []int) {
	// 计票子集：大点云抽 evalCap 个，小点云全量
	eval := remaining
	if len(remaining) > evalCap {
		eval = make([]int, evalCap)
		for i := range eval {
			eval[i] = remaining[rng.r.Intn(len(remaining))]
		}
	}

	hash := buildHash(points, remaining, neighborCell)

	bestN, bestD, bestCount := [3]float64{0, 0, 1}, 0.0, -1
	for t := 0; t < opt.Trials; t++ {
		aIdx := remaining[rng.r.Intn(len(remaining))]
		nb := hash.neighborsOf(points, aIdx)
		if len(nb) < 3 {
			continue
		}
		a := points[aIdx]
		b := points[nb[rng.r.Intn(len(nb))]]
		c := points[nb[rng.r.Intn(len(nb))]]

		ab := [3]float64{b.X - a.X, b.Y - a.Y, b.Z - a.Z}
		ac := [3]float64{c.X - a.X, c.Y - a.Y, c.Z - a.Z}
		n := cross(ab, ac)
		l := norm(n)
		if l < 1e-9 {
			continue // 三点共线
		}
		n = [3]float64{n[0] / l, n[1] / l, n[2] / l}
		// 法向符号归一：最大分量取正（确定性 + 便于消费者约定）
		if ai, _, _ := maxAbsAxis(n); n[ai] < 0 {
			n = [3]float64{-n[0], -n[1], -n[2]}
		}
		d := -dot(n, [3]float64{a.X, a.Y, a.Z})

		count := 0
		for _, idx := range eval {
			p := points[idx]
			if math.Abs(dot(n, [3]float64{p.X, p.Y, p.Z})+d) <= opt.Dist {
				count++
			}
		}
		if count > bestCount {
			bestN, bestD, bestCount = n, d, count
		}
	}
	if bestCount < 0 {
		return [3]float64{0, 1, 0}, 0, nil
	}

	// 精算：全量内点
	var inliers []int
	for _, idx := range remaining {
		p := points[idx]
		if math.Abs(dot(bestN, [3]float64{p.X, p.Y, p.Z})+bestD) <= opt.Dist {
			inliers = append(inliers, idx)
		}
	}
	return bestN, bestD, inliers
}

// extractPlanes 把一个平面的内点按占据栅格连通域拆成多个 Plane（每个连通域一片）。
func extractPlanes(points []pcd.Point, inliers []int, normal [3]float64, d float64, opt Options) []Plane {
	// 平面内正交基
	u, v := planeBasis(normal)

	// 质心
	var cx, cy, cz float64
	var cr, cg, cb float64
	hasColor := false
	for _, idx := range inliers {
		p := points[idx]
		cx += p.X
		cy += p.Y
		cz += p.Z
		if p.HasRGB {
			cr += float64(p.R)
			cg += float64(p.G)
			cb += float64(p.B)
			hasColor = true
		}
	}
	n := float64(len(inliers))
	centroid := [3]float64{cx / n, cy / n, cz / n}
	// 投影到平面
	centroid = projectToPlane(centroid, normal, d)
	color := [3]uint8{176, 176, 176}
	if hasColor {
		color = [3]uint8{uint8(cr / n), uint8(cg / n), uint8(cb / n)}
	}

	// 内点投影到平面局部 2D
	pts2 := make([][2]float64, len(inliers))
	for i, idx := range inliers {
		p := points[idx]
		rel := [3]float64{p.X - centroid[0], p.Y - centroid[1], p.Z - centroid[2]}
		pts2[i] = [2]float64{dot(rel, u), dot(rel, v)}
	}

	var out []Plane
	polys := boundaryPolygons(pts2, opt.Grid, opt.Simplify)
	if len(polys) == 0 {
		return nil
	}
	// 计算各连通片面积，剔除共享边缘点产生的细条碎片（< 最大片面积的 10%）
	areas := make([]float64, len(polys))
	maxArea := 0.0
	for i, poly := range polys {
		for _, tri := range EarClip(poly) {
			areas[i] += triArea2Of(poly, tri)
		}
		if areas[i] > maxArea {
			maxArea = areas[i]
		}
	}
	for i, poly := range polys {
		if len(poly) < 3 || areas[i] < 0.1*maxArea {
			continue
		}
		// 2D 边界映射回 3D
		poly3 := make([][3]float64, len(poly))
		for k, q := range poly {
			poly3[k] = [3]float64{
				centroid[0] + u[0]*q[0] + v[0]*q[1],
				centroid[1] + u[1]*q[0] + v[1]*q[1],
				centroid[2] + u[2]*q[0] + v[2]*q[1],
			}
		}
		out = append(out, Plane{
			Normal:   normal,
			Centroid: centroid,
			Color:    color,
			Polygon:  poly3,
			Inliers:  len(inliers), // 同一平面的各连通片共享内点统计
		})
	}
	return out
}

func triArea2Of(poly [][2]float64, tri [3]int) float64 {
	return math.Abs(cross2(poly[tri[0]], poly[tri[1]], poly[tri[2]])) / 2
}

// planeBasis 由法向构造平面内正交基（确定性：选与法向最不对齐的坐标轴）。
func planeBasis(n [3]float64) (u, v [3]float64) {
	axis := [3]float64{0, 0, 1}
	if math.Abs(n[2]) > math.Abs(n[0]) && math.Abs(n[2]) > math.Abs(n[1]) {
		axis = [3]float64{1, 0, 0}
	}
	u = normalize(cross(n, axis))
	v = cross(n, u)
	return u, v
}

func projectToPlane(p, n [3]float64, d float64) [3]float64 {
	t := dot(n, p) + d
	return [3]float64{p[0] - n[0]*t, p[1] - n[1]*t, p[2] - n[2]*t}
}

func cross(a, b [3]float64) [3]float64 {
	return [3]float64{
		a[1]*b[2] - a[2]*b[1],
		a[2]*b[0] - a[0]*b[2],
		a[0]*b[1] - a[1]*b[0],
	}
}

func dot(a, b [3]float64) float64 { return a[0]*b[0] + a[1]*b[1] + a[2]*b[2] }

func norm(a [3]float64) float64 { return math.Sqrt(dot(a, a)) }

// maxAbsAxis 返回绝对值最大的分量下标及其绝对值。
func maxAbsAxis(n [3]float64) (int, float64, float64) {
	idx, best := 0, math.Abs(n[0])
	if v := math.Abs(n[1]); v > best {
		idx, best = 1, v
	}
	if v := math.Abs(n[2]); v > best {
		idx, best = 2, v
	}
	return idx, best, math.Abs(n[idx])
}

func normalize(a [3]float64) [3]float64 {
	l := norm(a)
	if l < 1e-12 {
		return [3]float64{1, 0, 0}
	}
	return [3]float64{a[0] / l, a[1] / l, a[2] / l}
}
