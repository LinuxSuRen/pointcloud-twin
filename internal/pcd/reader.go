package pcd

import (
	"encoding/binary"
	"fmt"
	"io"
	"math"
	"strconv"
	"strings"

	"github.com/LinuxSuRen/pointcloud-twin/internal/apperr"
)

// readASCII 解析 DATA ascii 编码的点云。
func (r *reader) readASCII() (*Cloud, error) {
	h := &r.header
	expanded := h.expandedFields()
	cloud := &Cloud{}
	cloud.Points = make([]Point, 0, h.Points)

	// 回到数据区起始位置重新按行读
	if err := r.seekData(); err != nil {
		return nil, err
	}
	n := 0
	for n < h.Points {
		line, err := r.readLine()
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, apperr.New(apperr.E1002, err, "读取 ASCII 数据失败")
		}
		if strings.TrimSpace(line) == "" {
			continue
		}
		vals := strings.Fields(line)
		if len(vals) < h.expandedCount() {
			return nil, apperr.New(apperr.E1002, nil,
				fmt.Sprintf("第 %d 行字段数不足：%d < %d", n+1, len(vals), h.expandedCount()))
		}
		p, ok, err := buildPointFromASCII(expanded, vals)
		if err != nil {
			return nil, err
		}
		if ok {
			cloud.Points = append(cloud.Points, p)
		} else {
			cloud.Skipped++
		}
		n++
	}
	return cloud, nil
}

// readBinary 解析 DATA binary 编码的点云。
func (r *reader) readBinary() (*Cloud, error) {
	h := &r.header
	buf := make([]byte, h.pointSize*h.Points)
	if err := r.seekData(); err != nil {
		return nil, err
	}
	if _, err := io.ReadFull(r.br, buf); err != nil {
		return nil, apperr.New(apperr.E1002, err,
			fmt.Sprintf("binary 数据不完整（期望 %d 字节）", len(buf)))
	}
	return r.decodeBinary(buf, h.Points, h.pointSize), nil
}

// readBinaryCompressed 解析 DATA binary_compressed（LZF）编码的点云。
//
// 压缩前数据按「字段优先」布局：字段 i 的全部 Points 个值连续存放，
// 其偏移由前面字段占用字节数累加得到。
func (r *reader) readBinaryCompressed() (*Cloud, error) {
	h := &r.header
	if err := r.seekData(); err != nil {
		return nil, err
	}
	var hdr [8]byte
	if _, err := io.ReadFull(r.br, hdr[:]); err != nil {
		return nil, apperr.New(apperr.E1002, err, "binary_compressed 头读取失败")
	}
	compressedLen := binary.LittleEndian.Uint32(hdr[0:4])
	uncompressedLen := binary.LittleEndian.Uint32(hdr[4:8])

	want := int64(h.pointSize) * int64(h.Points)
	if int64(uncompressedLen) < want {
		return nil, apperr.New(apperr.E1002, nil,
			fmt.Sprintf("binary_compressed 尺寸不符：解压后 %d 字节，期望 %d", uncompressedLen, want))
	}
	raw := make([]byte, compressedLen)
	if _, err := io.ReadFull(r.br, raw); err != nil {
		return nil, apperr.New(apperr.E1002, err, "binary_compressed 数据读取失败")
	}
	out, err := lzfDecompress(raw, int(uncompressedLen))
	if err != nil {
		return nil, apperr.New(apperr.E1002, err, "LZF 解压失败")
	}
	// 转成点优先布局后复用 binary 解码
	uncompressed := make([]byte, 0, want)
	fieldOff := make([]int, len(h.Fields))
	off := 0
	for i := range h.Fields {
		fieldOff[i] = off
		off += h.Sizes[i] * h.Counts[i] * h.Points
	}
	stride := h.pointSize
	for p := 0; p < h.Points; p++ {
		rec := make([]byte, 0, stride)
		for i := range h.Fields {
			start := fieldOff[i] + p*h.Sizes[i]*h.Counts[i]
			rec = append(rec, out[start:start+h.Sizes[i]*h.Counts[i]]...)
		}
		uncompressed = append(uncompressed, rec...)
	}
	return r.decodeBinary(uncompressed, h.Points, h.pointSize), nil
}

