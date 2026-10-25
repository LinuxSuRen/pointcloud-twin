// Package export 把转换产物导出为面向外部平台的数字孪生模型：
//   - 单文件 GLB（glTF 2.0 二进制）：点云 + 设备代理体网格，本地 ENU 坐标系
//     （米），根节点携带 z-up→y-up 变换，可直接导入 UE/Unity/Blender/three.js；
//   - zip 打包：3D Tiles 目录 + devices.json + report.json，部署到 Cesium 系平台。
package export

import (
	"bytes"
	"encoding/binary"
	"encoding/json"
	"math"

	"github.com/LinuxSuRen/pointcloud-twin/internal/downsample"
	"github.com/LinuxSuRen/pointcloud-twin/internal/pcd"
	"github.com/LinuxSuRen/pointcloud-twin/internal/proxy"
)

// deviceTypeColors 是设备类型的默认材质色（线性 RGB，0~1）。
var deviceTypeColors = map[string][3]float64{
	"battery-cluster": {0.18, 0.38, 0.71},
	"pcs":             {0.84, 0.49, 0.17},
	"transformer":     {0.42, 0.48, 0.42},
}

func deviceColor(t string) [3]float64 {
	if c, ok := deviceTypeColors[t]; ok {
		return c
	}
	return [3]float64{0.24, 0.62, 0.94}
}

// zUpToYUp 是列主序 z-up→y-up 旋转（与 3D Tiles 瓦片一致的规范做法），
// 使 z-up 源数据在 y-up 的 glTF/通用引擎中保持直立。
var zUpToYUp = [16]float32{
	1, 0, 0, 0,
	0, 0, -1, 0,
	0, 1, 0, 0,
	0, 0, 0, 1,
}

// ---- glTF 文档构建 ----

type gltfAccessor struct {
	BufferView    int       `json:"bufferView"`
	ComponentType int       `json:"componentType"`
	Count         int       `json:"count"`
	Type          string    `json:"type"`
	Min           []float32 `json:"min,omitempty"`
	Max           []float32 `json:"max,omitempty"`
}

type gltfBufferView struct {
	Buffer     int `json:"buffer"`
	ByteOffset int `json:"byteOffset"`
	ByteLength int `json:"byteLength"`
}

type gltfPrimitive struct {
	Mode       int            `json:"mode"`
	Attributes map[string]int `json:"attributes"`
	Indices    *int           `json:"indices,omitempty"`
	Material   *int           `json:"material,omitempty"`
}

type gltfNode struct {
	Name        string       `json:"name,omitempty"`
	Matrix      *[16]float32 `json:"matrix,omitempty"`
	Mesh        *int         `json:"mesh,omitempty"`
	Translation *[3]float64  `json:"translation,omitempty"`
	Rotation    *[4]float64  `json:"rotation,omitempty"`
	Scale       *[3]float64  `json:"scale,omitempty"`
	Children    []int        `json:"children,omitempty"`
}

type gltfMaterial struct {
	Name string `json:"name"`
	PBR  struct {
		BaseColorFactor [4]float64 `json:"baseColorFactor"`
		MetallicFactor  float64    `json:"metallicFactor"`
		RoughnessFactor float64    `json:"roughnessFactor"`
	} `json:"pbrMetallicRoughness"`
	DoubleSided bool `json:"doubleSided"`
}

type gltfDoc struct {
	Asset struct {
		Version   string `json:"version"`
		Generator string `json:"generator"`
	} `json:"asset"`
	Scene  int `json:"scene"`
	Scenes []struct {
		Nodes []int `json:"nodes"`
	} `json:"scenes"`
	Nodes       []gltfNode        `json:"nodes"`
	Meshes      [][]gltfPrimitive `json:"meshes"`
	Materials   []gltfMaterial    `json:"materials"`
	Accessors   []gltfAccessor    `json:"accessors"`
	BufferViews []gltfBufferView  `json:"bufferViews"`
	Buffers     []struct {
		ByteLength int `json:"byteLength"`
	} `json:"buffers"`
}

type glbBuilder struct {
	doc      gltfDoc
	bin      bytes.Buffer
	children []int
}

func newBuilder() *glbBuilder {
	b := &glbBuilder{}
	b.doc.Asset.Version = "2.0"
	b.doc.Asset.Generator = "pointcloud-twin export"
	b.doc.Scene = 0
	b.doc.Scenes = append(b.doc.Scenes, struct {
		Nodes []int `json:"nodes"`
	}{})
	return b
}

