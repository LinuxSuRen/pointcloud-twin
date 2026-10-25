package server

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/LinuxSuRen/pointcloud-twin/internal/pcd"
	"github.com/LinuxSuRen/pointcloud-twin/internal/pcdfix"
)

// testServer 构造小分片的服务实例。
func testServer(t *testing.T) (*Server, *httptest.Server) {
	t.Helper()
	srv, err := New(Options{
		DataDir:   t.TempDir(),
		ChunkSize: 1024, // 小分片，方便多片场景
		MaxUpload: 64 << 20,
		Log:       slog.New(slog.NewTextHandler(io.Discard, nil)),
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(srv.Close)
	ts := httptest.NewServer(srv.Handler())
	t.Cleanup(ts.Close)
	return srv, ts
}

func samplePCDBytes(t *testing.T, n int) []byte {
	t.Helper()
	var pts []pcd.Point
	for i := 0; i < n; i++ {
		pts = append(pts, pcd.Point{
			X: float64(i % 20), Y: float64(i / 20 % 20), Z: float64(i / 400),
			R: 60, G: 120, B: 180, HasRGB: true, Intensity: 1000, HasIntensity: true,
		})
	}
	var buf bytes.Buffer
	if err := pcdfix.WriteBinary(&buf, pts); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

// uploadViaAPI 走完整分片上传流程（含 sha256 校验），返回 uploadId。
func uploadViaAPI(t *testing.T, base, purpose, filename string, data []byte) string {
	t.Helper()
	// init
	body, _ := json.Marshal(initRequest{Filename: filename, Size: int64(len(data)), Purpose: purpose})
	resp, err := http.Post(base+"/api/uploads/init", "application/json", bytes.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	var initResp struct {
		UploadID  string `json:"uploadId"`
		ChunkSize int64  `json:"chunkSize"`
	}
	json.NewDecoder(resp.Body).Decode(&initResp)
	resp.Body.Close()
	if initResp.UploadID == "" {
		t.Fatal("init 未返回 uploadId")
	}

	// PUT 分片（按服务端分片大小切块）
	chunkSize := int(initResp.ChunkSize)
	for off, idx := 0, 0; off < len(data); off, idx = off+chunkSize, idx+1 {
		end := off + chunkSize
		if end > len(data) {
			end = len(data)
		}
		req, _ := http.NewRequest(http.MethodPut,
			fmt.Sprintf("%s/api/uploads/%s/chunks/%d", base, initResp.UploadID, idx),
			bytes.NewReader(data[off:end]))
		cresp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		io.Copy(io.Discard, cresp.Body)
		cresp.Body.Close()
		if cresp.StatusCode != http.StatusOK {
			t.Fatalf("分片 %d 上传失败：HTTP %d", idx, cresp.StatusCode)
		}
	}

	// complete（带 sha256）
	sum := sha256.Sum256(data)
	cbody, _ := json.Marshal(completeRequest{SHA256: hex.EncodeToString(sum[:])})
	creq, _ := http.NewRequest(http.MethodPost,
		fmt.Sprintf("%s/api/uploads/%s/complete", base, initResp.UploadID),
		bytes.NewReader(cbody))
	cresp, err := http.DefaultClient.Do(creq)
	if err != nil {
		t.Fatal(err)
	}
	defer cresp.Body.Close()
	if cresp.StatusCode != http.StatusOK {
		t.Fatalf("complete 失败：HTTP %d", cresp.StatusCode)
	}
	return initResp.UploadID
}

func TestUploadFlowWithSHA256(t *testing.T) {
	_, ts := testServer(t)
	data := samplePCDBytes(t, 50) // ~1KB 头 + 1000B 数据，多片
	id := uploadViaAPI(t, ts.URL, purposePCD, "site.pcd", data)

	// status 应为 completed 且分片齐全
	resp, _ := http.Get(ts.URL + "/api/uploads/" + id)
	var st statusBody
	json.NewDecoder(resp.Body).Decode(&st)
	resp.Body.Close()
	if st.State != "completed" || st.TotalChunks < 2 || int64(len(st.ReceivedChunks)) != st.TotalChunks {
		t.Fatalf("status 异常：%+v", st)
	}

	// 重复 complete 幂等
	creq, _ := http.NewRequest(http.MethodPost, ts.URL+"/api/uploads/"+id+"/complete", strings.NewReader("{}"))
	cresp, _ := http.DefaultClient.Do(creq)
	io.Copy(io.Discard, cresp.Body)
	cresp.Body.Close()
	if cresp.StatusCode != http.StatusOK {
		t.Fatalf("幂等 complete 失败：HTTP %d", cresp.StatusCode)
	}
}

func TestUploadChunkBoundary(t *testing.T) {
	srv, ts := testServer(t)
	_ = srv
	// init 一个 10 字节文件（chunk 1024 → 1 片期望 10 字节）
	body, _ := json.Marshal(initRequest{Filename: "a.pcd", Size: 10, Purpose: purposePCD})
	resp, _ := http.Post(ts.URL+"/api/uploads/init", "application/json", bytes.NewReader(body))
	var ir struct {
		UploadID string `json:"uploadId"`
	}
	json.NewDecoder(resp.Body).Decode(&ir)
	resp.Body.Close()

	// 多粘一个字节 → 400
	req, _ := http.NewRequest(http.MethodPut, ts.URL+"/api/uploads/"+ir.UploadID+"/chunks/0",
		bytes.NewReader(make([]byte, 11)))
	cresp, _ := http.DefaultClient.Do(req)
	io.Copy(io.Discard, cresp.Body)
	cresp.Body.Close()
	if cresp.StatusCode != http.StatusBadRequest {
		t.Fatalf("粘连请求应 400，实际 %d", cresp.StatusCode)
	}

	// 少一个字节 → 400
	req, _ = http.NewRequest(http.MethodPut, ts.URL+"/api/uploads/"+ir.UploadID+"/chunks/0",
		bytes.NewReader(make([]byte, 9)))
	cresp, _ = http.DefaultClient.Do(req)
	io.Copy(io.Discard, cresp.Body)
	cresp.Body.Close()
	if cresp.StatusCode != http.StatusBadRequest {
		t.Fatalf("缺字节应 400，实际 %d", cresp.StatusCode)
	}

	// complete 应报分片不齐全
	creq, _ := http.NewRequest(http.MethodPost, ts.URL+"/api/uploads/"+ir.UploadID+"/complete", strings.NewReader("{}"))
	cresp, _ = http.DefaultClient.Do(creq)
	defer io.Copy(io.Discard, cresp.Body)
	cresp.Body.Close()
	if cresp.StatusCode != http.StatusBadRequest {
		t.Fatalf("分片不齐全应 400，实际 %d", cresp.StatusCode)
	}
}

func TestUploadIdempotentReput(t *testing.T) {
	_, ts := testServer(t)
	data := samplePCDBytes(t, 5)
	id := uploadViaAPI(t, ts.URL, purposePCD, "b.pcd", data)

	// 会话已完成，重复传片应 409
	req, _ := http.NewRequest(http.MethodPut, ts.URL+"/api/uploads/"+id+"/chunks/0",
		bytes.NewReader(data[:1]))
	cresp, _ := http.DefaultClient.Do(req)
	io.Copy(io.Discard, cresp.Body)
	cresp.Body.Close()
	if cresp.StatusCode != http.StatusConflict {
		t.Fatalf("已定稿会话应 409，实际 %d", cresp.StatusCode)
	}
}

func TestJobEndToEnd(t *testing.T) {
	_, ts := testServer(t)
	data := samplePCDBytes(t, 300)
	pcdID := uploadViaAPI(t, ts.URL, purposePCD, "site.pcd", data)
	devices := `{"devices":[{"deviceId":"pcs-01","deviceType":"pcs","center":[1,1,1],"size":[2,2,2],"heading":0}]}`
	devID := uploadViaAPI(t, ts.URL, purposeDevices, "devices.json", []byte(devices))

	// 建任务
	jbody := fmt.Sprintf(`{"pcdUploadId":%q,"devicesUploadId":%q,"options":{"crsEnu":"121.47,31.23,10","pointsPerTile":50}}`, pcdID, devID)
	resp, err := http.Post(ts.URL+"/api/jobs", "application/json", strings.NewReader(jbody))
	if err != nil {
		t.Fatal(err)
	}
	var job JobView
	json.NewDecoder(resp.Body).Decode(&job)
	resp.Body.Close()
	if resp.StatusCode != http.StatusAccepted {
		t.Fatalf("建任务应 202，实际 %d", resp.StatusCode)
	}

	// 轮询至终态
	deadline := time.Now().Add(30 * time.Second)
	for {
		gresp, _ := http.Get(fmt.Sprintf("%s/api/jobs/%s", ts.URL, job.ID))
		json.NewDecoder(gresp.Body).Decode(&job)
		gresp.Body.Close()
		if job.State.terminal() {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("任务超时未结束，状态 %s", job.State)
		}
		time.Sleep(100 * time.Millisecond)
	}
	if job.State != stateSucceeded {
		t.Fatalf("任务应成功，实际 %s，错误 %+v", job.State, job.Error)
	}
	if job.Result == nil || !strings.HasSuffix(job.Result.TilesUrl, "tileset.json") {
		t.Fatalf("result 异常：%+v", job.Result)
	}

	// 产物可访问
	for _, u := range []string{job.Result.TilesUrl, job.Result.ReportUrl, job.Result.DevicesUrl} {
		oresp, err := http.Get(ts.URL + u)
		if err != nil {
			t.Fatal(err)
		}
		io.Copy(io.Discard, oresp.Body)
		oresp.Body.Close()
		if oresp.StatusCode != http.StatusOK {
			t.Fatalf("产物 %s 应 200，实际 %d", u, oresp.StatusCode)
		}
	}
	// 相对瓦片也可访问
	oresp, _ := http.Get(ts.URL + job.Result.TilesUrl)
	var ts2 struct {
		Root struct {
			Content struct {
				URI string `json:"uri"`
			} `json:"content"`
		} `json:"root"`
	}
	json.NewDecoder(oresp.Body).Decode(&ts2)
	oresp.Body.Close()
	gresp, _ := http.Get(fmt.Sprintf("%s/api/jobs/%s/out/%s", ts.URL, job.ID, ts2.Root.Content.URI))
	if gresp.StatusCode != http.StatusOK {
		t.Fatalf("瓦片应 200，实际 %d", gresp.StatusCode)
	}
	gresp.Body.Close()
}

func TestJobConflictWhileRunning(t *testing.T) {
	srv, ts := testServer(t)
	data := samplePCDBytes(t, 2000) // 4 万点数据，确保第一个任务还在跑
	pcdID := uploadViaAPI(t, ts.URL, purposePCD, "big.pcd", data)

	jbody := fmt.Sprintf(`{"pcdUploadId":%q,"options":{"pointsPerTile":50}}`, pcdID)
	resp, _ := http.Post(ts.URL+"/api/jobs", "application/json", strings.NewReader(jbody))
	io.Copy(io.Discard, resp.Body)
	resp.Body.Close()
	if resp.StatusCode != http.StatusAccepted {
		t.Fatalf("首任务应 202，实际 %d", resp.StatusCode)
	}

	// 立即再建一个 → 409
	resp2, _ := http.Post(ts.URL+"/api/jobs", "application/json", strings.NewReader(jbody))
	var eb errorBody
	json.NewDecoder(resp2.Body).Decode(&eb)
	resp2.Body.Close()
	if resp2.StatusCode != http.StatusConflict {
		t.Fatalf("并发超限应 409，实际 %d", resp2.StatusCode)
	}
	if eb.Error.Message == "" {
		t.Fatal("409 应携带明确文案")
	}

	// 等待首任务结束，避免与其他用例互扰
	waitIdle(t, srv)
}

func TestJobCancel(t *testing.T) {
	_, ts := testServer(t)
	data := samplePCDBytes(t, 3000)
	pcdID := uploadViaAPI(t, ts.URL, purposePCD, "c.pcd", data)

	jbody := fmt.Sprintf(`{"pcdUploadId":%q,"options":{"pointsPerTile":10}}`, pcdID)
	resp, _ := http.Post(ts.URL+"/api/jobs", "application/json", strings.NewReader(jbody))
	var job JobView
	json.NewDecoder(resp.Body).Decode(&job)
	resp.Body.Close()

	dreq, _ := http.NewRequest(http.MethodDelete, fmt.Sprintf("%s/api/jobs/%s", ts.URL, job.ID), nil)
	dresp, _ := http.DefaultClient.Do(dreq)
	io.Copy(io.Discard, dresp.Body)
	dresp.Body.Close()
	if dresp.StatusCode != http.StatusAccepted {
		t.Fatalf("取消应 202，实际 %d", dresp.StatusCode)
	}

	deadline := time.Now().Add(30 * time.Second)
	for {
		gresp, _ := http.Get(fmt.Sprintf("%s/api/jobs/%s", ts.URL, job.ID))
		json.NewDecoder(gresp.Body).Decode(&job)
		gresp.Body.Close()
		if job.State.terminal() {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("任务超时未结束")
		}
		time.Sleep(50 * time.Millisecond)
	}
	if job.State != stateCancelled {
		t.Fatalf("应取消化终态，实际 %s（错误 %+v）", job.State, job.Error)
	}
}

func TestJobSSEEvents(t *testing.T) {
	_, ts := testServer(t)
	data := samplePCDBytes(t, 800)
	pcdID := uploadViaAPI(t, ts.URL, purposePCD, "d.pcd", data)

	jbody := fmt.Sprintf(`{"pcdUploadId":%q,"options":{"pointsPerTile":50}}`, pcdID)
	resp, _ := http.Post(ts.URL+"/api/jobs", "application/json", strings.NewReader(jbody))
	var job JobView
	json.NewDecoder(resp.Body).Decode(&job)
	resp.Body.Close()

	eresp, err := http.Get(fmt.Sprintf("%s/api/jobs/%s/events", ts.URL, job.ID))
	if err != nil {
		t.Fatal(err)
	}
	defer eresp.Body.Close()
	if ct := eresp.Header.Get("Content-Type"); !strings.Contains(ct, "text/event-stream") {
		t.Fatalf("Content-Type = %s", ct)
	}
	if eresp.Header.Get("X-Accel-Buffering") != "no" {
		t.Fatal("缺少 X-Accel-Buffering: no")
	}

	// 读流直到连接关闭（终态即关）
	var last JobView
	frames := 0
	buf, err := io.ReadAll(eresp.Body)
	if err != nil {
		t.Fatal(err)
	}
	for _, line := range strings.Split(string(buf), "\n") {
		if !strings.HasPrefix(line, "data: ") {
			continue
		}
		frames++
		var v JobView
		if err := json.Unmarshal([]byte(strings.TrimPrefix(line, "data: ")), &v); err != nil {
			t.Fatalf("帧解析失败：%v", err)
		}
		last = v
	}
	if frames < 1 {
		t.Fatal("至少应有一帧快照")
	}
	if !last.State.terminal() {
		t.Fatalf("最后一帧应为终态，实际 %s", last.State)
	}
	if last.State == stateSucceeded && last.Result == nil {
		t.Fatal("成功终态应携带 result")
	}
}

func TestPanicIsolation(t *testing.T) {
	srv, ts := testServer(t)
	// 注入 panic 钩子：任务执行即 panic，服务不应崩溃且任务转「失败」
	orig := panicHook
	panicHook = func() { panic("boom") }
	t.Cleanup(func() { panicHook = orig })

	data := samplePCDBytes(t, 5)
	pcdID := uploadViaAPI(t, ts.URL, purposePCD, "e.pcd", data)
	jbody := fmt.Sprintf(`{"pcdUploadId":%q,"options":{}}`, pcdID)
	resp, _ := http.Post(ts.URL+"/api/jobs", "application/json", strings.NewReader(jbody))
	io.Copy(io.Discard, resp.Body)
	resp.Body.Close()
	if resp.StatusCode != http.StatusAccepted && resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("建任务异常：HTTP %d", resp.StatusCode)
	}

	deadline := time.Now().Add(10 * time.Second)
	for {
		srv.jobs.mu.Lock()
		var last *Job
		for _, j := range srv.jobs.jobs {
			last = j
		}
		srv.jobs.mu.Unlock()
		if last != nil && last.State.terminal() {
			if last.State != stateFailed || last.Error == nil || last.Error.Code != "E9001" {
				t.Fatalf("panic 应转失败 E9001，实际 %s %+v", last.State, last.Error)
			}
			return
		}
		if time.Now().After(deadline) {
			t.Fatal("任务未结束")
		}
		time.Sleep(50 * time.Millisecond)
	}
}

func TestUploadPurposeValidation(t *testing.T) {
	_, ts := testServer(t)
	// 非 .pcd 扩展名
	body, _ := json.Marshal(initRequest{Filename: "evil.exe", Size: 10, Purpose: purposePCD})
	resp, _ := http.Post(ts.URL+"/api/uploads/init", "application/json", bytes.NewReader(body))
	io.Copy(io.Discard, resp.Body)
	resp.Body.Close()
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("扩展名校验失败：HTTP %d", resp.StatusCode)
	}

	// 会话不存在
	gresp, _ := http.Get(ts.URL + "/api/uploads/nonexistent")
	if gresp.StatusCode != http.StatusNotFound {
		t.Fatalf("不存在的会话应 404，实际 %d", gresp.StatusCode)
	}
	gresp.Body.Close()
}

// waitIdle 等待 running 归零（避免用例间并发限制互扰）。
func waitIdle(t *testing.T, srv *Server) {
	t.Helper()
	deadline := time.Now().Add(60 * time.Second)
	for {
		srv.jobs.mu.Lock()
		running := srv.jobs.running
		srv.jobs.mu.Unlock()
		if running == 0 {
			return
		}
		if time.Now().After(deadline) {
			t.Fatal("等待任务结束超时")
		}
		time.Sleep(100 * time.Millisecond)
	}
}
