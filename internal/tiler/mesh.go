package tiler

import (
	"bytes"
	"encoding/binary"
	"encoding/json"
)

// encodeMeshGLB 把三角网（顶点 + 法向 + 索引 + 单色材质）编码为 GLB。
// 用于平面分割产出的面片瓦片（3D Tiles 1.1）。
func encodeMeshGLB(positions []float32, normals []float32, indices []uint32, cr, cg, cb uint8) ([]byte, error) {
	posBytes := f32Bytes(positions)
	nrmBytes := f32Bytes(normals)
	idxBytes := u32Bytes(indices)

	min, max := minMaxVec3(positions)

	type accessor struct {
		BufferView    int       `json:"bufferView"`
		ComponentType int       `json:"componentType"`
		Count         int       `json:"count"`
		Type          string    `json:"type"`
		Min           []float32 `json:"min,omitempty"`
		Max           []float32 `json:"max,omitempty"`
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
				Indices    int            `json:"indices"`
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
			DoubleSided bool `json:"doubleSided"`
		} `json:"materials"`
		Accessors   []accessor `json:"accessors"`
		BufferViews []struct {
			Buffer     int `json:"buffer"`
			ByteOffset int `json:"byteOffset"`
			ByteLength int `json:"byteLength"`
		} `json:"bufferViews"`
		Buffers []struct {
			ByteLength int `json:"byteLength"`
		} `json:"buffers"`
	}

	var gd doc
	gd.Asset.Version = "2.0"
	gd.Asset.Generator = generator
	gd.Scene = 0
	gd.Scenes = append(gd.Scenes, struct {
		Nodes []int `json:"nodes"`
	}{Nodes: []int{0}})
	gd.Nodes = append(gd.Nodes, struct {
		Name   string      `json:"name"`
		Matrix [16]float32 `json:"matrix"`
		Mesh   int         `json:"mesh"`
	}{Name: "plane", Matrix: zUpToYUpMatrix, Mesh: 0})
	gd.Meshes = append(gd.Meshes, struct {
		Primitives []struct {
			Mode       int            `json:"mode"`
			Attributes map[string]int `json:"attributes"`
			Indices    int            `json:"indices"`
			Material   int            `json:"material"`
		} `json:"primitives"`
	}{Primitives: []struct {
		Mode       int            `json:"mode"`
		Attributes map[string]int `json:"attributes"`
		Indices    int            `json:"indices"`
		Material   int            `json:"material"`
	}{
		{
			Mode:       4, // TRIANGLES
			Attributes: map[string]int{"POSITION": 0, "NORMAL": 1},
			Indices:    2,
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
		DoubleSided bool `json:"doubleSided"`
	}{Name: "plane", DoubleSided: true}
	mat.PBR.BaseColorFactor = [4]float32{float32(cr) / 255, float32(cg) / 255, float32(cb) / 255, 1}
	mat.PBR.MetallicFactor = 0
	mat.PBR.RoughnessFactor = 0.9
	gd.Materials = append(gd.Materials, mat)

	n := len(positions) / 3
	gd.Accessors = []accessor{
		{BufferView: 0, ComponentType: 5126, Count: n, Type: "VEC3", Min: min, Max: max},
		{BufferView: 1, ComponentType: 5126, Count: n, Type: "VEC3"},
		{BufferView: 2, ComponentType: 5125, Count: len(indices), Type: "SCALAR"},
	}
	gd.BufferViews = []struct {
		Buffer     int `json:"buffer"`
		ByteOffset int `json:"byteOffset"`
		ByteLength int `json:"byteLength"`
	}{
		{Buffer: 0, ByteOffset: 0, ByteLength: len(posBytes)},
		{Buffer: 0, ByteOffset: len(posBytes), ByteLength: len(nrmBytes)},
		{Buffer: 0, ByteOffset: len(posBytes) + len(nrmBytes), ByteLength: len(idxBytes)},
	}
	gd.Buffers = []struct {
		ByteLength int `json:"byteLength"`
	}{{ByteLength: len(posBytes) + len(nrmBytes) + len(idxBytes)}}

	var jsonBuf bytes.Buffer
	enc := json.NewEncoder(&jsonBuf)
	enc.SetEscapeHTML(false)
	if err := enc.Encode(&gd); err != nil {
		return nil, err
	}
	jsonBytes := bytes.TrimRight(jsonBuf.Bytes(), "\n")
	for len(jsonBytes)%4 != 0 {
		jsonBytes = append(jsonBytes, 0x20)
	}
	binLen := len(posBytes) + len(nrmBytes) + len(idxBytes)
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
	out = append(out, nrmBytes...)
	out = append(out, idxBytes...)
	for len(out) < total {
		out = append(out, 0)
	}
	return out, nil
}

func u32Bytes(vals []uint32) []byte {
	out := make([]byte, 4*len(vals))
	for i, v := range vals {
		binary.LittleEndian.PutUint32(out[i*4:i*4+4], v)
	}
	return out
}
