package server

import (
	"bufio"
	"bytes"
	"context"
	"encoding/csv"
	"encoding/json"
	"encoding/xml"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"
	"unicode"

	"github.com/Youzini-afk/ComfyNexus/internal/errs"
	"github.com/Youzini-afk/ComfyNexus/internal/sshmgr"
)

type gpuResponse struct {
	GPUs      []gpuInfo `json:"gpus"`
	Raw       string    `json:"raw,omitempty"`
	UpdatedAt string    `json:"updatedAt"`
}

type gpuInfo struct {
	Index             int      `json:"index"`
	Name              string   `json:"name,omitempty"`
	UUID              string   `json:"uuid,omitempty"`
	UtilizationGPU    *int     `json:"utilizationGpu,omitempty"`
	UtilizationMemory *int     `json:"utilizationMemory,omitempty"`
	MemoryTotalMiB    *int     `json:"memoryTotalMiB,omitempty"`
	MemoryUsedMiB     *int     `json:"memoryUsedMiB,omitempty"`
	TemperatureC      *int     `json:"temperatureC,omitempty"`
	PowerDrawW        *float64 `json:"powerDrawW,omitempty"`
	PowerLimitW       *float64 `json:"powerLimitW,omitempty"`
	DriverVersion     string   `json:"driverVersion,omitempty"`
	CUDAVersion       string   `json:"cudaVersion,omitempty"`
}

type comfyStatusResponse struct {
	Running   bool   `json:"running"`
	PIDs      []int  `json:"pids"`
	Port      int    `json:"port"`
	Root      string `json:"root,omitempty"`
	UpdatedAt string `json:"updatedAt"`
}

type comfyLogsResponse struct {
	Path      string `json:"path,omitempty"`
	Text      string `json:"text"`
	UpdatedAt string `json:"updatedAt"`
}

func (s *Server) systemGPU(w http.ResponseWriter, r *http.Request) {
	active, err := s.loadActiveInstance(r.Context())
	if err != nil {
		errs.Write(w, err)
		return
	}
	updatedAt := time.Now().UTC().Format(time.RFC3339)

	xmlOut, err := s.runRemoteCommand(r.Context(), active.Target, "command -v nvidia-smi >/dev/null 2>&1 && nvidia-smi -q -x", 20*time.Second)
	if err == nil && len(bytes.TrimSpace(xmlOut)) > 0 {
		gpus, parseErr := parseNvidiaXML(xmlOut)
		if parseErr == nil {
			resp := gpuResponse{GPUs: gpus, UpdatedAt: updatedAt}
			if r.URL.Query().Get("raw") == "1" || strings.EqualFold(r.URL.Query().Get("raw"), "true") {
				resp.Raw = redactText(string(xmlOut))
			}
			writeJSON(w, http.StatusOK, resp)
			return
		}
	}

	csvCmd := "command -v nvidia-smi >/dev/null 2>&1 && nvidia-smi --query-gpu=index,name,uuid,utilization.gpu,utilization.memory,memory.total,memory.used,temperature.gpu,power.draw,power.limit --format=csv,noheader,nounits"
	csvOut, csvErr := s.runRemoteCommand(r.Context(), active.Target, csvCmd, 20*time.Second)
	if csvErr == nil && len(bytes.TrimSpace(csvOut)) > 0 {
		gpus, parseErr := parseNvidiaCSV(csvOut)
		if parseErr == nil {
			resp := gpuResponse{GPUs: gpus, UpdatedAt: updatedAt}
			if r.URL.Query().Get("raw") == "1" || strings.EqualFold(r.URL.Query().Get("raw"), "true") {
				resp.Raw = redactText(string(csvOut))
			}
			writeJSON(w, http.StatusOK, resp)
			return
		}
	}

	// nvidia-smi may be absent on CPU-only hosts or unavailable inside a
	// container. Keep the endpoint stable and non-fatal for the ops panel.
	writeJSON(w, http.StatusOK, gpuResponse{GPUs: []gpuInfo{}, UpdatedAt: updatedAt})
}

func (s *Server) comfyStatus(w http.ResponseWriter, r *http.Request) {
	active, err := s.loadActiveInstance(r.Context())
	if err != nil {
		errs.Write(w, err)
		return
	}
	cmd := buildComfyStatusCommand(active)
	out, err := s.runRemoteCommand(r.Context(), active.Target, cmd, 10*time.Second)
	if err != nil {
		errs.Write(w, errs.New(errs.CodeInstanceUnreach, http.StatusBadGateway, "cannot query ComfyUI status: "+safeError(err)))
		return
	}
	pids, portOpen := parseComfyStatusOutput(string(out))
	writeJSON(w, http.StatusOK, comfyStatusResponse{
		Running:   portOpen || len(pids) > 0,
		PIDs:      pids,
		Port:      active.ComfyPort,
		Root:      active.ComfyRoot,
		UpdatedAt: time.Now().UTC().Format(time.RFC3339),
	})
}

