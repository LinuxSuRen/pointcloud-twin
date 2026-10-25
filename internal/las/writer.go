// Package las 写出标准 LAS 1.4 点云文件（点记录格式 7），
// 作为 PCD 与 3D Tiles 生态（gocesiumtiler、PDAL、PCL 等）之间的桥接格式。
//
// CRS 记录方式（满足需求的 R2.2）：
//   - LAS 1.4 全局编码 bit4（WKT）置位，LASF_Projection recordID=2112 VLR 写入 WKT2 工程 CRS；
//   - 另写一个自定义 VLR（pointcloud-twin / recordID=42），以 JSON 记录 ENU 原点，供本工具链回读。
package las

import (
	"encoding/binary"
	"io"
	"math"
	"os"
	"path/filepath"

	"github.com/LinuxSuRen/pointcloud-twin/internal/apperr"
	"github.com/LinuxSuRen/pointcloud-twin/internal/crs"
	"github.com/LinuxSuRen/pointcloud-twin/internal/pcd"
)

const (
	lasVersionMajor = 1
	lasVersionMinor = 4
	headerSize      = 375
	pointFormat7Len = 36
	scaleFactor     = 0.001 // 毫米精度
	wktRecordID     = 2112
	enuRecordID     = 42
	enuUserID       = "pointcloud-twin"
	projectionUser  = "LASF_Projection"
)

// WriteOptions 控制 LAS 写出。
type WriteOptions struct {
	// Origin 是 ENU 原点（缺省时写 0,0,0 的局部假设）。
	Origin crs.ENUOrigin
	// GeneratingSoftware 记录生成软件标识。
	GeneratingSoftware string
}

// Write 把点云写成 LAS 1.4（PDR 7）文件。
func Write(path string, cloud *pcd.Cloud, opt WriteOptions) error {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return apperr.New(apperr.E2002, err, dir)
	}
	f, err := os.Create(path)
	if err != nil {
		return apperr.New(apperr.E2001, err, path)
	}
	defer f.Close()

	w := &writer{f: f, opt: opt, cloud: cloud}
	return w.write()
}

type writer struct {
	f     *os.File
	opt   WriteOptions
	cloud *pcd.Cloud
}

func (w *writer) write() error {
	points := w.cloud.Points
	offX, offY, offZ := w.cloud.Min[0], w.cloud.Min[1], w.cloud.Min[2]

	wkt := w.opt.Origin.WKT()
	enuJSON := w.opt.Origin.JSONPayload()
	vlrs := []vlr{
		{userID: projectionUser, recordID: wktRecordID, desc: "OGC WKT CRS", payload: []byte(wkt)},
		{userID: enuUserID, recordID: enuRecordID, desc: "pointcloud-twin ENU origin", payload: []byte(enuJSON)},
	}
	vlrBytes := 0
	for _, v := range vlrs {
		vlrBytes += 54 + len(v.payload)
	}
	offsetToPointData := headerSize + vlrBytes

	hdr := make([]byte, headerSize)
	copy(hdr[0:4], "LASF")
	hdr[24] = lasVersionMajor
	hdr[25] = lasVersionMinor
	copy(hdr[26:58], "pointcloud-twin")
	software := w.opt.GeneratingSoftware
	if len(software) > 32 {
		software = software[:32]
	}
	copy(hdr[58:90], software)
	binary.LittleEndian.PutUint16(hdr[90:92], uint16(yday()))
	binary.LittleEndian.PutUint16(hdr[92:94], uint16(year()))
	binary.LittleEndian.PutUint16(hdr[94:96], headerSize)
	binary.LittleEndian.PutUint32(hdr[96:100], uint32(offsetToPointData))
	binary.LittleEndian.PutUint32(hdr[100:104], uint32(len(vlrs)))
	// 全局编码 bit4：WKT CRS
	binary.LittleEndian.PutUint16(hdr[6:8], 0x0010)
	hdr[104] = 7 // 点记录格式
	binary.LittleEndian.PutUint16(hdr[105:107], pointFormat7Len)
	// 旧版字段（兼容旧读取器）：超过 2^32-1 时写 0
	if uint64(len(points)) <= math.MaxUint32 {
		binary.LittleEndian.PutUint32(hdr[107:111], uint32(len(points)))
		binary.LittleEndian.PutUint32(hdr[111:115], uint32(len(points)))
	}
	putF64 := func(off int, v float64) { binary.LittleEndian.PutUint64(hdr[off:off+8], math.Float64bits(v)) }
	putF64(131, scaleFactor) // X/Y/Z 同一精度
	putF64(139, scaleFactor)
	putF64(147, scaleFactor)
	putF64(155, offX)
	putF64(163, offY)
	putF64(171, offZ)
	putF64(179, w.cloud.Max[0])
	putF64(187, w.cloud.Min[0])
	putF64(195, w.cloud.Max[1])
	putF64(203, w.cloud.Min[1])
	putF64(211, w.cloud.Max[2])
	putF64(219, w.cloud.Min[2])
	binary.LittleEndian.PutUint64(hdr[247:255], uint64(len(points))) // 1.4 点数（长整型）

	if _, err := w.f.Write(hdr); err != nil {
		return apperr.New(apperr.E2001, err, "写文件头失败")
	}
	for _, v := range vlrs {
		if err := writeVLR(w.f, v); err != nil {
			return apperr.New(apperr.E2001, err, "写 VLR 失败")
		}
	}

	// 点数据
	rec := make([]byte, pointFormat7Len)
	rec[16] = 1 // classification: unclassified
	buf := make([]byte, 0, 64*1024)
	toI32 := func(v float64, off float64) int32 {
		return int32(math.Round((v - off) / scaleFactor))
	}
	for _, p := range points {
		for i := range rec {
			rec[i] = 0
		}
		rec[16] = 1
		binary.LittleEndian.PutUint32(rec[0:4], uint32(toI32(p.X, offX)))
		binary.LittleEndian.PutUint32(rec[4:8], uint32(toI32(p.Y, offY)))
		binary.LittleEndian.PutUint32(rec[8:12], uint32(toI32(p.Z, offZ)))
		if p.HasIntensity {
			binary.LittleEndian.PutUint16(rec[12:14], p.Intensity)
		}
		if p.HasRGB {
			rec[30], rec[31], rec[32] = p.R, p.G, p.B
			rec[33] = 255
		}
		buf = append(buf, rec...)
		if len(buf) >= 64*1024 {
			if _, err := w.f.Write(buf); err != nil {
				return apperr.New(apperr.E2001, err, "写点数据失败")
			}
			buf = buf[:0]
		}
	}
	if len(buf) > 0 {
		if _, err := w.f.Write(buf); err != nil {
			return apperr.New(apperr.E2001, err, "写点数据失败")
		}
	}
	return nil
}

type vlr struct {
	userID   string
	recordID uint16
	desc     string
	payload  []byte
}

func writeVLR(f io.Writer, v vlr) error {
	h := make([]byte, 54)
	binary.LittleEndian.PutUint16(h[0:2], 0xAABB) // reserved
	uid := v.userID
	if len(uid) > 16 {
		uid = uid[:16]
	}
	copy(h[2:18], uid)
	binary.LittleEndian.PutUint16(h[18:20], v.recordID)
	binary.LittleEndian.PutUint16(h[20:22], uint16(len(v.payload)))
	desc := v.desc
	if len(desc) > 32 {
		desc = desc[:32]
	}
	copy(h[22:54], desc)
	if _, err := f.Write(h); err != nil {
		return err
	}
	_, err := f.Write(v.payload)
	return err
}
