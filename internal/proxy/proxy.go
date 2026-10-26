// Package proxy 实现设备代理体配置的读取、校验与写出。
//
// 配置记录设备在点云局部 ENU 坐标系中的中心、包围盒尺寸与朝向；
// deviceId 是稳定的人类可读标识，作为前端与实时数据流的映射键。
package proxy

import (
	"encoding/json"
	"fmt"
	"math"
	"os"

	"github.com/LinuxSuRen/pointcloud-twin/internal/apperr"
	"github.com/LinuxSuRen/pointcloud-twin/internal/crs"
)

// Device 是单台设备的代理体描述。
type Device struct {
	// DeviceID 是稳定的人类可读标识，如 battery-cluster-01。
	DeviceID string `json:"deviceId"`
	// DeviceType 是设备类型标识，如 battery-cluster / pcs / transformer。
	DeviceType string `json:"deviceType"`
	// Center 是包围盒中心（点云局部 ENU 坐标，米）。
	Center [3]float64 `json:"center"`
	// Size 是包围盒尺寸（米）。
	Size [3]float64 `json:"size"`
	// Heading 是绕 +Z（天向）的朝向角（度，0 = +X/东，顺时针）。
	Heading float64 `json:"heading"`
}

// Config 是代理体配置文件（devices.json）。
type Config struct {
	// Origin 是该配置使用的 ENU 原点（由转换工具写入，查看器据此与瓦片集对齐）。
	Origin  *crs.ENUOrigin `json:"origin,omitempty"`
	Devices []Device       `json:"devices"`
}

// Load 从 JSON 文件读取配置。
func Load(path string) (*Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, apperr.New(apperr.E4001, err, path)
	}
	return LoadFromBytes(data)
}

// LoadFromBytes 从 JSON 字节读取配置。
func LoadFromBytes(data []byte) (*Config, error) {
	var c Config
	if err := json.Unmarshal(data, &c); err != nil {
		return nil, apperr.New(apperr.E4001, err, "配置解析失败")
	}
	return &c, nil
}

// Save 把配置写成 JSON 文件。
func Save(path string, c *Config) error {
	data, err := json.MarshalIndent(c, "", "  ")
	if err != nil {
		return apperr.New(apperr.E4002, err, "配置序列化失败")
	}
	if err := os.WriteFile(path, append(data, '\n'), 0o644); err != nil {
		return apperr.New(apperr.E2002, err, path)
	}
	return nil
}

// Validate 校验配置合法性。
func (c *Config) Validate() error {
	if len(c.Devices) == 0 {
		return apperr.New(apperr.E4002, nil, "devices 为空")
	}
	ids := make(map[string]struct{}, len(c.Devices))
	for i, d := range c.Devices {
		if d.DeviceID == "" {
			return apperr.New(apperr.E4002, nil, fmt.Sprintf("第 %d 台设备缺少 deviceId", i+1))
		}
		if _, dup := ids[d.DeviceID]; dup {
			return apperr.New(apperr.E4002, nil, "deviceId 重复："+d.DeviceID)
		}
		ids[d.DeviceID] = struct{}{}
		if d.DeviceType == "" {
			return apperr.New(apperr.E4002, nil, "设备 "+d.DeviceID+" 缺少 deviceType")
		}
		for j, v := range d.Center {
			if math.IsNaN(v) || math.IsInf(v, 0) {
				return apperr.New(apperr.E4002, nil, "设备 "+d.DeviceID+" 中心坐标无效")
			}
			_ = j
		}
		for j, v := range d.Size {
			if !(v > 0) || math.IsInf(v, 0) {
				return apperr.New(apperr.E4002, nil,
					fmt.Sprintf("设备 %s 的尺寸必须为正数（第 %d 维=%.3f）", d.DeviceID, j+1, v))
			}
		}
		if math.IsNaN(d.Heading) || math.IsInf(d.Heading, 0) {
			return apperr.New(apperr.E4002, nil, "设备 "+d.DeviceID+" 朝向无效")
		}
	}
	return nil
}

// NormalizeHeading 把朝向角归一化到 [0, 360)。
func NormalizeHeading(deg float64) float64 {
	d := math.Mod(deg, 360)
	if d < 0 {
		d += 360
	}
	return d
}
