//go:build js && wasm

// Command wasm is compiled to WebAssembly and provides a subset of
// goroscope's analysis engine for offline use in the browser.
//
// Exposed JavaScript functions (on the global `goroscope` object):
//
//	goroscope.analyzeCapture(captureJSON: string) → string (JSON)
//
// The returned JSON has the shape:
//
//	{
//	  "goroutines": [...],
//	  "insights":   [...],
//	  "timeline":   [...],
//	  "annotations": [...],
//	  "error":      ""       // non-empty on failure
//	}
package main

import (
	"encoding/json"
	"syscall/js"

	"github.com/Khachatur86/goroscope/internal/analysis"
	"github.com/Khachatur86/goroscope/internal/session"
	"github.com/Khachatur86/goroscope/internal/tracebridge"
)

func main() {
	obj := js.Global().Get("Object").New()
	obj.Set("analyzeCapture", js.FuncOf(jsAnalyzeCapture))
	js.Global().Set("goroscope", obj)

	// Block forever — WASM must stay alive for callbacks to work.
	select {}
}

// jsAnalyzeCapture is the JS-callable wrapper for analyzeCapture.
func jsAnalyzeCapture(this js.Value, args []js.Value) any {
	if len(args) == 0 {
		return errorResult("missing capture JSON argument")
	}
	result := analyzeCapture(args[0].String())
	data, err := json.Marshal(result)
	if err != nil {
		return errorResult("marshal result: " + err.Error())
	}
	return string(data)
}

type analysisResult struct {
	Goroutines  any    `json:"goroutines"`
	Insights    any    `json:"insights"`
	Timeline    any    `json:"timeline"`
	Annotations any    `json:"annotations"`
	Error       string `json:"error"`
}

func errorResult(msg string) string {
	data, _ := json.Marshal(analysisResult{Error: msg})
	return string(data)
}

func analyzeCapture(captureJSON string) analysisResult {
	capture, err := tracebridge.LoadCaptureFromBytes([]byte(captureJSON))
	if err != nil {
		return analysisResult{Error: "parse capture: " + err.Error()}
	}

	eng := analysis.NewEngine()
	sessions := session.NewManager()
	sess := sessions.StartSession("offline", "wasm")

	bound := tracebridge.BindCaptureSession(capture, sess.ID)
	eng.LoadCapture(sess, bound)

	goroutines := eng.ListGoroutines()
	timeline := eng.Timeline()
	insights := analysis.GenerateInsights(analysis.GenerateInsightsInput{
		Goroutines: goroutines,
		Segments:   timeline,
		Edges:      eng.ResourceGraph(),
	})

	return analysisResult{
		Goroutines:  goroutines,
		Insights:    insights,
		Timeline:    timeline,
		Annotations: capture.Annotations,
	}
}
