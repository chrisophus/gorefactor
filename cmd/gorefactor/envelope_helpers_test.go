package main

import (
	"encoding/json"
	"testing"
)

// decodeEnvelope unmarshals the shared {ok, error, data} frame from a command's
// --json output, failing the test on malformed JSON or ok=false. When target is
// non-nil, the data payload is decoded into it.
func decodeEnvelope(t *testing.T, out string, target any) {
	t.Helper()
	var env struct {
		OK    bool            `json:"ok"`
		Error string          `json:"error"`
		Data  json.RawMessage `json:"data"`
	}
	if err := json.Unmarshal([]byte(out), &env); err != nil {
		t.Fatalf("invalid envelope JSON: %v\n%s", err, out)
	}
	if !env.OK {
		t.Fatalf("envelope ok=false, error=%q\n%s", env.Error, out)
	}
	if target != nil {
		if err := json.Unmarshal(env.Data, target); err != nil {
			t.Fatalf("envelope data does not decode: %v\n%s", err, out)
		}
	}
}
