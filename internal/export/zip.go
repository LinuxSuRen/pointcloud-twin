package export

import (
	"archive/zip"
	"io"
	"io/fs"
	"os"
	"path/filepath"
)

// ZipDir 把目录递归打包为 zip（路径相对 dir，使用 / 分隔符）。
// 供 3D Tiles 产物一键导出部署。
func ZipDir(w io.Writer, dir string) error {
	zw := zip.NewWriter(w)
	err := filepath.WalkDir(dir, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			return nil
		}
		rel, err := filepath.Rel(dir, path)
		if err != nil {
			return err
		}
		fw, err := zw.Create(filepath.ToSlash(rel))
		if err != nil {
			return err
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		_, err = fw.Write(data)
		return err
	})
	if err != nil {
		return err
	}
	return zw.Close()
}
