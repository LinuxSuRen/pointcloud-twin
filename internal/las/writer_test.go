package las

import (
	"encoding/binary"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/LinuxSuRen/pointcloud-twin/internal/crs"
	"github.com/LinuxSuRen/pointcloud-twin/internal/pcd"
)

func testCloud() *pcd.Cloud {
	points := []pcd.Point{
		{X: 1.5, Y: -2.25, Z: 3.125, Intensity: 100, R: 255, G: 128, B: 0, HasRGB: true, HasIntensity: true},
		{X: 100.25, Y: 200.5, Z: -50.75, Intensity: 42, R: 0, G: 255, B: 255, HasRGB: true, HasIntensity: true},
	}
	return &pcd.Cloud{Points: points, Min: [3]float64{1.5, -2.25, -50.75}, Max: [3]float64{100.25, 200.5, 3.125}}
}

func TestWriteAndVerify(t *testing.T) {
	path := filepath.Join(t.TempDir(), "out.las")
	origin := crs.ENUOrigin{Lon: 121.47, Lat: 31.23, H: 10, Explicit: true}
	if err := Write(path, testCloud(), WriteOptions{Origin: origin, GeneratingSoftware: "unit-test"}); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}

	if string(data[0:4]) != "LASF" {
		t.Fatalf("magic = %q", data[0:4])
	}
	if data[24] != 1 || data[25] != 4 {
		t.Fatalf("版本 = %d.%d", data[24], data[25])
	}
	if hs := binary.LittleEndian.Uint16(data[94:96]); hs != headerSize {
		t.Fatalf("header size = %d", hs)
	}
	if enc := binary.LittleEndian.Uint16(data[6:8]); enc&0x10 == 0 {
		t.Fatal("全局编码未置 WKT 位")
	}
	if data[104] != 7 {
		t.Fatalf("点格式 = %d", data[104])
	}
	if prl := binary.LittleEndian.Uint16(data[105:107]); prl != pointFormat7Len {
		t.Fatalf("点记录长度 = %d", prl)
	}
	if n := binary.LittleEndian.Uint64(data[247:255]); n != 2 {
		t.Fatalf("点数 = %d", n)
	}

	// VLR
	vlrCount := binary.LittleEndian.Uint32(data[100:104])
	offset := headerSize
	var sawWKT, sawENU bool
	for i := uint32(0); i < vlrCount; i++ {
		uid := strings.TrimRight(string(data[offset+2:offset+18]), "\x00")
		rid := binary.LittleEndian.Uint16(data[offset+18 : offset+20])
		plen := binary.LittleEndian.Uint16(data[offset+20 : offset+22])
		payload := string(data[offset+54 : offset+54+int(plen)])
		if uid == "LASF_Projection" && rid == wktRecordID {
			sawWKT = true
			if !strings.Contains(payload, "ENGCRS") || !strings.Contains(payload, "121.47") {
				t.Fatalf("WKT 内容异常：%q", payload)
			}
		}
		if uid == enuUserID && rid == enuRecordID {
			sawENU = true
			if !strings.Contains(payload, `"lon":121.47`) {
				t.Fatalf("ENU VLR 内容异常：%q", payload)
			}
		}
		offset += 54 + int(plen)
	}
	if !sawWKT || !sawENU {
		t.Fatalf("VLR 缺失：WKT=%v ENU=%v", sawWKT, sawENU)
	}

	// 点数据偏移正确 + 首点坐标
	pointOff := int(binary.LittleEndian.Uint32(data[96:100]))
	if pointOff != offset {
		t.Fatalf("点数据偏移 = %d，期望 %d", pointOff, offset)
	}
	scale := math64bits(data[131:139])
	offX := math64bits(data[155:163])
	ix := int32(binary.LittleEndian.Uint32(data[pointOff : pointOff+4]))
	wantX := int32(round((1.5 - offX) / scale))
	if ix != wantX {
		t.Fatalf("X = %d，期望 %d", ix, wantX)
	}
	// RGB
	if data[pointOff+30] != 255 || data[pointOff+31] != 128 || data[pointOff+32] != 0 {
		t.Fatalf("RGB 解析错误：%v", data[pointOff+30:pointOff+34])
	}
}

func math64bits(b []byte) float64 {
	return float64FromBits(binary.LittleEndian.Uint64(b))
}
