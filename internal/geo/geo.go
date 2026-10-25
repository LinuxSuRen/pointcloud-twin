// Package geo 提供本项目需要的最小地理坐标换算（WGS84）：
// 大地坐标 ↔ ECEF、ENU → ECEF 旋转矩阵。闭式公式实现，不依赖 PROJ。
package geo

import "math"

const (
	wgs84A   = 6378137.0               // 长半轴（米）
	wgs84F   = 1.0 / 298.257223563     // 扁率
	wgs84E2  = wgs84F * (2.0 - wgs84F) // 第一偏心率平方
	degToRad = math.Pi / 180.0
)

// GeodeticToECEF 把 WGS84 大地坐标（度、度、米）转换为 ECEF（米）。
func GeodeticToECEF(lonDeg, latDeg, h float64) (x, y, z float64) {
	lon := lonDeg * degToRad
	lat := latDeg * degToRad
	sLat, cLat := math.Sin(lat), math.Cos(lat)
	sLon, cLon := math.Sin(lon), math.Cos(lon)
	n := wgs84A / math.Sqrt(1-wgs84E2*sLat*sLat)
	x = (n + h) * cLat * cLon
	y = (n + h) * cLat * sLon
	z = (n*(1-wgs84E2) + h) * sLat
	return x, y, z
}

// ENUToECEFMatrix 返回以 (lonDeg, latDeg, h) 为原点的 ENU → ECEF
// 齐次变换矩阵（列主序 4x4，长度 16），可直接作为 3D Tiles 根瓦片 transform。
func ENUToECEFMatrix(lonDeg, latDeg, h float64) [16]float64 {
	ox, oy, oz := GeodeticToECEF(lonDeg, latDeg, h)

	lon := lonDeg * degToRad
	lat := latDeg * degToRad
	sLat, cLat := math.Sin(lat), math.Cos(lat)
	sLon, cLon := math.Sin(lon), math.Cos(lon)

	// 行向量基：east = (-sLon, cLon, 0)；north = (-sLat*cLon, -sLat*sLon, cLat)；up = (cLat*cLon, cLat*sLon, sLat)
	// 列主序矩阵的列为基向量（作用于列向量 v：ECEF = M · ENU）
	return [16]float64{
		-sLon, cLon, 0, 0, // 第 1 列：east
		-sLat * cLon, -sLat * sLon, cLat, 0, // 第 2 列：north
		cLat * cLon, cLat * sLon, sLat, 0, // 第 3 列：up
		ox, oy, oz, 1, // 第 4 列：平移（ECEF 原点）
	}
}

// Mat4Mul 计算 a·b（列主序）。
func Mat4Mul(a, b [16]float64) [16]float64 {
	var out [16]float64
	for c := 0; c < 4; c++ {
		for r := 0; r < 4; r++ {
			var sum float64
			for k := 0; k < 4; k++ {
				sum += a[k*4+r] * b[c*4+k]
			}
			out[c*4+r] = sum
		}
	}
	return out
}

// TranslateMatrix 返回平移矩阵（列主序）。
func TranslateMatrix(x, y, z float64) [16]float64 {
	return [16]float64{
		1, 0, 0, 0,
		0, 1, 0, 0,
		0, 0, 1, 0,
		x, y, z, 1,
	}
}
