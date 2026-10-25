package main

import (
	"github.com/spf13/cobra"

	"github.com/LinuxSuRen/pointcloud-twin/internal/crs"
	"github.com/LinuxSuRen/pointcloud-twin/internal/logx"
	"github.com/LinuxSuRen/pointcloud-twin/internal/pipeline"
)

func newConvertCmd() *cobra.Command {
	var (
		input         string
		output        string
		crsEnu        string
		voxel         float64
		pointsPerTile int
		refine        string
		tilesVersion  string
		devices       string
		skipLAS       bool
	)
	cmd := &cobra.Command{
		Use:   "convert",
		Short: "把 PCD 点云转换为 3D Tiles 底图（含 LAS 桥接文件与转换报告）",
		RunE: func(cmd *cobra.Command, args []string) error {
			logLevel, _ := cmd.Flags().GetString("log-level")
			log := logx.New(logLevel)

			origin := crs.ENUOrigin{}
			if crsEnu != "" {
				o, err := crs.ParseENUOrigin(crsEnu)
				if err != nil {
					return err
				}
				origin = o
			}

			opt := pipeline.ConvertOptions{
				Input:         input,
				OutputDir:     output,
				Origin:        origin,
				Voxel:         voxel,
				PointsPerTile: pointsPerTile,
				Refine:        refine,
				TilesVersion:  tilesVersion,
				DevicesFile:   devices,
				SkipLAS:       skipLAS,
				Log:           log,
				ToolVersion:   version,
			}
			if err := pipeline.Convert(opt); err != nil {
				log.Debug("转换失败", "err", err.Error())
				return err
			}
			return nil
		},
	}
	cmd.Flags().StringVarP(&input, "input", "i", "", "输入 PCD 文件路径（必填）")
	cmd.Flags().StringVarP(&output, "output", "o", "", "输出目录（必填）")
	cmd.Flags().StringVar(&crsEnu, "crs-enu", "", "局部 ENU 原点，格式 lon,lat,h（度，米；h 可省略）。缺省按局部坐标假设处理")
	cmd.Flags().Float64Var(&voxel, "voxel", 0, "体素降采样边长（米）；0 表示不降采样")
	cmd.Flags().IntVar(&pointsPerTile, "points-per-tile", 50000, "单瓦片目标点数")
	cmd.Flags().StringVar(&refine, "refine", "add", "LOD 细化模式：add | replace")
	cmd.Flags().StringVar(&tilesVersion, "tiles-version", "1.1", "3D Tiles 版本：1.1（glb，默认）| 1.0（pnts）")
	cmd.Flags().StringVar(&devices, "devices", "", "设备代理体配置文件（JSON），校验后拷贝到输出目录")
	cmd.Flags().BoolVar(&skipLAS, "skip-las", false, "不写中间 LAS 文件")
	_ = cmd.MarkFlagRequired("input")
	_ = cmd.MarkFlagRequired("output")
	return cmd
}
