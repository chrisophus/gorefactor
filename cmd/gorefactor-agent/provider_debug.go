package main

import (
	"fmt"
	"os"
	"time"
)

// providerDebug, when true (set from -verbose), makes the LLM provider
// layer print one diagnostic line per HTTP round-trip to stderr:
// endpoint, model, payload sizes, status, elapsed, token usage. This is
// the sensor that answers "is the local/remote model actually
// responding?" -- without it a hung or misconfigured Ollama looks
// identical to a slow model.
var providerDebug bool

// provDebugf writes a timestamped PROVIDER line to stderr when
// providerDebug is on; a cheap no-op otherwise.
func provDebugf(format string, a ...any) {
	if !providerDebug {
		return
	}
	fmt.Fprintf(os.Stderr, "[provider %s] %s\n",
		time.Now().Format("15:04:05.000"), fmt.Sprintf(format, a...))
}
