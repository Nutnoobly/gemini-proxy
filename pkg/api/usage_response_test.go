package api

import (
	"encoding/json"
	"testing"
)

func TestRequestPreservesUsageOptIn(t *testing.T) {
	var req ChatCompletionRequest
	if err := json.Unmarshal([]byte(`{"stream":true,"stream_options":{"include_usage":true}}`), &req); err != nil {
		t.Fatal(err)
	}
	data, err := json.Marshal(req)
	if err != nil {
		t.Fatal(err)
	}
	var got map[string]any
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatal(err)
	}
	options, ok := got["stream_options"].(map[string]any)
	if !ok || options["include_usage"] != true {
		t.Fatalf("stream usage opt-in lost: %s", data)
	}
}

func TestUnknownNonStreamingUsageIsOmitted(t *testing.T) {
	data, err := json.Marshal(ChatCompletionResponse{})
	if err != nil {
		t.Fatal(err)
	}
	var got map[string]any
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatal(err)
	}
	if _, ok := got["usage"]; ok {
		t.Fatalf("unknown usage must not look like measured zeros: %s", data)
	}
}
