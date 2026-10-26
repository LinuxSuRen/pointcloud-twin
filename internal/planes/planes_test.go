package planes

import (
	"math"
	"math/rand"
	"testing"

	"github.com/LinuxSuRen/pointcloud-twin/internal/pcd"
)

// groundWithBoxes 生成「地面 + 两个箱体表面」的合成点云（0.1m 采样）。
func groundWithBoxes() []pcd.Point {
	var pts []pcd.Point
	addPlane := func(n [3]float64, origin, du, dv [3]float64, nu, nv int, r, g, b uint8) {
		for i := 0; i <= nu; i++ {
			for j := 0; j <= nv; j++ {
				u := float64(i) / float64(nu)
				v := float64(j) / float64(nv)
				pts = append(pts, pcd.Point{
					X: origin[0] + du[0]*u + dv[0]*v,
					Y: origin[1] + du[1]*u + dv[1]*v,
					Z: origin[2] + du[2]*u + dv[2]*v,
					R: r, G: g, B: b, HasRGB: true,
				})
			}
		}
	}
	// 地面 20m × 10m（z=0）
	addPlane([3]float64{0, 0, 1}, [3]float64{0, 0, 0}, [3]float64{20, 0, 0}, [3]float64{0, 10, 0}, 200, 100, 90, 100, 80)
	// 箱体 A：顶面 z=3（5m × 3m）与两个侧面
	addPlane([3]float64{0, 0, 1}, [3]float64{2, 2, 3}, [3]float64{5, 0, 0}, [3]float64{0, 3, 0}, 50, 30, 60, 120, 200)
	addPlane([3]float64{0, 1, 0}, [3]float64{2, 2, 0}, [3]float64{5, 0, 0}, [3]float64{0, 0, 3}, 50, 30, 60, 120, 200)
	addPlane([3]float64{1, 0, 0}, [3]float64{2, 5, 0}, [3]float64{0, 0, 3}, [3]float64{0, -3, 0}, 30, 30, 60, 120, 200)
	// 箱体 B：顶面 z=2.5（4m × 2.5m）
	addPlane([3]float64{0, 0, 1}, [3]float64{10, 2, 2.5}, [3]float64{4, 0, 0}, [3]float64{0, 2.5, 0}, 40, 25, 200, 120, 60)
	return pts
}

func TestDetectGroundAndBoxes(t *testing.T) {
	pts := groundWithBoxes()
	found, residual := Detect(pts, Options{Dist: 0.02, MinPoints: 300, Grid: 0.2, Simplify: 0.3})
	if len(found) < 4 {
		t.Fatalf("应至少检测出地面 + 3~4 个箱体面，实际 %d 个", len(found))
	}
	if got := Coverage(len(pts), found); got < 0.85 {
		t.Fatalf("平面覆盖率过低：%.2f（残余 %d 点）", got, len(residual))
	}

	// 找地面（法向近 +Z、内点最多）
	var ground *Plane
	for i := range found {
		p := &found[i]
		if p.Normal[2] > 0.9 && (ground == nil || p.Inliers > ground.Inliers) {
			ground = p
		}
	}
	if ground == nil {
		t.Fatal("未检测出地面")
	}
	// 地面多边形面积应接近 20×10=200m²
	area := PolygonArea3D(*ground)
	if math.Abs(area-200) > 30 {
		t.Fatalf("地面面积 = %.1f，期望约 200", area)
	}
	// 三角化
	tris := ground.Triangles()
	if len(tris) < 1 {
		t.Fatal("地面三角化为空")
	}
	sum := 0.0
	for _, tri := range tris {
		sum += TriangleArea3D(tri)
	}
	if math.Abs(sum-area) > area*0.05 {
		t.Fatalf("三角化总面积 %.1f 与多边形面积 %.1f 偏差过大", sum, area)
	}
}

func TestDetectDeterministic(t *testing.T) {
	pts := groundWithBoxes()
	opt := Options{Dist: 0.02, MinPoints: 300, Grid: 0.2, Simplify: 0.3}
	a, ra := Detect(pts, opt)
	b, rb := Detect(pts, opt)
	if len(ra) != len(rb) || len(a) != len(b) {
		t.Fatalf("同种子结果不稳定：%d/%d 平面，残余 %d/%d", len(a), len(b), len(ra), len(rb))
	}
}

