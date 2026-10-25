package tiler

import (
	"encoding/binary"
	"encoding/json"
)

// pnts 常量（3D Tiles 1.0 点云瓦片）
const (
	pntsMagic   = 0x706E7473
	pntsVersion = 1
)

// encodePnts 把一个瓦片的点编码为 .pnts 瓦片（3D Tiles 1.0）。
// positions 是相对瓦片中心的 z-up 坐标（float32×3），colors 是 RGB（uint8×3）。
func encodePnts(positions []float32, colors []uint8) ([]byte, error) {
	n := len(positions) / 3
	posBytes := f32Bytes(positions)

	feat := map[string]any{
		"POINTS_LENGTH": n,
		"POSITION":      map[string]int{"byteOffset": 0},
		"RGB":           map[string]int{"byteOffset": len(posBytes)},
	}
	jsonBytes, err := json.Marshal(feat)
	if err != nil {
		return nil, err
	}
	// Feature Table JSON 用空格补齐，使二进制段起始位置 8 字节对齐
	for (28+len(jsonBytes))%8 != 0 {
		jsonBytes = append(jsonBytes, 0x20)
	}

	body := len(posBytes) + len(colors)
	total := 28 + len(jsonBytes) + body
	totalPadded := total
	for totalPadded%8 != 0 {
		totalPadded++
	}

	out := make([]byte, 0, totalPadded)
	var h [28]byte
	binary.LittleEndian.PutUint32(h[0:4], pntsMagic)
	binary.LittleEndian.PutUint32(h[4:8], pntsVersion)
	binary.LittleEndian.PutUint32(h[8:12], uint32(totalPadded))
	binary.LittleEndian.PutUint32(h[12:16], uint32(len(jsonBytes)))
	binary.LittleEndian.PutUint32(h[16:20], uint32(body))
	binary.LittleEndian.PutUint32(h[20:24], 0)
	binary.LittleEndian.PutUint32(h[24:28], 0)
	out = append(out, h[:]...)
	out = append(out, jsonBytes...)
	out = append(out, posBytes...)
	out = append(out, colors...)
	for len(out) < totalPadded {
		out = append(out, 0)
	}
	return out, nil
}
