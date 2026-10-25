package pcd

import "testing"

func TestLZFBackref(t *testing.T) {
	// out = "abc" + 从 ref=0 回拷 7 字节
	in := []byte{2, 'a', 'b', 'c', (5 << 5) | 0, 2}
	out, err := lzfDecompress(in, 10)
	if err != nil {
		t.Fatal(err)
	}
	if string(out) != "abcabcabca" {
		t.Fatalf("解压结果 = %q", string(out))
	}
}
