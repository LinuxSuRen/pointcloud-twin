package proxy

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/LinuxSuRen/pointcloud-twin/internal/apperr"
	"github.com/LinuxSuRen/pointcloud-twin/internal/crs"
)

func validConfig() *Config {
	return &Config{
		Origin: &crs.ENUOrigin{Lon: 121.47, Lat: 31.23, Explicit: true},
		Devices: []Device{
			{DeviceID: "battery-cluster-01", DeviceType: "battery-cluster", Center: [3]float64{0, 0, 1.5}, Size: [3]float64{12, 2.5, 3}, Heading: 0},
			{DeviceID: "pcs-01", DeviceType: "pcs", Center: [3]float64{15, 0, 1.2}, Size: [3]float64{3, 2, 2.4}, Heading: 90},
		},
	}
}

func TestValidateOK(t *testing.T) {
	if err := validConfig().Validate(); err != nil {
		t.Fatal(err)
	}
}

func TestValidateErrors(t *testing.T) {
	cases := []func(*Config){
		func(c *Config) { c.Devices[0].DeviceID = "" },
		func(c *Config) { c.Devices[1].DeviceID = c.Devices[0].DeviceID },
		func(c *Config) { c.Devices[0].DeviceType = "" },
		func(c *Config) { c.Devices[0].Size = [3]float64{1, -1, 1} },
		func(c *Config) { c.Devices = nil },
	}
	for i, mutate := range cases {
		c := validConfig()
		mutate(c)
		err := c.Validate()
		if err == nil {
			t.Fatalf("用例 %d 应报错", i)
		}
		if _, ok := apperr.From(err); !ok {
			t.Fatalf("用例 %d 应带稳定错误码", i)
		}
	}
}

func TestLoadSaveRoundTrip(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "devices.json")
	c := validConfig()
	if err := Save(path, c); err != nil {
		t.Fatal(err)
	}
	got, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(got.Devices) != 2 || got.Devices[0].DeviceID != "battery-cluster-01" {
		t.Fatalf("回读错误：%+v", got)
	}
	if got.Origin == nil || got.Origin.Lon != 121.47 {
		t.Fatalf("origin 回读错误：%+v", got.Origin)
	}

	// 非法 JSON
	bad := filepath.Join(dir, "bad.json")
	os.WriteFile(bad, []byte("{not json"), 0o644)
	if _, err := Load(bad); err == nil {
		t.Fatal("应报解析错误")
	}
}

func TestNormalizeHeading(t *testing.T) {
	if NormalizeHeading(-90) != 270 || NormalizeHeading(450) != 90 || NormalizeHeading(360) != 0 {
		t.Fatal("朝向归一化错误")
	}
}

func TestJSONFieldNames(t *testing.T) {
	// 契约字段名与设计文档保持一致（camelCase）
	data, _ := json.Marshal(validConfig())
	for _, key := range []string{"deviceId", "deviceType", "center", "size", "heading"} {
		if !contains(string(data), key) {
			t.Fatalf("缺少字段 %s：%s", key, data)
		}
	}
}

func contains(s, sub string) bool {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}
