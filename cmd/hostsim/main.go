//go:build windows

// Command hostsim exercises a CPA plugin DLL without starting the gateway.
// It implements the minimal host side of the C ABI: plugin.register,
// model.static and executor.execute_stream.
package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"strings"
	"syscall"
	"time"
	"unsafe"
)

type cBuffer struct {
	ptr uintptr
	len uintptr
}

type hostAPI struct {
	abiVersion uint32
	_          uint32
	hostCtx    uintptr
	call       uintptr
	freeBuffer uintptr
}

type pluginAPI struct {
	abiVersion uint32
	_          uint32
	call       uintptr
	freeBuffer uintptr
	shutdown   uintptr
}

var (
	kernel32       = syscall.NewLazyDLL("kernel32.dll")
	procLocalAlloc = kernel32.NewProc("LocalAlloc")
	procLocalFree  = kernel32.NewProc("LocalFree")
	streamDone     = make(chan error, 1)
)

func main() {
	dllPath := flag.String("dll", `bin\xiaoyiwork-v0.1.0.dll`, "plugin DLL path")
	model := flag.String("model", "xiaoyiwork/GLM_5.3_FLASH_VE", "client-facing model id")
	prompt := flag.String("prompt", "Say OK", "user prompt")
	stream := flag.Bool("stream", true, "exercise the streaming executor")
	tools := flag.Bool("tools", false, "attach one get_weather tool definition")
	flag.Parse()

	dll, err := syscall.LoadDLL(*dllPath)
	if err != nil {
		fail("LoadDLL: %v", err)
	}
	defer dll.Release()

	procInit, err := dll.FindProc("cliproxy_plugin_init")
	if err != nil {
		fail("FindProc init: %v", err)
	}
	procCall, err := dll.FindProc("cliproxyPluginCall")
	if err != nil {
		fail("FindProc call: %v", err)
	}
	procFree, err := dll.FindProc("cliproxyPluginFree")
	if err != nil {
		fail("FindProc free: %v", err)
	}

	host := hostAPI{
		abiVersion: 1,
		call:       syscall.NewCallback(hostCall),
		freeBuffer: syscall.NewCallback(hostFree),
	}
	var plugin pluginAPI
	rc, _, _ := procInit.Call(uintptr(unsafe.Pointer(&host)), uintptr(unsafe.Pointer(&plugin)))
	if rc != 0 {
		fail("plugin init failed: %d", rc)
	}
	if plugin.call == 0 {
		fail("plugin returned no call entry")
	}
	fmt.Println("plugin initialised, abi =", plugin.abiVersion)

	call := func(method string, request any) map[string]any {
		var raw []byte
		if request != nil {
			raw, _ = json.Marshal(request)
		}
		resp, err := callPlugin(procCall, procFree, plugin, method, raw)
		if err != nil {
			fail("%s: %v", method, err)
		}
		var envelope map[string]any
		if err := json.Unmarshal(resp, &envelope); err != nil {
			fail("%s: decode envelope: %v (%s)", method, err, string(resp))
		}
		if ok, _ := envelope["ok"].(bool); !ok {
			pretty, _ := json.Marshal(envelope)
			fail("%s: plugin error: %s", method, string(pretty))
		}
		result, _ := envelope["result"].(map[string]any)
		return result
	}

	registration := call("plugin.register", map[string]any{})
	pretty, _ := json.MarshalIndent(registration, "", "  ")
	fmt.Printf("registration: %s\n", pretty)

	catalog := call("model.static", map[string]any{})
	pretty, _ = json.MarshalIndent(catalog, "", "  ")
	fmt.Printf("catalog: %s\n", pretty)

	requestBody := map[string]any{
		"model":    *model,
		"messages": []map[string]any{{"role": "user", "content": *prompt}},
		"stream":   true,
	}
	if *tools {
		requestBody["tools"] = []map[string]any{{
			"type": "function",
			"function": map[string]any{
				"name":        "get_weather",
				"description": "Get the current weather for a city",
				"parameters": map[string]any{
					"type":       "object",
					"properties": map[string]any{"city": map[string]any{"type": "string"}},
					"required":   []string{"city"},
				},
			},
		}}
	}
	payload, _ := json.Marshal(requestBody)
	request := map[string]any{
		"Model":        *model,
		"Payload":      payload,
		"Stream":       *stream,
		"StreamID":     "hostsim-1",
		"Format":       "chat-completions",
		"SourceFormat": "chat-completions",
	}
	method := "executor.execute_stream"
	if !*stream {
		method = "executor.execute"
	}
	resp, err := callPlugin(procCall, procFree, plugin, method, mustJSON(request))
	if err != nil {
		fail("%s: %v", method, err)
	}
	if !*stream {
		fmt.Printf("non-stream response: %s\n", string(resp))
		return
	}

	select {
	case err := <-streamDone:
		if err != nil {
			fail("stream closed with error: %v", err)
		}
		fmt.Println("stream closed cleanly")
	case <-time.After(120 * time.Second):
		fail("stream did not close within 120s")
	}
}

