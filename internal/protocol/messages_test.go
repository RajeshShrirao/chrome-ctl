package protocol

import (
	"encoding/json"
	"testing"
)

func TestViewportInfoFloatJSON(t *testing.T) {
	raw := `{"scrollY":123.4,"viewportHeight":768.5,"totalHeight":2048.75,"percent":45.6}`
	var v ViewportInfo
	if err := json.Unmarshal([]byte(raw), &v); err != nil {
		t.Fatalf("unmarshal ViewportInfo: %v", err)
	}

	if v.ScrollY != 123.4 || v.ViewportHeight != 768.5 || v.TotalHeight != 2048.75 || v.Percent != 45.6 {
		t.Errorf("unexpected ViewportInfo: %+v", v)
	}

	// Verify SnapResult also embeds ViewportInfo with floats correctly.
	snapRaw := `{"tabId":1,"title":"Test","url":"http://test","docId":"d1","snapshotId":1,"viewport":{"scrollY":10.5,"viewportHeight":100.5,"totalHeight":200.5,"percent":5.25},"elements":[],"formattedText":""}`
	var snap SnapResult
	if err := json.Unmarshal([]byte(snapRaw), &snap); err != nil {
		t.Fatalf("unmarshal SnapResult: %v", err)
	}
	if snap.Viewport.ScrollY != 10.5 {
		t.Errorf("unexpected snap.Viewport.ScrollY: %f", snap.Viewport.ScrollY)
	}
}

func TestActionScrollResultFloatJSON(t *testing.T) {
	raw := `{"scrolledPixels":720.5,"newScrollY":720.5,"viewportPercent":17.25}`
	var res ActionScrollResult
	if err := json.Unmarshal([]byte(raw), &res); err != nil {
		t.Fatalf("unmarshal ActionScrollResult: %v", err)
	}

	if res.ScrolledPixels != 720.5 || res.NewScrollY != 720.5 || res.ViewportPercent != 17.25 {
		t.Errorf("unexpected ActionScrollResult: %+v", res)
	}
}

func TestPageWaitParamsLongPoll(t *testing.T) {
	p := PageWaitParams{
		Selector:   "#submit",
		TimeoutMs:  5000,
		LongPollMs: 250,
	}

	data, err := json.Marshal(p)
	if err != nil {
		t.Fatalf("marshal PageWaitParams: %v", err)
	}

	var decoded PageWaitParams
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("unmarshal PageWaitParams: %v", err)
	}

	if decoded.LongPollMs != 250 {
		t.Errorf("LongPollMs = %d, want 250", decoded.LongPollMs)
	}

	// Test omitempty when LongPollMs is 0.
	zeroParams := PageWaitParams{TimeoutMs: 1000}
	zeroData, err := json.Marshal(zeroParams)
	if err != nil {
		t.Fatalf("marshal zeroParams: %v", err)
	}
	if string(zeroData) != `{"timeoutMs":1000}` {
		t.Errorf("expected omitted longPollMs, got %s", string(zeroData))
	}
}