func (s *Server) comfyRestart(w http.ResponseWriter, r *http.Request) {
	active, err := s.loadActiveInstance(r.Context())
	if err != nil {
		errs.Write(w, err)
		return
	}
	startCmd := strings.TrimSpace(active.ComfyStartCmd)
	if startCmd == "" {
		errs.Write(w, errs.New(errs.CodeBadRequest, http.StatusBadRequest, "comfy_start_cmd is not configured for the active instance; set it to your known-safe ComfyUI start/restart command before using restart"))
		return
	}

	cmd := "nohup sh -lc " + shellQuote(prefixWithCD(active.ComfyRoot, startCmd)) + " >/tmp/comfynexus-comfyui-start.log 2>&1 </dev/null &"
	if _, err := s.runRemoteCommand(r.Context(), active.Target, cmd, 10*time.Second); err != nil {
		errs.Write(w, errs.New(errs.CodeInstanceUnreach, http.StatusBadGateway, "cannot start ComfyUI: "+safeError(err)))
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"status":    "ok",
		"updatedAt": time.Now().UTC().Format(time.RFC3339),
	})
}

func (s *Server) comfyLogs(w http.ResponseWriter, r *http.Request) {
	active, err := s.loadActiveInstance(r.Context())
	if err != nil {
		errs.Write(w, err)
		return
	}
	lines := boundedLines(r.URL.Query().Get("lines"), 200)
	logPath, err := s.findComfyLog(r.Context(), active)
	if err != nil {
		errs.Write(w, errs.New(errs.CodeNotFound, http.StatusNotFound, "no ComfyUI log found"))
		return
	}
	out, err := s.runRemoteCommand(r.Context(), active.Target, "tail -n "+strconv.Itoa(lines)+" -- "+shellQuote(logPath), 10*time.Second)
	if err != nil {
		errs.Write(w, errs.New(errs.CodeInstanceUnreach, http.StatusBadGateway, "cannot read ComfyUI log: "+safeError(err)))
		return
	}
	writeJSON(w, http.StatusOK, comfyLogsResponse{Path: logPath, Text: redactText(string(out)), UpdatedAt: time.Now().UTC().Format(time.RFC3339)})
}

func (s *Server) comfyLogsStream(w http.ResponseWriter, r *http.Request) {
	active, err := s.loadActiveInstance(r.Context())
	if err != nil {
		errs.Write(w, err)
		return
	}
	flusher, ok := w.(http.Flusher)
	if !ok {
		errs.Write(w, errs.New(errs.CodeInternal, http.StatusInternalServerError, "streaming unsupported"))
		return
	}
	w.Header().Set("Content-Type", "text/event-stream; charset=utf-8")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")

	logPath, err := s.findComfyLog(r.Context(), active)
	if err != nil {
		writeSSE(w, "error", map[string]string{"message": "no ComfyUI log found"})
		flusher.Flush()
		return
	}
	writeSSE(w, "ready", map[string]string{"path": logPath})
	flusher.Flush()

	cli, err := s.SSH.Get(r.Context(), active.Target)
	if err != nil {
		writeSSE(w, "error", map[string]string{"message": "cannot connect to active instance"})
		flusher.Flush()
		return
	}
	sess, err := cli.NewSession()
	if err != nil {
		writeSSE(w, "error", map[string]string{"message": "cannot open SSH session"})
		flusher.Flush()
		return
	}
	defer sess.Close()
	stdout, err := sess.StdoutPipe()
	if err != nil {
		writeSSE(w, "error", map[string]string{"message": "cannot open log stream"})
		flusher.Flush()
		return
	}
	stderr, _ := sess.StderrPipe()
	cmd := "tail -n 50 -f -- " + shellQuote(logPath)
	if err := sess.Start(cmd); err != nil {
		writeSSE(w, "error", map[string]string{"message": "cannot start remote tail"})
		flusher.Flush()
		return
	}

	go func() {
		<-r.Context().Done()
		_ = sess.Close()
	}()

	if stderr != nil {
		go drainSSHStderr(stderr)
	}
	scanner := bufio.NewScanner(stdout)
	scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	for scanner.Scan() {
		writeSSE(w, "log", map[string]string{"line": redactText(scanner.Text())})
		flusher.Flush()
	}
	_ = sess.Wait()
}

