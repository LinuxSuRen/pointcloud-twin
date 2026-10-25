package geo

import (
	"math"
	"testing"
)

func TestGeodeticToECEF(t *testing.T) {
	x, y, z := GeodeticToECEF(0, 0, 0)
	if math.Abs(x-6378137) > 1e-6 || math.Abs(y) > 1e-6 || math.Abs(z) > 1e-6 {
		t.Fatalf("原点 ECEF = %v %v %v", x, y, z)
	}
}

func TestENUToECEFMatrix(t *testing.T) {
	m := ENUToECEFMatrix(0, 0, 0)
	// 在 (0°,0°)：east=(0,1,0)，north=(0,0,1)，up=(1,0,0)，原点=(a,0,0)
	apply := func(v [3]float64) [3]float64 {
		var out [3]float64
		for r := 0; r < 3; r++ {
			out[r] = m[0*4+r]*v[0] + m[1*4+r]*v[1] + m[2*4+r]*v[2] + m[3*4+r]
		}
		return out
	}
	origin := apply([3]float64{0, 0, 0})
	if math.Abs(origin[0]-6378137) > 1e-6 || math.Abs(origin[1]) > 1e-9 || math.Abs(origin[2]) > 1e-9 {
		t.Fatalf("原点平移错误：%v", origin)
	}
	if e := apply([3]float64{1, 0, 0}); math.Abs(e[1]-1) > 1e-9 || math.Abs(e[0]-origin[0]) > 1e-9 {
		t.Fatalf("east 轴错误：%v", e)
	}
	if n := apply([3]float64{0, 1, 0}); math.Abs(n[2]-1) > 1e-9 {
		t.Fatalf("north 轴错误：%v", n)
	}
	if u := apply([3]float64{0, 0, 1}); math.Abs(u[0]-origin[0]-1) > 1e-9 {
		t.Fatalf("up 轴错误：%v", u)
	}
}

func TestMat4MulTranslate(t *testing.T) {
	t1 := TranslateMatrix(1, 2, 3)
	t2 := TranslateMatrix(10, 20, 30)
	m := Mat4Mul(t1, t2)
	// 作用在原点上得到平移之和
	v := [4]float64{0, 0, 0, 1}
	var out [4]float64
	for r := 0; r < 4; r++ {
		out[r] = m[0*4+r]*v[0] + m[1*4+r]*v[1] + m[2*4+r]*v[2] + m[3*4+r]*v[3]
	}
	if out[0] != 11 || out[1] != 22 || out[2] != 33 {
		t.Fatalf("矩阵乘法错误：%v", out)
	}
}
