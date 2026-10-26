package planes

import (
	"encoding/json"
	"fmt"
	"os"
	"strconv"
	"strings"

	"github.com/LinuxSuRen/pointcloud-twin/internal/apperr"
)

// ParseOptions 解析 k=v 逗号串：
// dist=0.03,min=500,max=100,grid=0.1,tol=0.2,trials=128,seed=1
// 空串返回默认值且 enabled=false 由调用方判断。
func ParseOptions(spec string) (Options, error) {
	var opt Options
	if strings.TrimSpace(spec) == "" {
		opt.fillDefaults()
		return opt, nil
	}
	for _, kv := range strings.Split(spec, ",") {
		kv = strings.TrimSpace(kv)
		if kv == "" {
			continue
		}
		parts := strings.SplitN(kv, "=", 2)
		if len(parts) != 2 {
			return opt, fmt.Errorf("参数应为 k=v 形式，实际：%s", kv)
		}
		key := strings.TrimSpace(parts[0])
		val := strings.TrimSpace(parts[1])
		num, err := strconv.ParseFloat(val, 64)
		if err != nil {
			return opt, fmt.Errorf("参数 %s 的值无效：%s", key, val)
		}
		switch key {
		case "dist":
			opt.Dist = num
		case "min":
			opt.MinPoints = int(num)
		case "max":
			opt.MaxPlanes = int(num)
		case "grid":
			opt.Grid = num
		case "tol":
			opt.Simplify = num
		case "trials":
			opt.Trials = int(num)
		case "seed":
			opt.Seed = int64(num)
		default:
			return opt, fmt.Errorf("未知参数：%s（可用 dist/min/max/grid/tol/trials/seed）", key)
		}
	}
	opt.fillDefaults()
	return opt, nil
}

// file 是 planes.json 的结构（dist 供导出侧复算残余点使用）。
type file struct {
	Dist   float64 `json:"dist"`
	Planes []Plane `json:"planes"`
}

// Save 把平面结果写成 JSON 文件。
func Save(path string, dist float64, ps []Plane) error {
	data, err := json.MarshalIndent(file{Dist: dist, Planes: ps}, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, append(data, '\n'), 0o644)
}

// Load 读取平面结果文件。
func Load(path string) (dist float64, ps []Plane, err error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return 0, nil, apperr.New(apperr.E4001, err, path)
	}
	var f file
	if err := json.Unmarshal(data, &f); err != nil {
		return 0, nil, apperr.New(apperr.E4001, err, path)
	}
	return f.Dist, f.Planes, nil
}
