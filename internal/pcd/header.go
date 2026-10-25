package pcd

import (
	"bufio"
	"fmt"
	"io"
	"os"
	"strconv"
	"strings"

	"github.com/LinuxSuRen/pointcloud-twin/internal/apperr"
)

type reader struct {
	f      *os.File
	br     *bufio.Reader
	header Header
}

func openReader(path string) (*reader, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, apperr.New(apperr.E1001, err, path)
	}
	return &reader{f: f, br: bufio.NewReaderSize(f, 1<<20)}, nil
}

func (r *reader) close() error { return r.f.Close() }

// parseHeader 逐行解析 PCD 文件头，直到 DATA 行（含）为止。
func (r *reader) parseHeader() error {
	h := &r.header
	h.fieldIndex = map[string]int{}
	seen := map[string]bool{}
	h.Counts = nil

	for {
		line, err := r.readLine()
		if err == io.EOF {
			return apperr.New(apperr.E1002, nil, "文件头不完整，未找到 DATA 行")
		}
		if err != nil {
			return apperr.New(apperr.E1002, err, "读取文件头失败")
		}
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		key, rest := splitKey(line)
		switch key {
		case "VERSION":
			h.Version = rest
		case "FIELDS":
			h.Fields = strings.Fields(rest)
		case "SIZE":
			for _, s := range strings.Fields(rest) {
				n, err := strconv.Atoi(s)
				if err != nil {
					return apperr.New(apperr.E1002, err, "SIZE 行无效："+rest)
				}
				h.Sizes = append(h.Sizes, n)
			}
		case "TYPE":
			for _, s := range strings.Fields(rest) {
				if len(s) != 1 || (s[0] != 'F' && s[0] != 'U' && s[0] != 'I') {
					return apperr.New(apperr.E1002, nil, "TYPE 行无效："+rest)
				}
				h.Types = append(h.Types, s[0])
			}
		case "COUNT":
			for _, s := range strings.Fields(rest) {
				n, err := strconv.Atoi(s)
				if err != nil {
					return apperr.New(apperr.E1002, err, "COUNT 行无效："+rest)
				}
				h.Counts = append(h.Counts, n)
			}
		case "WIDTH":
			n, err := strconv.Atoi(strings.TrimSpace(rest))
			if err != nil {
				return apperr.New(apperr.E1002, err, "WIDTH 行无效："+rest)
			}
			h.Width = n
		case "HEIGHT":
			n, err := strconv.Atoi(strings.TrimSpace(rest))
			if err != nil {
				return apperr.New(apperr.E1002, err, "HEIGHT 行无效："+rest)
			}
			h.Height = n
		case "VIEWPOINT":
			// 与转换无关，忽略
		case "POINTS":
			n, err := strconv.Atoi(strings.TrimSpace(rest))
			if err != nil {
				return apperr.New(apperr.E1002, err, "POINTS 行无效："+rest)
			}
			h.Points = n
		case "DATA":
			h.Data = strings.TrimSpace(rest)
			if h.Data == "" {
				return apperr.New(apperr.E1002, nil, "DATA 行缺少编码类型")
			}
			r.header.dataOffset = int(r.pos())
			return r.finishHeader(seen)
		default:
			// 未知键忽略，保持向前兼容
		}
		seen[key] = true
	}
}

// finishHeader 校验并补全文件头派生信息。
func (r *reader) finishHeader(seen map[string]bool) error {
	h := &r.header
	if len(h.Fields) == 0 {
		return apperr.New(apperr.E1002, nil, "缺少 FIELDS 行")
	}
	if len(h.Sizes) == 0 || len(h.Types) == 0 {
		return apperr.New(apperr.E1002, nil, "缺少 SIZE/TYPE 行")
	}
	if len(h.Sizes) != len(h.Fields) || len(h.Types) != len(h.Fields) {
		return apperr.New(apperr.E1002, nil,
			fmt.Sprintf("FIELDS/SIZE/TYPE 数量不一致：%v / %v / %v", h.Fields, h.Sizes, h.Types))
	}
	if len(h.Counts) == 0 {
		h.Counts = make([]int, len(h.Fields))
		for i := range h.Counts {
			h.Counts[i] = 1
		}
	} else if len(h.Counts) != len(h.Fields) {
		return apperr.New(apperr.E1002, nil, "COUNT 与 FIELDS 数量不一致")
	}
	for i, f := range h.Fields {
		if _, dup := h.fieldIndex[f]; !dup {
			h.fieldIndex[f] = i
		}
	}
	h.pointSize = 0
	for i := range h.Fields {
		h.pointSize += h.Sizes[i] * h.Counts[i]
	}
	if h.Points <= 0 {
		// 个别导出工具漏写 POINTS，用 WIDTH*HEIGHT 兜底
		if h.Width > 0 && h.Height > 0 {
			h.Points = h.Width * h.Height
		}
	}
	return nil
}

// readLine 读取一行（去掉 \r\n），并推进已读计数。
func (r *reader) readLine() (string, error) {
	line, err := r.br.ReadString('\n')
	if err != nil && err != io.EOF {
		return "", err
	}
	if line == "" && err == io.EOF {
		return "", io.EOF
	}
	return strings.TrimRight(line, "\r\n"), nil
}

// pos 估算当前文件偏移（用于定位 DATA 起始位置，随后会 Seek 回来）。
func (r *reader) pos() int64 {
	off, _ := r.f.Seek(0, io.SeekCurrent)
	// bufio 可能已预读，减去缓冲区未消费部分
	return off - int64(r.br.Buffered())
}

// seekData 回到 DATA 数据区起始位置。
func (r *reader) seekData() error {
	if _, err := r.f.Seek(int64(r.header.dataOffset), io.SeekStart); err != nil {
		return apperr.New(apperr.E1002, err, "定位数据区失败")
	}
	r.br.Reset(r.f)
	return nil
}

func splitKey(line string) (key, rest string) {
	idx := strings.IndexAny(line, " \t")
	if idx < 0 {
		return strings.ToUpper(line), ""
	}
	return strings.ToUpper(line[:idx]), strings.TrimSpace(line[idx+1:])
}
