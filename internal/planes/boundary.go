package planes

import "math"

// boundaryPolygons 由平面局部 2D 点提取各连通域的外边界多边形（简化后、逆时针）。
// 步骤：占据栅格 → 连通域（4 邻接）→ 裂缝跟踪（沿占据格边缘走）→ DP 简化。
func boundaryPolygons(pts [][2]float64, grid, tol float64) [][][2]float64 {
	if len(pts) == 0 {
		return nil
	}
	cells := make(map[[2]int]struct{}, len(pts))
	for _, p := range pts {
		cells[[2]int{int(math.Floor(p[0] / grid)), int(math.Floor(p[1] / grid))}] = struct{}{}
	}

	var polys [][][2]float64
	visited := make(map[[2]int]struct{}, len(cells))
	for start := range cells {
		if _, ok := visited[start]; ok {
			continue
		}
		// 连通域洪泛（4 邻接）
		component := map[[2]int]struct{}{start: {}}
		queue := [][2]int{start}
		visited[start] = struct{}{}
		for len(queue) > 0 {
			cur := queue[len(queue)-1]
			queue = queue[:len(queue)-1]
			for _, d := range [4][2]int{{1, 0}, {-1, 0}, {0, 1}, {0, -1}} {
				nb := [2]int{cur[0] + d[0], cur[1] + d[1]}
				if _, occ := cells[nb]; !occ {
					continue
				}
				if _, seen := visited[nb]; seen {
					continue
				}
				visited[nb] = struct{}{}
				component[nb] = struct{}{}
				queue = append(queue, nb)
			}
		}
		// 过小的连通域（碎屑）跳过
		if len(component) < 4 {
			continue
		}
		ring := traceContour(component)
		if len(ring) < 4 {
			continue
		}
		poly := make([][2]float64, len(ring))
		for i, c := range ring {
			poly[i] = [2]float64{float64(c[0]) * grid, float64(c[1]) * grid}
		}
		poly = simplifyRing(poly, tol)
		if len(poly) < 3 {
			continue
		}
		ensureCCW(poly)
		polys = append(polys, poly)
	}
	return polys
}

// 4 方向（顺时针序）：E、S、W、N。坐标系 u 右、v 上。
var crackDirs = [4][2]int{{1, 0}, {0, -1}, {-1, 0}, {0, 1}}

// frontCells 返回从角点 (x,y) 朝方向 d 前方相邻角点的两个格：
// FL 是前方左格，FR 是前方右格（决定左转/直行/右转）。
func frontCells(x, y, d int) (fl, fr [2]int) {
	switch d {
	case 0: // E：前方列 x，左=北、右=南
		return [2]int{x, y}, [2]int{x, y - 1}
	case 1: // S：前行 y-1，左=东、右=西
		return [2]int{x, y - 1}, [2]int{x - 1, y - 1}
	case 2: // W：前方列 x-1，左=南、右=北
		return [2]int{x - 1, y - 1}, [2]int{x - 1, y}
	default: // N：前行 y，左=西、右=东
		return [2]int{x - 1, y}, [2]int{x, y}
	}
}

