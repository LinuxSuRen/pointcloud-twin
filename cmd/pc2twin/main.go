// pc2twin 把点云（PCD）转换为数字孪生可用资产：
// 3D Tiles 1.1 点云底图 + 设备代理体配置，供 Cesium 查看器加载并接入实时数据。
package main

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"
)

// version 由构建注入（goreleaser -X main.version）。
var version = "dev"

func newRootCmd() *cobra.Command {
	root := &cobra.Command{
		Use:   "pc2twin",
		Short: "把点云（PCD）转换为数字孪生可用资产：3D Tiles 底图 + 设备代理体配置",
		Long: "pc2twin 把现场三维扫描得到的 PCD 点云转换为 Web 端可高效加载的 3D Tiles 底图，\n" +
			"并输出设备代理体配置（设备 ID + 包围盒），供前端把 SCADA/BMS/PCS 实时状态映射到设备上。",
		SilenceErrors: true,
		SilenceUsage:  true,
	}
	root.PersistentFlags().String("log-level", "info", "日志级别：debug | info | warn | error")
	root.AddCommand(
		newConvertCmd(),
		newPCD2LASCmd(),
		newServeCmd(),
		newVersionCmd(),
	)
	return root
}

func main() {
	if err := newRootCmd().Execute(); err != nil {
		fmt.Fprintf(os.Stderr, "错误：%s\n", err.Error())
		fmt.Fprintln(os.Stderr, "提示：使用 --log-level debug 可查看技术详情。")
		os.Exit(1)
	}
}
