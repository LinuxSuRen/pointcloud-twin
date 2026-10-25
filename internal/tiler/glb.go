package tiler

import (
	"bytes"
	"encoding/binary"
	"encoding/json"
	"math"
)

// glTF 常量
const (
	glbMagic        = 0x46546C67
	chunkJSON       = 0x4E4F534A
	chunkBIN        = 0x004E4942
	componentFloat  = 5126
	componentUByte  = 5121
	targetArrayBuf  = 34962
	primitivePoints = 0
)

// zUpToYUpMatrix 是 3D Tiles 1.1 规范「informative」节给出的 glTF 根节点矩阵
// （列主序）。源数据本身是 z-up，写入时顶点保持原值，运行时 y-up→z-up 旋转与
// 该矩阵相抵消。
var zUpToYUpMatrix = [16]float32{
	1, 0, 0, 0,
	0, 0, -1, 0,
	0, 1, 0, 0,
	0, 0, 0, 1,
}

// encodeGLB 把一个瓦片的点编码为二进制 glTF（POINTS 图元）。
// positions 是相对瓦片中心的 z-up 坐标（float32×3），colors 是 RGBA（uint8×4）。
func encodeGLB(positions []float32, colors []uint8) ([]byte, error) {
	n := len(positions) / 3
	posBytes := f32Bytes(positions)
	colBytes := colors

	// POSITION accessor 需要 min/max
	min, max := minMaxVec3(positions)

	type accessor struct {
		BufferView    int       `json:"bufferView"`
		ComponentType int       `json:"componentType"`
		Count         int       `json:"count"`
		Type          string    `json:"type"`
		Min           []float32 `json:"min,omitempty"`
		Max           []float32 `json:"max,omitempty"`
		Normalized    bool      `json:"normalized,omitempty"`
	}
	type doc struct {
		Asset struct {
			Version   string `json:"version"`
			Generator string `json:"generator"`
		} `json:"asset"`
		Scene  int `json:"scene"`
		Scenes []struct {
			Nodes []int `json:"nodes"`
		} `json:"scenes"`
		Nodes []struct {
			Name   string      `json:"name"`
			Matrix [16]float32 `json:"matrix"`
			Mesh   int         `json:"mesh"`
		} `json:"nodes"`
		Meshes []struct {
			Primitives []struct {
				Mode       int            `json:"mode"`
				Attributes map[string]int `json:"attributes"`
				Material   int            `json:"material"`
			} `json:"primitives"`
		} `json:"meshes"`
		Materials []struct {
			Name string `json:"name"`
			PBR  struct {
				BaseColorFactor [4]float32 `json:"baseColorFactor"`
				MetallicFactor  float32    `json:"metallicFactor"`
				RoughnessFactor float32    `json:"roughnessFactor"`
			} `json:"pbrMetallicRoughness"`
		} `json:"materials"`
		Accessors   []accessor `json:"accessors"`
		BufferViews []struct {
			Buffer     int `json:"buffer"`
			ByteOffset int `json:"byteOffset"`
			ByteLength int `json:"byteLength"`
			Target     int `json:"target"`
		} `json:"bufferViews"`
		Buffers []struct {
			ByteLength int `json:"byteLength"`
		} `json:"buffers"`
	}

	var g doc
	g.Asset.Version = "2.0"
	g.Asset.Generator = "pointcloud-twin"
	g.Scene = 0
	g.Scenes = append(g.Scenes, struct {
		Nodes []int `json:"nodes"`
	}{Nodes: []int{0}})
	g.Nodes = append(g.Nodes, struct {
		Name   string      `json:"name"`
		Matrix [16]float32 `json:"matrix"`
		Mesh   int         `json:"mesh"`
	}{Name: "root", Matrix: zUpToYUpMatrix, Mesh: 0})
	g.Meshes = append(g.Meshes, struct {
		Primitives []struct {
			Mode       int            `json:"mode"`
			Attributes map[string]int `json:"attributes"`
			Material   int            `json:"material"`
		} `json:"primitives"`
	}{Primitives: []struct {
		Mode       int            `json:"mode"`
		Attributes map[string]int `json:"attributes"`
		Material   int            `json:"material"`
	}{
		{
			Mode:       primitivePoints,
			Attributes: map[string]int{"POSITION": 0, "COLOR_0": 1},
			Material:   0,
		},
	}})
	mat := struct {
		Name string `json:"name"`
		PBR  struct {
			BaseColorFactor [4]float32 `json:"baseColorFactor"`
			MetallicFactor  float32    `json:"metallicFactor"`
			RoughnessFactor float32    `json:"roughnessFactor"`
		} `json:"pbrMetallicRoughness"`
	}{Name: "points"}
	mat.PBR.BaseColorFactor = [4]float32{1, 1, 1, 1}
	mat.PBR.MetallicFactor = 0
	mat.PBR.RoughnessFactor = 1
	g.Materials = append(g.Materials, mat)

	g.Accessors = []accessor{
		{BufferView: 0, ComponentType: componentFloat, Count: n, Type: "VEC3", Min: min, Max: max},
		{BufferView: 1, ComponentType: componentUByte, Count: n, Type: "VEC4", Normalized: true},
	}
	g.BufferViews = []struct {
		Buffer     int `json:"buffer"`
		ByteOffset int `json:"byteOffset"`
		ByteLength int `json:"byteLength"`
		Target     int `json:"target"`
	}{
		{Buffer: 0, ByteOffset: 0, ByteLength: len(posBytes), Target: targetArrayBuf},
		{Buffer: 0, ByteOffset: len(posBytes), ByteLength: len(colBytes), Target: targetArrayBuf},
	}
	g.Buffers = []struct {
		ByteLength int `json:"byteLength"`
	}{{ByteLength: len(posBytes) + len(colBytes)}}

	var jsonBuf bytes.Buffer
	enc := json.NewEncoder(&jsonBuf)
	enc.SetEscapeHTML(false)
	if err := enc.Encode(&g); err != nil {
		return nil, err
	}
	jsonBytes := bytes.TrimRight(jsonBuf.Bytes(), "\n")
	for len(jsonBytes)%4 != 0 {
		jsonBytes = append(jsonBytes, 0x20)
	}

	binLen := len(posBytes) + len(colBytes)
	binPadded := binLen
	for binPadded%4 != 0 {
		binPadded++
	}
	total := 12 + 8 + len(jsonBytes) + 8 + binPadded

	out := make([]byte, 0, total)
	var h [12]byte
	binary.LittleEndian.PutUint32(h[0:4], glbMagic)
	binary.LittleEndian.PutUint32(h[4:8], 2)
	binary.LittleEndian.PutUint32(h[8:12], uint32(total))
	out = append(out, h[:]...)

	var c0 [8]byte
	binary.LittleEndian.PutUint32(c0[0:4], uint32(len(jsonBytes)))
	binary.LittleEndian.PutUint32(c0[4:8], chunkJSON)
	out = append(out, c0[:]...)
	out = append(out, jsonBytes...)

	var c1 [8]byte
	binary.LittleEndian.PutUint32(c1[0:4], uint32(binPadded))
	binary.LittleEndian.PutUint32(c1[4:8], chunkBIN)
	out = append(out, c1[:]...)
	out = append(out, posBytes...)
	out = append(out, colBytes...)
	for len(out) < total {
		out = append(out, 0)
	}
	return out, nil
}

func f32Bytes(vals []float32) []byte {
	out := make([]byte, 4*len(vals))
	for i, v := range vals {
		binary.LittleEndian.PutUint32(out[i*4:i*4+4], math.Float32bits(v))
	}
	return out
}

func minMaxVec3(positions []float32) (mn, mx []float32) {
	mn = []float32{math.MaxFloat32, math.MaxFloat32, math.MaxFloat32}
	mx = []float32{-math.MaxFloat32, -math.MaxFloat32, -math.MaxFloat32}
	for i := 0; i+2 < len(positions); i += 3 {
		for d := 0; d < 3; d++ {
			if positions[i+d] < mn[d] {
				mn[d] = positions[i+d]
			}
			if positions[i+d] > mx[d] {
				mx[d] = positions[i+d]
			}
		}
	}
	return mn, mx
}
