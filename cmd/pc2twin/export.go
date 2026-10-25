package main

import (
	"github.com/spf13/cobra"

	"github.com/LinuxSuRen/pointcloud-twin/internal/export"
	"github.com/LinuxSuRen/pointcloud-twin/internal/logx"
	"github.com/LinuxSuRen/pointcloud-twin/internal/pcd"
	"github.com/LinuxSuRen/pointcloud-twin/internal/proxy"
)

func newExportCmd() *cobra.Command {
	var (
		input   string
		devices string
		output  string
		voxel   float64
	)
	cmd := &cobra.Command{
		Use:   "export",
		Short: "导出单文件 GLB 数字孪生模型（点云 + 设备代理体），可导入 UE/Unity/Blender 等",
		Long: "export 把 PCD 点云与设备代理体配置组装为一个 glTF 2.0 二进制文件（GLB）。\n" +
			"坐标为本地 ENU（米），根节点携带 z-up→y-up 变换，导入通用引擎后方向正确。\n" +
			"大点云建议用 --voxel 降采样控制单文件体积。",
		RunE: func(cmd *cobra.Command, args []string) error {
			logLevel, _ := cmd.Flags().GetString("log-level")
			log := logx.New(logLevel)

			cloud, err := pcd.Load(input)
			if err != nil {
				log.Debug("PCD 读取失败", "err", err.Error())
				return err
			}
			var cfg *proxy.Config
			if devices != "" {
				cfg, err = proxy.Load(devices)
				if err != nil {
					return err
				}
			}
			var deviceList []proxy.Device
			if cfg != nil {
				deviceList = cfg.Devices
			}
			data, err := export.BuildGLB(cloud, deviceList, export.GLBOptions{Voxel: voxel})
			if err != nil {
				log.Debug("GLB 生成失败", "err", err.Error())
				return err
			}
			if err := writeFileEnsureDir(output, data); err != nil {
				return err
			}
			log.Info("GLB 导出完成", "path", output, "points", len(cloud.Points), "devices", len(deviceList))
			return nil
		},
	}
	cmd.Flags().StringVarP(&input, "input", "i", "", "输入 PCD 文件路径（必填）")
	cmd.Flags().StringVar(&devices, "devices", "", "设备代理体配置（JSON，可选）")
	cmd.Flags().StringVarP(&output, "output", "o", "", "输出 GLB 文件路径（必填）")
	cmd.Flags().Float64Var(&voxel, "voxel", 0, "导出时体素降采样边长（米）；0 表示不降采样")
	_ = cmd.MarkFlagRequired("input")
	_ = cmd.MarkFlagRequired("output")
	return cmd
}
