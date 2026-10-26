// Package tiler 把点云切分为 3D Tiles 瓦片集。
//
// 支持输出：
//   - 3D Tiles 1.1（默认）：glTF 二进制（.glb）瓦片，POINTS 图元；
//   - 3D Tiles 1.0：传统 .pnts 瓦片。
//
// 空间结构为八叉树；refine=ADD 时每个点恰好落在一个瓦片内容中（不跨 LOD 重复存储），
// refine=REPLACE 时子瓦片承载全量点、父瓦片承载抽稀粗层。
package tiler

import "github.com/LinuxSuRen/pointcloud-twin/internal/pcd"

// tileNode 是八叉树节点。
type tileNode struct {
	// center/half 是节点立方体（局部 ENU 坐标，米）。
	center [3]float64
	half   float64
	// indices 是该子树的全部点索引（决定了瓦片包围盒）。
	indices []int32
	// content 是该瓦片自身内容包含的点索引（叶节点 = indices）。
	content []int32
	// bbox 是 indices 的紧致包围盒。
	bboxMin, bboxMax [3]float64
	children         []*tileNode
}

const (
	subsampleStride = 8
	minHalf         = 1e-4 // 立方体最小半边长（米），防止退化点集无限切分
	maxDepth        = 24
)

// buildTree 构建八叉树。bbox 为整幅点云包围盒；replace 为 true 时子瓦片承载全量点。
func buildTree(points []pcd.Point, indices []int32, bboxMin, bboxMax [3]float64, target int, replace bool, depth int) *tileNode {
	n := &tileNode{indices: indices}
	var c [3]float64
	var maxExtent float64
	for i := 0; i < 3; i++ {
		c[i] = (bboxMin[i] + bboxMax[i]) / 2
		if ext := bboxMax[i] - bboxMin[i]; ext > maxExtent {
			maxExtent = ext
		}
	}
	n.center = c
	n.half = maxExtent/2 + 1e-6 // 加 epsilon 保证边界点落入

	// 覆盖全部点的紧致包围盒
	n.bboxMin, n.bboxMax = boundsOf(points, indices)

	shouldSplit := len(indices) > target && depth < maxDepth && n.half > minHalf
	if !shouldSplit {
		n.content = indices
		return n
	}

	// 父瓦片内容：等步长抽稀，作为粗层 LOD
	var content, down []int32
	for i, idx := range indices {
		if i%subsampleStride == 0 {
			content = append(content, idx)
		} else {
			down = append(down, idx)
		}
	}
	if replace {
		down = indices // REPLACE：子瓦片承载全量点
	}
	n.content = content

	// 八分域划分
	var buckets [8][]int32
	h2 := n.half / 2
	for _, idx := range down {
		p := points[idx]
		b := 0
		if p.X >= n.center[0] {
			b |= 1
		}
		if p.Y >= n.center[1] {
			b |= 2
		}
		if p.Z >= n.center[2] {
			b |= 4
		}
		buckets[b] = append(buckets[b], idx)
	}
	for b := 0; b < 8; b++ {
		if len(buckets[b]) == 0 {
			continue
		}
		cc := [3]float64{n.center[0], n.center[1], n.center[2]}
		if b&1 == 0 {
			cc[0] -= h2
		} else {
			cc[0] += h2
		}
		if b&2 == 0 {
			cc[1] -= h2
		} else {
			cc[1] += h2
		}
		if b&4 == 0 {
			cc[2] -= h2
		} else {
			cc[2] += h2
		}
		cmin, cmax := boundsOf(points, buckets[b])
		child := buildTree(points, buckets[b], cmin, cmax, target, replace, depth+1)
		child.center = cc
		child.half = h2
		n.children = append(n.children, child)
	}
	return n
}

func boundsOf(points []pcd.Point, indices []int32) (mn, mx [3]float64) {
	mn = [3]float64{inf(), inf(), inf()}
	mx = [3]float64{-inf(), -inf(), -inf()}
	for _, idx := range indices {
		p := points[idx]
		v := [3]float64{p.X, p.Y, p.Z}
		for i := 0; i < 3; i++ {
			if v[i] < mn[i] {
				mn[i] = v[i]
			}
			if v[i] > mx[i] {
				mx[i] = v[i]
			}
		}
	}
	return mn, mx
}
