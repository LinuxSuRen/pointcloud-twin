package crs

import (
	"testing"

	"github.com/LinuxSuRen/pointcloud-twin/internal/apperr"
)

func TestParseENUOrigin(t *testing.T) {
	o, err := ParseENUOrigin("121.47, 31.23, 10")
	if err != nil {
		t.Fatal(err)
	}
	if o.Lon != 121.47 || o.Lat != 31.23 || o.H != 10 || !o.Explicit {
		t.Fatalf("解析错误：%+v", o)
	}

	o, err = ParseENUOrigin("0,0")
	if err != nil || o.H != 0 {
		t.Fatalf("省略 h 时应默认 0：%+v %v", o, err)
	}

	for _, bad := range []string{"121.47", "a,b", "200,31", "0,95", "1,2,3,4"} {
		if _, err := ParseENUOrigin(bad); err == nil {
			t.Fatalf("应报错：%q", bad)
		} else if _, ok := apperr.From(err); !ok {
			t.Fatalf("应返回稳定错误码：%q", bad)
		}
	}
}

func TestWKTAndPayload(t *testing.T) {
	o := ENUOrigin{Lon: 1, Lat: 2, H: 3, Explicit: true}
	w := o.WKT()
	if !contains(w, "ENGCRS") || !contains(w, "PARAMETER") {
		t.Fatalf("WKT 异常：%q", w)
	}
	if !contains(o.JSONPayload(), `"explicit":true`) {
		t.Fatalf("JSON 载荷异常：%q", o.JSONPayload())
	}
}

func contains(s, sub string) bool {
	return len(s) >= len(sub) && (s == sub || len(sub) == 0 || indexOf(s, sub) >= 0)
}

func indexOf(s, sub string) int {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return i
		}
	}
	return -1
}
