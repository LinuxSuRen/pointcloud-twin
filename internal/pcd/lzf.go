package pcd

import "errors"

// lzfDecompress 解压 PCL binary_compressed 使用的 LZF 格式。
// 算法参考 liblzf 的 lzf_decompress（Marc Lehmann，许可见 PCL 上游）。
func lzfDecompress(in []byte, outLen int) ([]byte, error) {
	out := make([]byte, outLen)
	ip, op := 0, 0
	inLen, outLimit := len(in), outLen
	for ip < inLen {
		ctrl := int(in[ip])
		ip++
		if ctrl < 1<<5 {
			// 字面量：ctrl+1 个字节直接拷贝
			length := ctrl + 1
			if ip+length > inLen || op+length > outLimit {
				return nil, errors.New("lzf: 字面量越界")
			}
			copy(out[op:op+length], in[ip:ip+length])
			ip += length
			op += length
			continue
		}
		// 回溯引用
		length := ctrl >> 5
		if length == 7 {
			if ip >= inLen {
				return nil, errors.New("lzf: 长度字节缺失")
			}
			length += int(in[ip])
			ip++
		}
		if ip >= inLen {
			return nil, errors.New("lzf: 引用字节缺失")
		}
		ref := op - (((ctrl & 0x1f) << 8) + int(in[ip])) - 1
		ip++
		if ref < 0 || op+length+2 > outLimit {
			return nil, errors.New("lzf: 引用越界")
		}
		length += 2
		// 逐字节拷贝以支持重叠引用
		for i := 0; i < length; i++ {
			out[op] = out[ref]
			op++
			ref++
		}
	}
	if op != outLimit {
		return nil, errors.New("lzf: 解压长度不符")
	}
	return out, nil
}
