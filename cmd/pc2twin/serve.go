package main

import (
	"github.com/spf13/cobra"

	"github.com/LinuxSuRen/pointcloud-twin/internal/logx"
	"github.com/LinuxSuRen/pointcloud-twin/internal/server"
)

func newServeCmd() *cobra.Command {
	var (
		addr      string
		dataDir   string
		chunkMB   string
		maxUpload string
	)
	cmd := &cobra.Command{
		Use:   "serve",
		Short: "启动 Web 服务：分片上传 PCD、异步转换（SSE 进度）、托管产物供查看器加载",
		RunE: func(cmd *cobra.Command, args []string) error {
			logLevel, _ := cmd.Flags().GetString("log-level")
			log := logx.New(logLevel)

			chunkSize, err := server.ParseSizeMB(chunkMB)
			if err != nil || chunkSize <= 0 {
				return errBadFlag("--chunk-size 需为正数（MB）")
			}
			maxSize, err := server.ParseSizeMB(maxUpload)
			if err != nil || maxSize <= 0 {
				return errBadFlag("--max-upload 需为正数（MB）")
			}
			srv, err := server.New(server.Options{
				DataDir:     dataDir,
				Addr:        addr,
				ChunkSize:   chunkSize,
				MaxUpload:   maxSize,
				ToolVersion: version,
				Log:         log,
			})
			if err != nil {
				log.Debug("服务初始化失败", "err", err.Error())
				return errBadFlag("服务初始化失败：" + dataDir)
			}
			defer srv.Close()
			return srv.ListenAndServe()
		},
	}
	cmd.Flags().StringVarP(&addr, "addr", "a", "127.0.0.1:8080", "监听地址")
	cmd.Flags().StringVar(&dataDir, "data", "pc2twin-data", "数据目录（上传暂存与转换产物）")
	cmd.Flags().StringVar(&chunkMB, "chunk-size", "16", "分片上传的单片大小（MB）")
	cmd.Flags().StringVar(&maxUpload, "max-upload", "2048", "单文件上传上限（MB）")
	return cmd
}

type badFlag struct{ msg string }

func (e *badFlag) Error() string { return e.msg }

func errBadFlag(msg string) error { return &badFlag{msg} }