func (s *Server) runRemoteCommand(ctx context.Context, target sshmgr.Target, cmd string, timeout time.Duration) ([]byte, error) {
	cmdCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	cli, err := s.SSH.Get(cmdCtx, target)
	if err != nil {
		return nil, err
	}
	sess, err := cli.NewSession()
	if err != nil {
		return nil, err
	}
	defer sess.Close()

	type result struct {
		out []byte
		err error
	}
	done := make(chan result, 1)
	go func() {
		out, err := sess.CombinedOutput(cmd)
		done <- result{out: out, err: err}
	}()

	select {
	case res := <-done:
		return res.out, res.err
	case <-cmdCtx.Done():
		_ = sess.Close()
		select {
		case res := <-done:
			if res.err != nil {
				return res.out, res.err
			}
			return res.out, cmdCtx.Err()
		case <-time.After(2 * time.Second):
			return nil, cmdCtx.Err()
		}
	}
}

func parseNvidiaXML(b []byte) ([]gpuInfo, error) {
	var doc struct {
		DriverVersion string `xml:"driver_version"`
		CUDAVersion   string `xml:"cuda_version"`
		GPUs          []struct {
			ProductName string `xml:"product_name"`
			UUID        string `xml:"uuid"`
			FBMemory    struct {
				Total string `xml:"total"`
				Used  string `xml:"used"`
			} `xml:"fb_memory_usage"`
			Utilization struct {
				GPU    string `xml:"gpu_util"`
				Memory string `xml:"memory_util"`
			} `xml:"utilization"`
			Temperature struct {
				GPU string `xml:"gpu_temp"`
			} `xml:"temperature"`
			PowerReadings struct {
				Draw  string `xml:"power_draw"`
				Limit string `xml:"power_limit"`
			} `xml:"power_readings"`
			GPUPowerReadings struct {
				Draw  string `xml:"power_draw"`
				Limit string `xml:"power_limit"`
			} `xml:"gpu_power_readings"`
		} `xml:"gpu"`
	}
	if err := xml.Unmarshal(b, &doc); err != nil {
		return nil, err
	}
	if len(doc.GPUs) == 0 {
		return nil, errors.New("no GPUs in nvidia-smi XML")
	}
	out := make([]gpuInfo, 0, len(doc.GPUs))
	for i, g := range doc.GPUs {
		powerDraw := g.PowerReadings.Draw
		powerLimit := g.PowerReadings.Limit
		if powerDraw == "" {
			powerDraw = g.GPUPowerReadings.Draw
		}
		if powerLimit == "" {
			powerLimit = g.GPUPowerReadings.Limit
		}
		out = append(out, gpuInfo{
			Index:             i,
			Name:              strings.TrimSpace(g.ProductName),
			UUID:              strings.TrimSpace(g.UUID),
			UtilizationGPU:    parseIntPtr(g.Utilization.GPU),
			UtilizationMemory: parseIntPtr(g.Utilization.Memory),
			MemoryTotalMiB:    parseIntPtr(g.FBMemory.Total),
			MemoryUsedMiB:     parseIntPtr(g.FBMemory.Used),
			TemperatureC:      parseIntPtr(g.Temperature.GPU),
			PowerDrawW:        parseFloatPtr(powerDraw),
			PowerLimitW:       parseFloatPtr(powerLimit),
			DriverVersion:     strings.TrimSpace(doc.DriverVersion),
			CUDAVersion:       strings.TrimSpace(doc.CUDAVersion),
		})
	}
	return out, nil
}

func parseNvidiaCSV(b []byte) ([]gpuInfo, error) {
	r := csv.NewReader(bytes.NewReader(b))
	r.TrimLeadingSpace = true
	recs, err := r.ReadAll()
	if err != nil {
		return nil, err
	}
	out := make([]gpuInfo, 0, len(recs))
	for _, rec := range recs {
		if len(rec) < 10 {
			continue
		}
		idx := 0
		if v := parseIntPtr(rec[0]); v != nil {
			idx = *v
		}
		out = append(out, gpuInfo{
			Index:             idx,
			Name:              strings.TrimSpace(rec[1]),
			UUID:              strings.TrimSpace(rec[2]),
			UtilizationGPU:    parseIntPtr(rec[3]),
			UtilizationMemory: parseIntPtr(rec[4]),
			MemoryTotalMiB:    parseIntPtr(rec[5]),
			MemoryUsedMiB:     parseIntPtr(rec[6]),
			TemperatureC:      parseIntPtr(rec[7]),
			PowerDrawW:        parseFloatPtr(rec[8]),
			PowerLimitW:       parseFloatPtr(rec[9]),
		})
	}
	if len(out) == 0 {
		return nil, errors.New("no GPUs in nvidia-smi CSV")
	}
	return out, nil
}

