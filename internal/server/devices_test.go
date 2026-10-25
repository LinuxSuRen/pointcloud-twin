package server

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"
)

func TestSaveDevicesWithoutInitialConfig(t *testing.T) {
	_, ts := testServer(t)
	// 不带 devices 上传的任务：写回时 origin 应从 report.json 回填
	data := samplePCDBytes(t, 30)
	pcdID := uploadViaAPI(t, ts.URL, purposePCD, "site.pcd", data)
	resp, _ := http.Post(ts.URL+"/api/jobs", "application/json",
		strings.NewReader(`{"pcdUploadId":"`+pcdID+`","options":{"crsEnu":"121.47,31.23,10"}}`))
	var job JobView
	json.NewDecoder(resp.Body).Decode(&job)
	resp.Body.Close()
	for i := 0; i < 100; i++ {
		gresp, _ := http.Get(ts.URL + "/api/jobs/" + job.ID)
		json.NewDecoder(gresp.Body).Decode(&job)
		gresp.Body.Close()
		if job.State.terminal() {
			break
		}
		time.Sleep(100 * time.Millisecond)
	}
	if job.State != stateSucceeded {
		t.Fatalf("任务应成功：%+v", job.Error)
	}

	body := `{"devices":[{"deviceId":"annotated-01","deviceType":"pcs","center":[1,1,1],"size":[2,2,2],"heading":45}]}`
	req, _ := http.NewRequest(http.MethodPut, ts.URL+"/api/jobs/"+job.ID+"/out/devices.json", strings.NewReader(body))
	putResp, _ := http.DefaultClient.Do(req)
	io.Copy(io.Discard, putResp.Body)
	putResp.Body.Close()
	if putResp.StatusCode != http.StatusOK {
		t.Fatalf("写回应 200，实际 %d", putResp.StatusCode)
	}

	dresp, _ := http.Get(ts.URL + "/api/jobs/" + job.ID + "/out/devices.json")
	var cfg map[string]any
	json.NewDecoder(dresp.Body).Decode(&cfg)
	dresp.Body.Close()
	origin, _ := cfg["origin"].(map[string]any)
	if origin == nil || origin["lon"] != 121.47 {
		t.Fatalf("origin 应从 report.json 回填：%v", origin)
	}

	// 导出 GLB 应包含标注后的设备（读取产物中编辑后的 devices.json）
	glbResp, _ := http.Get(ts.URL + "/api/jobs/" + job.ID + "/export/glb")
	glb, _ := io.ReadAll(glbResp.Body)
	glbResp.Body.Close()
	if !bytes.Contains(glb, []byte("annotated-01")) {
		t.Fatal("GLB 应包含标注后的设备")
	}
}

func TestSaveDevices(t *testing.T) {
	_, ts := testServer(t)
	job := runJobToSuccess(t, ts, 50)

	// 合法配置写回
	body := `{"devices":[{"deviceId":"battery-cluster-01","deviceType":"battery-cluster","center":[1,1,1],"size":[2,2,2],"heading":10}]}`
	req, _ := http.NewRequest(http.MethodPut, ts.URL+"/api/jobs/"+job.ID+"/out/devices.json", strings.NewReader(body))
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	io.Copy(io.Discard, resp.Body)
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("写回应 200，实际 %d", resp.StatusCode)
	}

	// 写回后产物内容更新且 origin 被保留
	dresp, _ := http.Get(ts.URL + "/api/jobs/" + job.ID + "/out/devices.json")
	var cfg map[string]any
	json.NewDecoder(dresp.Body).Decode(&cfg)
	dresp.Body.Close()
	devices := cfg["devices"].([]any)
	if len(devices) != 1 {
		t.Fatalf("写回后设备数 = %d", len(devices))
	}
	if cfg["origin"] == nil {
		t.Fatal("写回后应保留 origin（convert 注入的 ENU 原点）")
	}

	// 非法配置（重复 ID）→ 400
	bad := `{"devices":[{"deviceId":"a","deviceType":"pcs","center":[0,0,0],"size":[1,1,1],"heading":0},{"deviceId":"a","deviceType":"pcs","center":[0,0,0],"size":[1,1,1],"heading":0}]}`
	breq, _ := http.NewRequest(http.MethodPut, ts.URL+"/api/jobs/"+job.ID+"/out/devices.json", strings.NewReader(bad))
	bresp, _ := http.DefaultClient.Do(breq)
	var eb errorBody
	json.NewDecoder(bresp.Body).Decode(&eb)
	bresp.Body.Close()
	if bresp.StatusCode != http.StatusBadRequest || !bytes.Contains([]byte(eb.Error.Message), []byte("重复")) {
		t.Fatalf("重复 ID 应 400 且文案明确：%d %s", bresp.StatusCode, eb.Error.Message)
	}

	// 坏 JSON → 400
	creq, _ := http.NewRequest(http.MethodPut, ts.URL+"/api/jobs/"+job.ID+"/out/devices.json", strings.NewReader("{bad"))
	cresp, _ := http.DefaultClient.Do(creq)
	io.Copy(io.Discard, cresp.Body)
	cresp.Body.Close()
	if cresp.StatusCode != http.StatusBadRequest {
		t.Fatalf("坏 JSON 应 400，实际 %d", cresp.StatusCode)
	}

	// 不存在的任务 → 404
	dreq, _ := http.NewRequest(http.MethodPut, ts.URL+"/api/jobs/nope/out/devices.json", strings.NewReader(body))
	dresp2, _ := http.DefaultClient.Do(dreq)
	io.Copy(io.Discard, dresp2.Body)
	dresp2.Body.Close()
	if dresp2.StatusCode != http.StatusNotFound {
		t.Fatalf("未知任务应 404，实际 %d", dresp2.StatusCode)
	}
}