func TestDetectNoiseResidual(t *testing.T) {
	pts := groundWithBoxes()
	r := rand.New(rand.NewSource(7))
	for i := 0; i < 500; i++ { // 撒随机噪声点（不属于任何平面）
		pts = append(pts, pcd.Point{X: r.Float64()*30 - 5, Y: r.Float64()*12 - 1, Z: r.Float64()*5 + 0.5})
	}
	found, residual := Detect(pts, Options{Dist: 0.02, MinPoints: 300, Grid: 0.2, Simplify: 0.3})
	if len(residual) > 700 {
		t.Fatalf("噪声点应留在残余里（期望 ≤700，实际 %d）", len(residual))
	}
	if len(found) == 0 {
		t.Fatal("应能检出平面")
	}
}

func TestParseOptions(t *testing.T) {
	opt, err := ParseOptions("dist=0.05,min=1000,grid=0.25,tol=0.4,max=50,seed=9")
	if err != nil {
		t.Fatal(err)
	}
	if opt.Dist != 0.05 || opt.MinPoints != 1000 || opt.Grid != 0.25 || opt.Simplify != 0.4 || opt.MaxPlanes != 50 || opt.Seed != 9 {
		t.Fatalf("解析结果异常：%+v", opt)
	}
	if _, err := ParseOptions("dist=abc"); err == nil {
		t.Fatal("非法值应报错")
	}
	if _, err := ParseOptions("foo=1"); err == nil {
		t.Fatal("未知键应报错")
	}
	def, _ := ParseOptions("")
	if def.Dist != 0.03 || def.MinPoints != 500 || def.Trials != 128 {
		t.Fatalf("默认值异常：%+v", def)
	}
}

func TestSaveLoadRoundTrip(t *testing.T) {
	dir := t.TempDir()
	path := dir + "/planes.json"
	pts := groundWithBoxes()
	found, _ := Detect(pts, Options{Dist: 0.02, MinPoints: 300, Grid: 0.2, Simplify: 0.3})
	if err := Save(path, 0.02, found); err != nil {
		t.Fatal(err)
	}
	dist, loaded, err := Load(path)
	if err != nil || dist != 0.02 || len(loaded) != len(found) {
		t.Fatalf("回读异常：dist=%v n=%d/%d err=%v", dist, len(loaded), len(found), err)
	}
	if len(loaded[0].Polygon) < 3 {
		t.Fatal("回读多边形缺失")
	}
}

func TestEarClip(t *testing.T) {
	// 凹多边形（L 形）
	poly := [][2]float64{{0, 0}, {2, 0}, {2, 1}, {1, 1}, {1, 2}, {0, 2}}
	tris := EarClip(poly)
	if len(tris) != 4 {
		t.Fatalf("L 形应三角化为 4 片，实际 %d", len(tris))
	}
	// 面积守恒
	area := 0.0
	for _, tr := range tris {
		area += triArea2(poly[tr[0]], poly[tr[1]], poly[tr[2]])
	}
	if math.Abs(area-3) > 1e-9 {
		t.Fatalf("三角化面积 = %v，期望 3", area)
	}
}

// ---- 面积辅助 ----

func PolygonArea3D(p Plane) float64 {
	tris := p.Triangles()
	area := 0.0
	for _, tri := range tris {
		area += TriangleArea3D(tri)
	}
	return area
}

func TriangleArea3D(tri [3][3]float64) float64 {
	ab := [3]float64{tri[1][0] - tri[0][0], tri[1][1] - tri[0][1], tri[1][2] - tri[0][2]}
	ac := [3]float64{tri[2][0] - tri[0][0], tri[2][1] - tri[0][1], tri[2][2] - tri[0][2]}
	cr := cross(ab, ac)
	return norm(cr) / 2
}

func triArea2(a, b, c [2]float64) float64 {
	return math.Abs(cross2(a, b, c)) / 2
}