// traceContour 用裂缝跟踪提取连通域外边界：沿「占据格在左、空格在右」的
// 边缘行走。决策规则：前方左格空 → 左转；前方右格空 → 直行；否则右转。
// 返回角点环（格坐标，不含闭合重复点），外边界为逆时针。
func traceContour(component map[[2]int]struct{}) [][2]int {
	// 起点：最小 v（再最小 u）的占据格——从其西南角向东出发
	var startCell [2]int
	first := true
	for c := range component {
		if first || c[1] < startCell[1] || (c[1] == startCell[1] && c[0] < startCell[0]) {
			startCell = c
			first = false
		}
	}
	sx, sy := startCell[0], startCell[1]
	const startDir = 0 // E

	occupied := func(c [2]int) bool {
		_, ok := component[c]
		return ok
	}

	x, y, d := sx, sy, startDir
	ring := [][2]int{{x, y}}
	limit := 4*len(component) + 8
	for step := 0; step < limit; step++ {
		fl, fr := frontCells(x, y, d)
		next := d
		if !occupied(fl) {
			next = (d + 3) % 4 // 左转
		} else if !occupied(fr) {
			// 直行
		} else {
			next = (d + 1) % 4 // 右转
		}
		// 回到起点且下一步将重复初始走向 → 闭环
		if x == sx && y == sy && next == startDir && len(ring) >= 4 {
			break
		}
		d = next
		x += crackDirs[d][0]
		y += crackDirs[d][1]
		ring = append(ring, [2]int{x, y})
	}
	// 去掉末尾与起点重复的角点
	if len(ring) > 1 && ring[len(ring)-1][0] == sx && ring[len(ring)-1][1] == sy {
		ring = ring[:len(ring)-1]
	}
	return ring
}

// simplifyRing 对闭合环做 Douglas-Peucker 简化：以首点为锚拆成两段开路径。
func simplifyRing(ring [][2]float64, tol float64) [][2]float64 {
	if len(ring) <= 4 || tol <= 0 {
		return ring
	}
	// 找离首点最远的点作为第二锚点
	far := 1
	maxD := -1.0
	for i := 1; i < len(ring); i++ {
		if d := dist2(ring[i], ring[0]); d > maxD {
			maxD = d
			far = i
		}
	}
	part1 := rdp(ring[:far+1], tol)
	part2 := rdp(append(append([][2]float64{}, ring[far:]...), ring[0]), tol)
	out := append(part1[:len(part1)-1], part2[:len(part2)-1]...)
	if len(out) < 3 {
		return ring
	}
	return out
}

func rdp(pts [][2]float64, tol float64) [][2]float64 {
	if len(pts) <= 2 {
		return pts
	}
	bestIdx, bestDist := 0, -1.0
	for i := 1; i < len(pts)-1; i++ {
		d := pointSegDist(pts[i], pts[0], pts[len(pts)-1])
		if d > bestDist {
			bestDist = d
			bestIdx = i
		}
	}
	if bestDist <= tol {
		return [][2]float64{pts[0], pts[len(pts)-1]}
	}
	left := rdp(pts[:bestIdx+1], tol)
	right := rdp(pts[bestIdx:], tol)
	return append(left[:len(left)-1], right...)
}

func pointSegDist(p, a, b [2]float64) float64 {
	ab := [2]float64{b[0] - a[0], b[1] - a[1]}
	ap := [2]float64{p[0] - a[0], p[1] - a[1]}
	ab2 := ab[0]*ab[0] + ab[1]*ab[1]
	if ab2 < 1e-12 {
		return math.Sqrt(ap[0]*ap[0] + ap[1]*ap[1])
	}
	t := (ap[0]*ab[0] + ap[1]*ab[1]) / ab2
	if t < 0 {
		t = 0
	} else if t > 1 {
		t = 1
	}
	q := [2]float64{a[0] + ab[0]*t, a[1] + ab[1]*t}
	return math.Sqrt(dist2(p, q))
}

func dist2(a, b [2]float64) float64 {
	dx, dy := a[0]-b[0], a[1]-b[1]
	return dx*dx + dy*dy
}

// signedArea2 返回两倍有向面积（>0 为逆时针）。
func signedArea2(poly [][2]float64) float64 {
	s := 0.0
	for i := 0; i < len(poly); i++ {
		j := (i + 1) % len(poly)
		s += poly[i][0]*poly[j][1] - poly[j][0]*poly[i][1]
	}
	return s
}

func ensureCCW(poly [][2]float64) {
	if signedArea2(poly) < 0 {
		for i, j := 0, len(poly)-1; i < j; i, j = i+1, j-1 {
			poly[i], poly[j] = poly[j], poly[i]
		}
	}
}