// decodeBinary 解码点优先布局的二进制数据。stride 是单点字节数。
func (r *reader) decodeBinary(buf []byte, count, stride int) *Cloud {
	h := &r.header
	cloud := &Cloud{}
	cloud.Points = make([]Point, 0, count)
	// 预计算每个字段在单点记录内的偏移（count>1 的字段只取第一个值）
	off := 0
	fieldOffsets := make([]int, len(h.Fields))
	for i := range h.Fields {
		fieldOffsets[i] = off
		off += h.Sizes[i] * h.Counts[i]
	}
	for p := 0; p < count; p++ {
		rec := buf[p*stride : (p+1)*stride]
		var pt Point
		if idx, ok := h.fieldIndex["x"]; ok {
			pt.X = decodeFloat(rec[fieldOffsets[idx]:], h.Types[idx], h.Sizes[idx])
		}
		if idx, ok := h.fieldIndex["y"]; ok {
			pt.Y = decodeFloat(rec[fieldOffsets[idx]:], h.Types[idx], h.Sizes[idx])
		}
		if idx, ok := h.fieldIndex["z"]; ok {
			pt.Z = decodeFloat(rec[fieldOffsets[idx]:], h.Types[idx], h.Sizes[idx])
		}
		if idx, ok := h.fieldIndex["rgb"]; ok {
			pt.R, pt.G, pt.B, _ = decodePackedColor(rec[fieldOffsets[idx]:], h.Sizes[idx])
			pt.HasRGB = true
		} else if idx, ok := h.fieldIndex["rgba"]; ok {
			var a uint8
			pt.R, pt.G, pt.B, a = decodePackedColor(rec[fieldOffsets[idx]:], h.Sizes[idx])
			pt.HasRGB = true
			_ = a
		} else {
			rf, rok := h.fieldIndex["r"]
			gf, gok := h.fieldIndex["g"]
			bf, bok := h.fieldIndex["b"]
			if rok && gok && bok {
				pt.R = decodeByte(rec[fieldOffsets[rf]:], h.Types[rf], h.Sizes[rf])
				pt.G = decodeByte(rec[fieldOffsets[gf]:], h.Types[gf], h.Sizes[gf])
				pt.B = decodeByte(rec[fieldOffsets[bf]:], h.Types[bf], h.Sizes[bf])
				pt.HasRGB = true
			}
		}
		if idx, ok := h.fieldIndex["intensity"]; ok {
			v := decodeFloat(rec[fieldOffsets[idx]:], h.Types[idx], h.Sizes[idx])
			pt.Intensity = clampU16(v)
			pt.HasIntensity = true
		}
		if isValid(pt) {
			cloud.Points = append(cloud.Points, pt)
		} else {
			cloud.Skipped++
		}
	}
	return cloud
}

type asciiField struct {
	name string
	typ  byte
	size int
}

func (h *Header) expandedFields() []asciiField {
	var out []asciiField
	for i, f := range h.Fields {
		for c := 0; c < h.Counts[i]; c++ {
			out = append(out, asciiField{name: f, typ: h.Types[i], size: h.Sizes[i]})
		}
	}
	return out
}

func (h *Header) expandedCount() int {
	n := 0
	for i := range h.Fields {
		n += h.Counts[i]
	}
	return n
}

