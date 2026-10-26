// Package downsample 提供点云预处理：体素栅格降采样。
package downsample

import "github.com/LinuxSuRen/pointcloud-twin/internal/pcd"

type voxelKey struct {
	x, y, z int64
}

// Voxel 执行体素栅格降采样：每个被占据的体素保留第一个点。
// size 为体素边长（米），非正值时原样返回。
func Voxel(points []pcd.Point, size float64) []pcd.Point {
	if size <= 0 || len(points) == 0 {
		return points
	}
	seen := make(map[voxelKey]struct{}, len(points)/2)
	out := make([]pcd.Point, 0, len(points)/2)
	for _, p := range points {
		k := voxelKey{
			x: floorDiv(p.X, size),
			y: floorDiv(p.Y, size),
			z: floorDiv(p.Z, size),
		}
		if _, dup := seen[k]; dup {
			continue
		}
		seen[k] = struct{}{}
		out = append(out, p)
	}
	return out
}

// floorDiv 把坐标映射为体素索引（负坐标向下取整）。
func floorDiv(v, size float64) int64 {
	i := int64(v / size)
	if v < 0 && float64(i)*size > v {
		i--
	}
	return i
}