func callPlugin(procCall, procFree *syscall.Proc, plugin pluginAPI, method string, request []byte) ([]byte, error) {
	methodBytes := append([]byte(method), 0)
	var reqPtr uintptr
	if len(request) > 0 {
		reqPtr = uintptr(unsafe.Pointer(&request[0]))
	}
	var resp cBuffer
	rc, _, _ := procCall.Call(
		uintptr(unsafe.Pointer(&methodBytes[0])),
		reqPtr,
		uintptr(len(request)),
		uintptr(unsafe.Pointer(&resp)),
	)
	if rc != 0 {
		return nil, fmt.Errorf("plugin call returned %d", rc)
	}
	if resp.ptr == 0 || resp.len == 0 {
		return []byte(`{"ok":true}`), nil
	}
	out := unsafe.Slice((*byte)(unsafe.Pointer(resp.ptr)), int(resp.len))
	copied := append([]byte(nil), out...)
	procFree.Call(resp.ptr, resp.len)
	return copied, nil
}

func hostCall(_ uintptr, methodPtr uintptr, reqPtr uintptr, reqLen uintptr, respPtr uintptr) uintptr {
	defer func() {
		if r := recover(); r != nil {
			fmt.Fprintf(os.Stderr, "host callback panic: %v\n", r)
		}
	}()
	method := readCString(methodPtr)
	var request []byte
	if reqPtr != 0 && reqLen > 0 {
		request = unsafe.Slice((*byte)(unsafe.Pointer(reqPtr)), int(reqLen))
	}
	var result any = map[string]any{}
	switch method {
	case "host.stream.emit":
		var chunk struct {
			StreamID string `json:"stream_id"`
			Payload  []byte `json:"payload"`
		}
		_ = json.Unmarshal(request, &chunk)
		fmt.Print(string(chunk.Payload))
	case "host.stream.close":
		var chunk struct {
			StreamID string `json:"stream_id"`
			Error    string `json:"error"`
		}
		_ = json.Unmarshal(request, &chunk)
		if strings.TrimSpace(chunk.Error) != "" {
			streamDone <- fmt.Errorf("%s", chunk.Error)
		} else {
			streamDone <- nil
		}
	case "host.log":
		var entry struct {
			Level   string            `json:"level"`
			Message string            `json:"message"`
			Fields  map[string]string `json:"fields"`
		}
		_ = json.Unmarshal(request, &entry)
		fmt.Fprintf(os.Stderr, "[%s] %s %v\n", entry.Level, entry.Message, entry.Fields)
	case "host.auth.list":
		result = map[string]any{"files": []any{}}
	}

	envelope, _ := json.Marshal(map[string]any{"ok": true, "result": result})
	writeBuffer(respPtr, envelope)
	return 0
}

func hostFree(ptr uintptr, _ uintptr) uintptr {
	if ptr != 0 {
		procLocalFree.Call(ptr)
	}
	return 0
}

func writeBuffer(respPtr uintptr, payload []byte) {
	if respPtr == 0 || len(payload) == 0 {
		return
	}
	mem, _, _ := procLocalAlloc.Call(0x0040, uintptr(len(payload)))
	if mem == 0 {
		return
	}
	copy(unsafe.Slice((*byte)(unsafe.Pointer(mem)), len(payload)), payload)
	resp := (*cBuffer)(unsafe.Pointer(respPtr))
	resp.ptr = mem
	resp.len = uintptr(len(payload))
}

func readCString(ptr uintptr) string {
	if ptr == 0 {
		return ""
	}
	var out []byte
	for i := 0; i < 4096; i++ {
		b := *(*byte)(unsafe.Pointer(ptr + uintptr(i)))
		if b == 0 {
			break
		}
		out = append(out, b)
	}
	return string(out)
}

func mustJSON(v any) []byte {
	raw, _ := json.Marshal(v)
	return raw
}

func fail(format string, args ...any) {
	fmt.Fprintf(os.Stderr, format+"\n", args...)
	os.Exit(1)
}
