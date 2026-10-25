package server

import (
	"bytes"
	"encoding/binary"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

// runJobToSuccess 走完上传+任务流程，返回成功的任务视图。
func runJobToSuccess(t *testing.T, ts *httptest.Server, points int) JobView {
	t.Helper()
	data := samplePCDBytes(t, points)
	pcdID := uploadViaAPI(t, ts.URL, purposePCD, "site.pcd", data)
	devices := `{"devices":[{"deviceId":"pcs-01","deviceType":"pcs","center":[1,1,1],"size":[2,2,2],"heading":0}]}`
	devID := uploadViaAPI(t, ts.URL, purposeDevices, "devices.json", []byte(devices))

	resp, err := http.Post(ts.URL+"/api/jobs", "application/json",
		strings.NewReader(`{"pcdUploadId":"`+pcdID+`","devicesUploadId":"`+devID+`","options":{"pointsPerTile":50}}`))
	if err != nil {
		t.Fatal(err)
	}
	var job JobView
	json.NewDecoder(resp.Body).Decode(&job)
	resp.Body.Close()

	deadline := time.Now().Add(30 * time.Second)
	for {
		gresp, _ := http.Get(ts.URL + "/api/jobs/" + job.ID)
		json.NewDecoder(gresp.Body).Decode(&job)
		gresp.Body.Close()
		if job.State.terminal() {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("任务超时")
		}
		time.Sleep(100 * time.Millisecond)
	}
	if job.State != stateSucceeded {
		t.Fatalf("任务应成功：%+v", job.Error)
	}
	return job
}

func TestJobExport(t *testing.T) {
	_, ts := testServer(t)
	job := runJobToSuccess(t, ts, 200)

	if job.Result.ExportZipUrl == "" || job.Result.ExportGlbUrl == "" {
		t.Fatalf("result 缺少导出地址：%+v", job.Result)
	}

	// zip 导出：PK 魔数 + application/zip
	zresp, err := http.Get(ts.URL + job.Result.ExportZipUrl)
	if err != nil {
		t.Fatal(err)
	}
	defer zresp.Body.Close()
	if zresp.StatusCode != http.StatusOK {
		t.Fatalf("zip 导出 HTTP %d", zresp.StatusCode)
	}
	if ct := zresp.Header.Get("Content-Type"); ct != "application/zip" {
		t.Fatalf("zip Content-Type = %s", ct)
	}
	var zhead [4]byte
	io.ReadFull(zresp.Body, zhead[:])
	if !bytes.Equal(zhead[:2], []byte("PK")) {
		t.Fatalf("zip 魔数错误：%v", zhead)
	}

	// GLB 导出：glTF 魔数 + 节点包含设备
	gresp, err := http.Get(ts.URL + job.Result.ExportGlbUrl)
	if err != nil {
		t.Fatal(err)
	}
	defer gresp.Body.Close()
	if gresp.StatusCode != http.StatusOK {
		t.Fatalf("GLB 导出 HTTP %d", gresp.StatusCode)
	}
	glb, err := io.ReadAll(gresp.Body)
	if err != nil {
		t.Fatal(err)
	}
	if binary.LittleEndian.Uint32(glb[0:4]) != 0x46546C67 {
		t.Fatal("GLB 魔数错误")
	}
	jsonLen := int(binary.LittleEndian.Uint32(glb[12:16]))
	if !strings.Contains(string(glb[20:20+jsonLen]), "pcs-01") {
		t.Fatal("GLB 中缺少设备节点")
	}
}

func TestJobExportUnknownJob(t *testing.T) {
	_, ts := testServer(t)
	for _, u := range []string{"/api/jobs/nope/export", "/api/jobs/nope/export/glb"} {
		resp, _ := http.Get(ts.URL + u)
		if resp.StatusCode != http.StatusNotFound {
			t.Fatalf("%s 应 404，实际 %d", u, resp.StatusCode)
		}
		resp.Body.Close()
	}
}
