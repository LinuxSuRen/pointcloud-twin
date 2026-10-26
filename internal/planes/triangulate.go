package planes

// EarClip 对逆时针简单多边形做耳切三角化，返回索引三元组。
// 顶点数在简化后通常为几十，O(n³) 最坏复杂度可接受。
func EarClip(poly [][2]float64) [][3]int {
	n := len(poly)
	if n < 3 {
		return nil
	}
	idx := make([]int, 0, n)
	for i := 0; i < n; i++ {
		idx = append(idx, i)
	}
	var tris [][3]int
	for len(idx) > 3 {
		ear := -1
		for i := 0; i < len(idx); i++ {
			ip := idx[(i+len(idx)-1)%len(idx)]
			ic := idx[i]
			in := idx[(i+1)%len(idx)]
			a, b, c := poly[ip], poly[ic], poly[in]
			if cross2(a, b, c) <= 0 {
				continue // 凹顶点
			}
			contained := false
			for _, j := range idx {
				if j == ip || j == ic || j == in {
					continue // 排除三角形自身顶点
				}
				if pointInTriangle(poly[j], a, b, c) {
					contained = true
					break
				}
			}
			if !contained {
				ear = i
				break
			}
		}
		if ear < 0 {
			// 数值退化：直接扇形三角化兜底
			for i := 1; i+1 < len(idx); i++ {
				tris = append(tris, [3]int{idx[0], idx[i], idx[i+1]})
			}
			return tris
		}
		prev := idx[(ear+len(idx)-1)%len(idx)]
		next := idx[(ear+1)%len(idx)]
		tris = append(tris, [3]int{prev, idx[ear], next})
		idx = append(idx[:ear], idx[ear+1:]...)
	}
	tris = append(tris, [3]int{idx[0], idx[1], idx[2]})
	return tris
}

func cross2(a, b, c [2]float64) float64 {
	return (b[0]-a[0])*(c[1]-a[1]) - (b[1]-a[1])*(c[0]-a[0])
}

// pointInTriangle 边界点视为在三角形内。
func pointInTriangle(p, a, b, c [2]float64) bool {
	d1 := cross2(a, b, p)
	d2 := cross2(b, c, p)
	d3 := cross2(c, a, p)
	hasNeg := d1 < 0 || d2 < 0 || d3 < 0
	hasPos := d1 > 0 || d2 > 0 || d3 > 0
	return !(hasNeg && hasPos)
}

// Triangles3D 把位于某平面上的 3D 多边形三角化（内部投影回 2D 处理）。
func Triangles3D(normal [3]float64, poly [][3]float64) [][3][3]float64 {
	idx := TrianglesIndexed(normal, poly)
	out := make([][3][3]float64, 0, len(idx))
	for _, t := range idx {
		out = append(out, [3][3]float64{poly[t[0]], poly[t[1]], poly[t[2]]})
	}
	return out
}

// TrianglesIndexed 把位于某平面上的 3D 多边形三角化，返回多边形顶点索引三元组。
func TrianglesIndexed(normal [3]float64, poly [][3]float64) [][3]int {
	if len(poly) < 3 {
		return nil
	}
	var centroid [3]float64
	for _, p := range poly {
		centroid[0] += p[0]
		centroid[1] += p[1]
		centroid[2] += p[2]
	}
	n := float64(len(poly))
	centroid = [3]float64{centroid[0] / n, centroid[1] / n, centroid[2] / n}
	u, v := planeBasis(normal)
	poly2 := make([][2]float64, len(poly))
	for i, p := range poly {
		rel := [3]float64{p[0] - centroid[0], p[1] - centroid[1], p[2] - centroid[2]}
		poly2[i] = [2]float64{dot(rel, u), dot(rel, v)}
	}
	ensureCCW(poly2)
	return EarClip(poly2)
}