func buildComfyStatusCommand(active activeInstance) string {
	pattern := "[C]omfyUI|[m]ain.py"
	if strings.TrimSpace(active.ComfyRoot) != "" {
		pattern = active.ComfyRoot + "|" + pattern
	}
	host := active.ComfyHost
	if host == "" {
		host = "127.0.0.1"
	}
	port := strconv.Itoa(active.ComfyPort)
	return strings.Join([]string{
		"pids=$(pgrep -f " + shellQuote(pattern) + " 2>/dev/null | tr '\\n' ' ' || true)",
		"port_open=0",
		"if command -v nc >/dev/null 2>&1; then nc -z -w 1 " + shellQuote(host) + " " + port + " >/dev/null 2>&1 && port_open=1; fi",
		"if [ \"$port_open\" != 1 ] && command -v ss >/dev/null 2>&1; then ss -ltn 2>/dev/null | grep -Eq '[:.]" + port + "[[:space:]]' && port_open=1; fi",
		"printf 'PIDS=%s\\nPORT_OPEN=%s\\n' \"$pids\" \"$port_open\"",
	}, "; ")
}

func parseComfyStatusOutput(out string) ([]int, bool) {
	var pids []int
	portOpen := false
	for _, line := range strings.Split(out, "\n") {
		key, val, ok := strings.Cut(line, "=")
		if !ok {
			continue
		}
		switch strings.TrimSpace(key) {
		case "PIDS":
			for _, f := range strings.Fields(val) {
				pid, err := strconv.Atoi(f)
				if err == nil && pid > 0 {
					pids = append(pids, pid)
				}
			}
		case "PORT_OPEN":
			portOpen = strings.TrimSpace(val) == "1"
		}
	}
	return pids, portOpen
}

func (s *Server) findComfyLog(ctx context.Context, active activeInstance) (string, error) {
	root := strings.TrimRight(strings.TrimSpace(active.ComfyRoot), "/")
	if root == "" {
		return "", errors.New("comfy root not configured")
	}
	paths := []string{root + "/comfyui.log", root + "/output.log", root + "/logs/comfyui.log"}
	parts := make([]string, 0, len(paths)+1)
	for _, p := range paths {
		parts = append(parts, "if [ -f "+shellQuote(p)+" ]; then printf '%s\\n' "+shellQuote(p)+"; exit 0; fi")
	}
	parts = append(parts, "exit 1")
	out, err := s.runRemoteCommand(ctx, active.Target, strings.Join(parts, "; "), 5*time.Second)
	if err != nil {
		return "", err
	}
	path := strings.TrimSpace(string(out))
	if path == "" {
		return "", errors.New("no log found")
	}
	return path, nil
}

func boundedLines(raw string, def int) int {
	if raw == "" {
		return def
	}
	n, err := strconv.Atoi(raw)
	if err != nil || n <= 0 {
		return def
	}
	if n > 2000 {
		return 2000
	}
	return n
}

func prefixWithCD(root, cmd string) string {
	root = strings.TrimSpace(root)
	if root == "" {
		return cmd
	}
	return "cd " + shellQuote(root) + " && " + cmd
}

func parseIntPtr(s string) *int {
	f := firstNumber(s)
	if f == "" {
		return nil
	}
	v, err := strconv.ParseFloat(f, 64)
	if err != nil {
		return nil
	}
	i := int(v)
	return &i
}

func parseFloatPtr(s string) *float64 {
	f := firstNumber(s)
	if f == "" {
		return nil
	}
	v, err := strconv.ParseFloat(f, 64)
	if err != nil {
		return nil
	}
	return &v
}

func firstNumber(s string) string {
	s = strings.TrimSpace(s)
	start := -1
	end := -1
	for i, r := range s {
		if start == -1 {
			if unicode.IsDigit(r) || r == '-' || r == '+' || r == '.' {
				start = i
				end = i + len(string(r))
			}
			continue
		}
		if unicode.IsDigit(r) || r == '.' {
			end = i + len(string(r))
			continue
		}
		break
	}
	if start == -1 {
		return ""
	}
	return s[start:end]
}

func writeSSE(w io.Writer, event string, v any) {
	b, _ := json.Marshal(v)
	_, _ = fmt.Fprintf(w, "event: %s\ndata: %s\n\n", event, b)
}

func drainSSHStderr(r io.Reader) {
	_, _ = io.Copy(io.Discard, r)
}

func safeError(err error) string {
	if err == nil {
		return ""
	}
	return redactText(err.Error())
}

func redactText(s string) string {
	if s == "" {
		return s
	}
	words := strings.Fields(s)
	for _, word := range words {
		lower := strings.ToLower(word)
		if strings.Contains(lower, "password=") || strings.Contains(lower, "passwd=") || strings.Contains(lower, "token=") || strings.Contains(lower, "secret=") || strings.Contains(lower, "api_key=") || strings.Contains(lower, "apikey=") {
			s = strings.ReplaceAll(s, word, redactAssignment(word))
		}
	}
	return s
}

func redactAssignment(s string) string {
	idx := strings.Index(s, "=")
	if idx < 0 {
		return "[REDACTED]"
	}
	return s[:idx+1] + "[REDACTED]"
}
