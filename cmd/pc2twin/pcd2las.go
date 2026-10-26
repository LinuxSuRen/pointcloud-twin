package main

import (
	"github.com/spf13/cobra"

	"github.com/LinuxSuRen/pointcloud-twin/internal/crs"
	"github.com/LinuxSuRen/pointcloud-twin/internal/logx"
	"github.com/LinuxSuRen/pointcloud-twin/internal/pipeline"
)

func newPCD2LASCmd() *cobra.Command {
	var (
		input  string
		output string
		crsEnu string
		voxel  float64
	)
	cmd := &cobra.Command{
		Use:   "pcd2las",
		Short: "把 PCD 点云转换为 LAS 1.4 文件（含 CRS 信息）",
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

			opt := pipeline.PCD2LASOptions{
				Input:       input,
				Output:      output,
				Origin:      origin,
				Voxel:       voxel,
				Log:         log,
				ToolVersion: version,
			}
			if err := pipeline.PCD2LAS(opt); err != nil {
				log.Debug("转换失败", "err", err.Error())
				return err
			}
			return nil
		},
	}
	cmd.Flags().StringVarP(&input, "input", "i", "", "输入 PCD 文件路径（必填）")
	cmd.Flags().StringVarP(&output, "output", "o", "", "输出 LAS 文件路径（必填）")
	cmd.Flags().StringVar(&crsEnu, "crs-enu", "", "局部 ENU 原点，格式 lon,lat,h（度，米；h 可省略）")
	cmd.Flags().Float64Var(&voxel, "voxel", 0, "体素降采样边长（米）；0 表示不降采样")
	_ = cmd.MarkFlagRequired("input")
	_ = cmd.MarkFlagRequired("output")
	return cmd
}
