package main

import (
	"bytes"
	"strings"
	"testing"

	"github.com/spf13/cobra"
)

func execute(t *testing.T, args ...string) (string, error) {
	t.Helper()
	var buf bytes.Buffer
	root := newRootCmd()
	root.SetOut(&buf)
	root.SetErr(&buf)
	root.SetArgs(args)
	err := root.Execute()
	return buf.String(), err
}

func TestVersionCommand(t *testing.T) {
	out, err := execute(t, "version")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "pc2twin") {
		t.Fatalf("输出异常：%s", out)
	}
}

func TestConvertRequiresInput(t *testing.T) {
	_, err := execute(t, "convert")
	if err == nil {
		t.Fatal("缺少必填参数应报错")
	}
}

func TestConvertBadCRS(t *testing.T) {
	_, err := execute(t, "convert", "-i", "x.pcd", "-o", "out", "--crs-enu", "bad")
	if err == nil || !strings.Contains(err.Error(), "E3001") {
		t.Fatalf("应返回 E3001：%v", err)
	}
}

var _ = cobra.Command{}
