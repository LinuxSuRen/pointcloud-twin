// Package crs 定义本项目使用的坐标系描述：局部 ENU（东北天）原点。
// ENU 原点是「人类可读的空间锚点」，写入 LAS VLR、tileset 变换与设备代理体配置，
// 保证整条链路在同一地理位置上叠加。
package crs

import (
	"fmt"
	"math"
	"strconv"
	"strings"

	"github.com/LinuxSuRen/pointcloud-twin/internal/apperr"
)

// ENUOrigin 是 WGS84 大地坐标表示的 ENU 原点。Lon/Lat 单位为度，H 为椭球高（米）。
type ENUOrigin struct {
	Lon float64 `json:"lon"`
	Lat float64 `json:"lat"`
	H   float64 `json:"h"`
	// Explicit 表示用户显式指定了原点；false 表示缺省的局部坐标假设。
	Explicit bool `json:"explicit"`
}

// String 返回人类可读描述。
func (o ENUOrigin) String() string {
	state := "假设"
	if o.Explicit {
		state = "显式"
	}
	return fmt.Sprintf("%s ENU 原点 (lon=%.6f, lat=%.6f, h=%.3f)", state, o.Lon, o.Lat, o.H)
}

// ParseENUOrigin 解析 "lon,lat,h" 形式的 ENU 原点（度；h 可省略，默认 0）。
func ParseENUOrigin(spec string) (ENUOrigin, error) {
	var o ENUOrigin
	parts := strings.Split(strings.TrimSpace(spec), ",")
	if len(parts) < 2 || len(parts) > 3 {
		return o, apperr.New(apperr.E3001, nil,
			"格式应为 lon,lat,h（度），实际输入："+spec)
	}
	var vals [3]float64
	for i, p := range parts {
		v, err := strconv.ParseFloat(strings.TrimSpace(p), 64)
		if err != nil || math.IsNaN(v) || math.IsInf(v, 0) {
			return o, apperr.New(apperr.E3001, err, "数值无效："+p)
		}
		vals[i] = v
	}
	o.Lon, o.Lat, o.H = vals[0], vals[1], vals[2]
	if o.Lon < -180 || o.Lon > 180 || o.Lat < -90 || o.Lat > 90 {
		return o, apperr.New(apperr.E3001, nil,
			fmt.Sprintf("经纬度超出范围：lon=%v lat=%v", o.Lon, o.Lat))
	}
	o.Explicit = true
	return o, nil
}

// WKT 返回描述该局部 ENU 坐标系的 WKT2 工程 CRS 字符串（写入 LAS VLR 2112）。
func (o ENUOrigin) WKT() string {
	kind := "assumed"
	if o.Explicit {
		kind = "explicit"
	}
	name := fmt.Sprintf("pointcloud-twin local ENU origin (lon=%.8f, lat=%.8f, h=%.4f)", o.Lon, o.Lat, o.H)
	return fmt.Sprintf(`ENGCRS["%s",
  EDATUM["pointcloud-twin engineering datum",
    ANCHOR["ENU origin, %s",
      PARAMETER["latitude of topocentric origin",%.8f],
      PARAMETER["longitude of topocentric origin",%.8f],
      PARAMETER["ellipsoidal height of topocentric origin",%.4f]]],
  CS[Cartesian,3],
    AXIS["topocentric East (U)",east,ORDER[1],LENGTHUNIT["metre",1]],
    AXIS["topocentric North (V)",north,ORDER[2],LENGTHUNIT["metre",1]],
    AXIS["topocentric height (W)",up,ORDER[3],LENGTHUNIT["metre",1]]]`,
		name, kind, o.Lat, o.Lon, o.H)
}

// JSONPayload 返回自定义 VLR 的 JSON 载荷，供工具链回读 ENU 原点。
func (o ENUOrigin) JSONPayload() string {
	return fmt.Sprintf(`{"type":"enu","lon":%.8f,"lat":%.8f,"h":%.4f,"explicit":%t}`,
		o.Lon, o.Lat, o.H, o.Explicit)
}
