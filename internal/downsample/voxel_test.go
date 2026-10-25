package downsample

import (
	"testing"

	"github.com/LinuxSuRen/pointcloud-twin/internal/pcd"
)

func TestVoxelDedup(t *testing.T) {
	points := []pcd.Point{
		{X: 0, Y: 0, Z: 0, R: 1},
		{X: 0.01, Y: 0.01, Z: 0.01, R: 2}, // 同一体素
		{X: 0.9, Y: 0, Z: 0, R: 3},        // 相邻体素
		{X: -0.9, Y: -0.9, Z: -0.9, R: 4}, // 负方向体素
	}
	out := Voxel(points, 0.5)
	if len(out) != 3 {
		t.Fatalf("降采样后 = %d 点，期望 3", len(out))
	}
	if out[0].R != 1 {
		t.Fatalf("应保留体素内第一个点：%v", out[0])
	}
}

func TestVoxelDisabled(t *testing.T) {
	points := []pcd.Point{{X: 0}, {X: 0.1}}
	if got := Voxel(points, 0); len(got) != 2 {
		t.Fatalf("size=0 应原样返回：%d", len(got))
	}
	if got := Voxel(nil, 1); len(got) != 0 {
		t.Fatal("空输入应返回空")
	}
}
