package pipeline

import (
	"encoding/json"
	"os"

	"github.com/LinuxSuRen/pointcloud-twin/internal/apperr"
	"github.com/LinuxSuRen/pointcloud-twin/internal/crs"
)

// Report 是输出目录中的转换报告（report.json），
// 记录 CRS、点数、包围盒与关键参数，供验收与叠加校验。
type Report struct {
	Tool        string  `json:"tool"`
	Version     string  `json:"version"`
	Input       string  `json:"input"`
	GeneratedAt string  `json:"generatedAt"`
	Voxel       float64 `json:"voxel,omitempty"`
	Points      struct {
		Input   int `json:"input"`
		Tiled   int `json:"tiled"`
		Skipped int `json:"skipped"`
	} `json:"points"`
	LasPath string `json:"lasPath,omitempty"`
	CRS     struct {
		Type   string        `json:"type"`
		Origin crs.ENUOrigin `json:"origin"`
	} `json:"crs"`
	Bounds struct {
		Min [3]float64 `json:"min"`
		Max [3]float64 `json:"max"`
	} `json:"bounds"`
	Tiles struct {
		Count              int     `json:"count"`
		Version            string  `json:"version"`
		Refine             string  `json:"refine"`
		PointsPerTile      int     `json:"pointsPerTile"`
		RootGeometricError float64 `json:"rootGeometricError"`
		TreeDepth          int     `json:"treeDepth"`
		Planes             int     `json:"planes"`
	} `json:"tiles"`
	// Planes 是平面拟合分割统计（未启用时缺省）。
	Planes *ReportPlanes `json:"planes,omitempty"`
}

// ReportPlanes 是平面分割的转换报告段。
type ReportPlanes struct {
	Enabled  bool    `json:"enabled"`
	Dist     float64 `json:"dist"`
	Count    int     `json:"count"`
	Coverage float64 `json:"coveragePercent"`
}

func writeReport(path string, rep *Report) error {
	data, err := json.MarshalIndent(rep, "", "  ")
	if err != nil {
		return apperr.New(apperr.E5001, err, "报告序列化失败")
	}
	if err := os.WriteFile(path, append(data, '\n'), 0o644); err != nil {
		return apperr.New(apperr.E2002, err, path)
	}
	return nil
}
