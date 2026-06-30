package main

import (
	"context"
	"fmt"
	"strings"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// Phase 4 of docs/mcp-server-plan.md (resources): expose gorefactor's
// token-cheap context packs as MCP *resources* the client can pull on demand,
// rather than only as tool calls. A resource read is a lighter-weight,
// cacheable interaction than a tool call, which fits these read-only,
// parameter-by-URI summaries:
//
//	gorefactor://skeleton/{path}   -> `skeleton <path>` (file shape, bodies elided)
//	gorefactor://inspect/{path}    -> `inspect <path>`  (one-page file summary)
//	gorefactor://context/{symbol}  -> `context <symbol>`(LLM context pack for a symbol)
//
// Each template is backed by the same registered Command the equivalent tool
// uses, so there is one source of truth for the behaviour. Resources are
// always registered (read or write mode) because they never mutate.

// mcpResource describes one resource template: the URI scheme path it serves,
// the backing command, and the MIME type of the produced text.
type mcpResource struct {
	// name is the backing command name (e.g. "skeleton").
	name string
	// segment is the URI path segment, e.g. "skeleton" in
	// gorefactor://skeleton/{path}.
	segment string
	// argReserved is true when the templated argument may contain "/"
	// (a file path), which needs RFC 6570 reserved expansion ({+x}).
	argReserved bool
	// mime is the MIME type reported for the resource contents.
	mime string
	// title/description are surfaced in resources/list for the client/LLM.
	title       string
	description string
}

var mcpResources = []mcpResource{
	{
		name:        "skeleton",
		segment:     "skeleton",
		argReserved: true,
		mime:        "text/plain",
		title:       "File skeleton",
		description: "Token-cheap shape of a Go file: declarations with function bodies elided. URI: gorefactor://skeleton/<path>",
	},
	{
		name:        "inspect",
		segment:     "inspect",
		argReserved: true,
		mime:        "text/markdown",
		title:       "File summary",
		description: "One-page summary of a Go file: declarations, sizes, lint hints, extraction candidates. URI: gorefactor://inspect/<path>",
	},
	{
		name:        "context",
		segment:     "context",
		argReserved: false,
		mime:        "text/plain",
		title:       "Symbol context pack",
		description: "LLM context pack for a symbol: definition, callers, signature types, tests. URI: gorefactor://context/<Symbol|Receiver:Method>",
	},
}

const mcpResourceScheme = "gorefactor://"

// registerMCPResources installs the resource templates on the server. Each
// template routes a gorefactor://<segment>/<arg> URI to its backing command,
// runs the command with stdout captured (no --json: resources serve the
// human/LLM-readable form), and returns the text as the resource contents.
func registerMCPResources(server *mcp.Server, cmds map[string]Command) {
	for _, r := range mcpResources {
		cmd, ok := cmds[r.name]
		if !ok {
			continue
		}
		expansion := "{symbol}"
		if r.argReserved {
			// Reserved expansion ("{+var}") so file paths containing "/" match.
			expansion = "{+path}"
		}
		template := &mcp.ResourceTemplate{
			Name:        r.name,
			Title:       r.title,
			Description: r.description,
			MIMEType:    r.mime,
			URITemplate: mcpResourceScheme + r.segment + "/" + expansion,
		}
		server.AddResourceTemplate(template, resourceHandler(cmd, r))
	}
}

// resourceHandler adapts a Command into an MCP ResourceHandler. It extracts the
// single argument from the request URI (everything after
// "gorefactor://<segment>/"), runs the command, and returns the captured text.
func resourceHandler(cmd Command, r mcpResource) mcp.ResourceHandler {
	prefix := mcpResourceScheme + r.segment + "/"
	return func(_ context.Context, req *mcp.ReadResourceRequest) (*mcp.ReadResourceResult, error) {
		uri := req.Params.URI
		arg := strings.TrimPrefix(uri, prefix)
		if arg == "" || arg == uri {
			return nil, fmt.Errorf("invalid resource URI %q (want %s<arg>)", uri, prefix)
		}

		var runErr error
		output := captureStdoutOf(func() {
			runErr = cmd.Run([]string{arg})
		})
		if runErr != nil {
			return nil, fmt.Errorf("%s %s: %w", r.name, arg, runErr)
		}

		return &mcp.ReadResourceResult{
			Contents: []*mcp.ResourceContents{{
				URI:      uri,
				MIMEType: r.mime,
				Text:     output,
			}},
		}, nil
	}
}