func (b *glbBuilder) addView(data []byte) int {
	for b.bin.Len()%4 != 0 {
		b.bin.WriteByte(0)
	}
	view := gltfBufferView{Buffer: 0, ByteOffset: b.bin.Len(), ByteLength: len(data)}
	b.doc.BufferViews = append(b.doc.BufferViews, view)
	b.bin.Write(data)
	return len(b.doc.BufferViews) - 1
}

func (b *glbBuilder) addAccessor(view, componentType int, typ string, count int, min, max []float32) int {
	b.doc.Accessors = append(b.doc.Accessors, gltfAccessor{
		BufferView: view, ComponentType: componentType, Type: typ, Count: count, Min: min, Max: max,
	})
	return len(b.doc.Accessors) - 1
}

func (b *glbBuilder) addMesh(prims []gltfPrimitive) int {
	b.doc.Meshes = append(b.doc.Meshes, prims)
	return len(b.doc.Meshes) - 1
}

func (b *glbBuilder) addNode(n gltfNode) int {
	b.doc.Nodes = append(b.doc.Nodes, n)
	return len(b.doc.Nodes) - 1
}

func (b *glbBuilder) build() ([]byte, error) {
	b.doc.Buffers = append(b.doc.Buffers, struct {
		ByteLength int `json:"byteLength"`
	}{ByteLength: b.bin.Len()})

	jsonBytes, err := marshalJSON(&b.doc)
	if err != nil {
		return nil, err
	}
	for len(jsonBytes)%4 != 0 {
		jsonBytes = append(jsonBytes, 0x20)
	}
	binPadded := b.bin.Len()
	for binPadded%4 != 0 {
		binPadded++
	}
	total := 12 + 8 + len(jsonBytes) + 8 + binPadded

	out := make([]byte, 0, total)
	var h [12]byte
	binary.LittleEndian.PutUint32(h[0:4], 0x46546C67)
	binary.LittleEndian.PutUint32(h[4:8], 2)
	binary.LittleEndian.PutUint32(h[8:12], uint32(total))
	out = append(out, h[:]...)
	var c0 [8]byte
	binary.LittleEndian.PutUint32(c0[0:4], uint32(len(jsonBytes)))
	binary.LittleEndian.PutUint32(c0[4:8], 0x4E4F534A)
	out = append(out, c0[:]...)
	out = append(out, jsonBytes...)
	var c1 [8]byte
	binary.LittleEndian.PutUint32(c1[0:4], uint32(binPadded))
	binary.LittleEndian.PutUint32(c1[4:8], 0x004E4942)
	out = append(out, c1[:]...)
	out = append(out, b.bin.Bytes()...)
	for len(out) < total {
		out = append(out, 0)
	}
	return out, nil
}

func marshalJSON(v any) ([]byte, error) {
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	if err := enc.Encode(v); err != nil {
		return nil, err
	}
	return bytes.TrimRight(buf.Bytes(), "\n"), nil
}

// ---- GLB 导出 ----

// GLBOptions 控制 GLB 导出。
type GLBOptions struct {
	// Voxel 为可选降采样边长（米）；0 表示不降采样。大点云建议降采样控制单文件体积。
	Voxel float64
}