func buildPointFromASCII(fields []asciiField, vals []string) (Point, bool, error) {
	var pt Point
	rgbSet := [3]uint8{}
	hasRGB := false
	for i, f := range fields {
		v := vals[i]
		switch f.name {
		case "x":
			fl, err := strconv.ParseFloat(v, 64)
			if err != nil {
				return pt, false, apperr.New(apperr.E1002, err, "x 坐标无效："+v)
			}
			pt.X = fl
		case "y":
			fl, err := strconv.ParseFloat(v, 64)
			if err != nil {
				return pt, false, apperr.New(apperr.E1002, err, "y 坐标无效："+v)
			}
			pt.Y = fl
		case "z":
			fl, err := strconv.ParseFloat(v, 64)
			if err != nil {
				return pt, false, apperr.New(apperr.E1002, err, "z 坐标无效："+v)
			}
			pt.Z = fl
		case "rgb", "rgba":
			bits := parseASCIIBits(v)
			pt.R = uint8(bits >> 16 & 0xff)
			pt.G = uint8(bits >> 8 & 0xff)
			pt.B = uint8(bits & 0xff)
			pt.HasRGB = true
		case "r", "g", "b":
			n, err := strconv.ParseUint(v, 10, 32)
			if err == nil {
				b := decodeByteFromUint(n)
				if f.name == "r" {
					rgbSet[0] = b
				} else if f.name == "g" {
					rgbSet[1] = b
				} else {
					rgbSet[2] = b
				}
				hasRGB = true
			}
		case "intensity":
			fl, _ := strconv.ParseFloat(v, 64)
			pt.Intensity = clampU16(fl)
			pt.HasIntensity = true
		}
	}
	if hasRGB && !pt.HasRGB {
		pt.R, pt.G, pt.B = rgbSet[0], rgbSet[1], rgbSet[2]
		pt.HasRGB = true
	}
	return pt, isValid(pt), nil
}

// parseASCIIBits 把 rgb/rgba 的 ASCII 表示解析为 32 位打包值。
// PCL 把打包颜色写成一个 float 字符串，其 float32 位模式即颜色位。
func parseASCIIBits(v string) uint32 {
	if fl, err := strconv.ParseFloat(v, 64); err == nil {
		return math.Float32bits(float32(fl))
	}
	if n, err := strconv.ParseUint(v, 10, 64); err == nil {
		return uint32(n)
	}
	return 0
}

func isValid(p Point) bool {
	return !math.IsNaN(p.X) && !math.IsNaN(p.Y) && !math.IsNaN(p.Z) &&
		!math.IsInf(p.X, 0) && !math.IsInf(p.Y, 0) && !math.IsInf(p.Z, 0)
}

func clampU16(v float64) uint16 {
	if v < 0 {
		return 0
	}
	if v > 65535 {
		return 65535
	}
	return uint16(v)
}

// decodeFloat 按字段类型把字节解析为浮点数。
func decodeFloat(b []byte, typ byte, size int) float64 {
	switch typ {
	case 'F':
		switch size {
		case 4:
			return float64(math.Float32frombits(binary.LittleEndian.Uint32(b)))
		case 8:
			return math.Float64frombits(binary.LittleEndian.Uint64(b))
		}
	default:
		u := decodeUint(b, size)
		if typ == 'I' {
			return float64(int64(u))
		}
		return float64(u)
	}
	return 0
}

func decodeUint(b []byte, size int) uint64 {
	switch size {
	case 1:
		return uint64(b[0])
	case 2:
		return uint64(binary.LittleEndian.Uint16(b))
	case 4:
		return uint64(binary.LittleEndian.Uint32(b))
	case 8:
		return binary.LittleEndian.Uint64(b)
	}
	return 0
}

// decodePackedColor 解析 rgb/rgba 打包颜色（PCL 位序 0xRRGGBB[AA]）。
func decodePackedColor(b []byte, size int) (r, g, bl, a uint8) {
	if size < 3 {
		return 0, 0, 0, 255
	}
	var v uint32
	if size >= 4 {
		v = binary.LittleEndian.Uint32(b)
		a = uint8(v >> 24 & 0xff)
	} else {
		v = uint32(binary.LittleEndian.Uint16(b)) | uint32(b[2])<<16
		a = 255
	}
	return uint8(v >> 16 & 0xff), uint8(v >> 8 & 0xff), uint8(v & 0xff), a
}

// decodeByte 把数值字段解析为 0~255 的颜色分量（16 位量程自动右移）。
func decodeByte(b []byte, typ byte, size int) uint8 {
	return decodeByteFromUint(decodeUint(b, size))
}

func decodeByteFromUint(n uint64) uint8 {
	if n > 255 {
		return uint8(n >> 8 & 0xff)
	}
	return uint8(n)
}