// BuildGLB 把点云与设备代理体组装为单文件 GLB（数字孪生模型）。
// 坐标为本地 ENU（米）；根节点矩阵完成 z-up→y-up，导入通用引擎后直立。
func BuildGLB(cloud *pcd.Cloud, devices []proxy.Device, opt GLBOptions) ([]byte, error) {
	points := cloud.Points
	if opt.Voxel > 0 {
		points = downsample.Voxel(points, opt.Voxel)
	}

	b := newBuilder()

	// 点云网格：POINTS 图元
	if len(points) > 0 {
		pos := make([]float32, 0, len(points)*3)
		col := make([]uint8, 0, len(points)*4)
		mn := [3]float32{math.MaxFloat32, math.MaxFloat32, math.MaxFloat32}
		mx := [3]float32{-math.MaxFloat32, -math.MaxFloat32, -math.MaxFloat32}
		for _, p := range points {
			pos = append(pos, float32(p.X), float32(p.Y), float32(p.Z))
			r, g, bl := pointColor(p)
			col = append(col, r, g, bl, 255)
			for d := 0; d < 3; d++ {
				v := pos[len(pos)-3+d]
				if v < mn[d] {
					mn[d] = v
				}
				if v > mx[d] {
					mx[d] = v
				}
			}
		}
		posAcc := b.addAccessor(b.addView(f32bytes(pos)), 5126, "VEC3", len(points), mn[:], mx[:])
		colAcc := b.addAccessor(b.addView(col), 5121, "VEC4", len(points), nil, nil)
		mesh := b.addMesh([]gltfPrimitive{{
			Mode:       0,
			Attributes: map[string]int{"POSITION": posAcc, "COLOR_0": colAcc},
		}})
		node := b.addNode(gltfNode{Name: "pointcloud", Mesh: &mesh})
		b.children = append(b.children, node)
	}

	// 设备盒体：共享单位立方体网格，节点按设备平移/旋转/缩放
	boxVerts, boxNormals, boxIndices := unitBox()
	bvAcc := b.addAccessor(b.addView(f32bytes(boxVerts)), 5126, "VEC3", len(boxVerts)/3, nil, nil)
	bnAcc := b.addAccessor(b.addView(f32bytes(boxNormals)), 5126, "VEC3", len(boxNormals)/3, nil, nil)
	biAcc := b.addAccessor(b.addView(u16bytes(boxIndices)), 5123, "SCALAR", len(boxIndices), nil, nil)

	matByType := map[string]int{}
	for i := range devices {
		t := devices[i].DeviceType
		matIdx, ok := matByType[t]
		if !ok {
			c := deviceColor(t)
			mat := gltfMaterial{Name: t, DoubleSided: true}
			mat.PBR.BaseColorFactor = [4]float64{c[0], c[1], c[2], 1}
			mat.PBR.MetallicFactor = 0
			mat.PBR.RoughnessFactor = 0.9
			b.doc.Materials = append(b.doc.Materials, mat)
			matIdx = len(b.doc.Materials) - 1
			matByType[t] = matIdx
		}
		mesh := b.addMesh([]gltfPrimitive{{
			Mode:       4,
			Attributes: map[string]int{"POSITION": bvAcc, "NORMAL": bnAcc},
			Indices:    &biAcc,
			Material:   &matIdx,
		}})
		// heading 与 Cesium 一致（俯视顺时针为正），等价于绕 +Z 旋转 -θ
		heading := -devices[i].Heading * math.Pi / 180
		half := heading / 2
		rot := [4]float64{0, 0, math.Sin(half), math.Cos(half)}
		node := b.addNode(gltfNode{
			Name:        devices[i].DeviceID,
			Mesh:        &mesh,
			Translation: &[3]float64{devices[i].Center[0], devices[i].Center[1], devices[i].Center[2]},
			Rotation:    &rot,
			Scale:       &[3]float64{devices[i].Size[0], devices[i].Size[1], devices[i].Size[2]},
		})
		b.children = append(b.children, node)
	}

	root := b.addNode(gltfNode{Name: "root", Matrix: &zUpToYUp, Children: b.children})
	b.doc.Scenes[0].Nodes = []int{root}
	return b.build()
}

// pointColor 与 tiler 一致的颜色回退规则。
func pointColor(p pcd.Point) (r, g, b uint8) {
	if p.HasRGB {
		return p.R, p.G, p.B
	}
	if p.HasIntensity {
		v := uint8(60 + float64(p.Intensity)/65535.0*180.0)
		return v, v, v
	}
	return 176, 176, 176
}

// unitBox 返回中心在原点的单位立方体（24 顶点带法线，36 索引）。
func unitBox() (verts, normals []float32, indices []uint16) {
	axis := [3][3]float32{{1, 0, 0}, {0, 1, 0}, {0, 0, 1}}
	addFace := func(n, u, v [3]float32) {
		base := uint16(len(verts) / 3)
		for _, su := range []float32{-0.5, 0.5} {
			for _, sv := range []float32{-0.5, 0.5} {
				verts = append(verts,
					n[0]*0.5+u[0]*su+v[0]*sv,
					n[1]*0.5+u[1]*su+v[1]*sv,
					n[2]*0.5+u[2]*su+v[2]*sv)
				normals = append(normals, n[0], n[1], n[2])
			}
		}
		indices = append(indices,
			base, base+1, base+2,
			base+2, base+1, base+3)
	}
	for a := 0; a < 3; a++ {
		n, u, v := axis[a], axis[(a+1)%3], axis[(a+2)%3]
		addFace(n, u, v)
		addFace([3]float32{-n[0], -n[1], -n[2]}, u, v)
	}
	return verts, normals, indices
}

func f32bytes(vals []float32) []byte {
	out := make([]byte, 4*len(vals))
	for i, v := range vals {
		binary.LittleEndian.PutUint32(out[i*4:i*4+4], math.Float32bits(v))
	}
	return out
}

func u16bytes(vals []uint16) []byte {
	out := make([]byte, 2*len(vals))
	for i, v := range vals {
		binary.LittleEndian.PutUint16(out[i*2:i*2+2], v)
	}
	return out
}
