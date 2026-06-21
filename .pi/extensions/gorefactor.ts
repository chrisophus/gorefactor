import type { ExtensionAPI, ExtensionContext } from "@earendil-works/pi-coding-agent";
import { Type } from "typebox";
import { execSync } from "child_process";

export default function (pi: ExtensionAPI) {
  // Register gorefactor analysis tools
  pi.registerTool({
    name: "gorefactor_parse",
    label: "Parse Go File",
    description: "Parse a Go file and return its AST structure as JSON",
    parameters: Type.Object({
      file: Type.String({ description: "Path to Go file to parse" }),
    }),
    async execute(toolCallId, params, signal, onUpdate, ctx) {
      try {
        const result = execSync(`gorefactor parse "${params.file}"`, {
          encoding: "utf-8",
          cwd: ctx.workingDir,
        });
        return {
          content: [{ type: "text", text: result }],
          details: { success: true },
        };
      } catch (error: any) {
        return {
          content: [
            {
              type: "text",
              text: `Error: ${error.message || String(error)}`,
            },
          ],
          details: { success: false, error: String(error) },
        };
      }
    },
  });

  pi.registerTool({
    name: "gorefactor_recommend",
    label: "Recommend Extractions",
    description:
      "Analyze a Go file and recommend extraction candidates with complexity scores",
    parameters: Type.Object({
      file: Type.String({ description: "Path to Go file to analyze" }),
      short: Type.Optional(
        Type.Boolean({ description: "Concise output (default: false)" })
      ),
    }),
    async execute(toolCallId, params, signal, onUpdate, ctx) {
      try {
        const cmd = `gorefactor recommend "${params.file}"${params.short ? " --short" : ""}`;
        const result = execSync(cmd, {
          encoding: "utf-8",
          cwd: ctx.workingDir,
        });
        return {
          content: [{ type: "text", text: result }],
          details: { success: true },
        };
      } catch (error: any) {
        return {
          content: [
            {
              type: "text",
              text: `Error: ${error.message || String(error)}`,
            },
          ],
          details: { success: false, error: String(error) },
        };
      }
    },
  });

  pi.registerTool({
    name: "gorefactor_find_callers",
    label: "Find Callers",
    description: "Find all places that call a function or method",
    parameters: Type.Object({
      target: Type.String({
        description:
          'Function name or "Receiver:Method" (e.g., "Parser:Parse")',
      }),
      json: Type.Optional(Type.Boolean({ description: "JSON output" })),
    }),
    async execute(toolCallId, params, signal, onUpdate, ctx) {
      try {
        const cmd = `gorefactor find-callers "${params.target}"${params.json ? " --json" : ""}`;
        const result = execSync(cmd, {
          encoding: "utf-8",
          cwd: ctx.workingDir,
        });
        return {
          content: [{ type: "text", text: result }],
          details: { success: true },
        };
      } catch (error: any) {
        return {
          content: [
            {
              type: "text",
              text: `Error: ${error.message || String(error)}`,
            },
          ],
          details: { success: false, error: String(error) },
        };
      }
    },
  });

  pi.registerTool({
    name: "gorefactor_find_uses",
    label: "Find Uses",
    description: "Find all places where a symbol is used",
    parameters: Type.Object({
      symbol: Type.String({
        description: 'Symbol name or "Receiver:Method"',
      }),
      json: Type.Optional(Type.Boolean({ description: "JSON output" })),
    }),
    async execute(toolCallId, params, signal, onUpdate, ctx) {
      try {
        const cmd = `gorefactor find-uses "${params.symbol}"${params.json ? " --json" : ""}`;
        const result = execSync(cmd, {
          encoding: "utf-8",
          cwd: ctx.workingDir,
        });
        return {
          content: [{ type: "text", text: result }],
          details: { success: true },
        };
      } catch (error: any) {
        return {
          content: [
            {
              type: "text",
              text: `Error: ${error.message || String(error)}`,
            },
          ],
          details: { success: false, error: String(error) },
        };
      }
    },
  });

  pi.registerTool({
    name: "gorefactor_find_implementations",
    label: "Find Implementations",
    description: "Find all types that implement an interface",
    parameters: Type.Object({
      interface: Type.String({ description: "Interface name" }),
      json: Type.Optional(Type.Boolean({ description: "JSON output" })),
    }),
    async execute(toolCallId, params, signal, onUpdate, ctx) {
      try {
        const cmd = `gorefactor find-implementations "${params.interface}"${params.json ? " --json" : ""}`;
        const result = execSync(cmd, {
          encoding: "utf-8",
          cwd: ctx.workingDir,
        });
        return {
          content: [{ type: "text", text: result }],
          details: { success: true },
        };
      } catch (error: any) {
        return {
          content: [
            {
              type: "text",
              text: `Error: ${error.message || String(error)}`,
            },
          ],
          details: { success: false, error: String(error) },
        };
      }
    },
  });

  // Mutation tools
  pi.registerTool({
    name: "gorefactor_create",
    label: "Create Go File",
    description: "Create a new Go file with the given content",
    parameters: Type.Object({
      path: Type.String({ description: "Path for new Go file" }),
      content: Type.String({ description: "Go code content" }),
    }),
    async execute(toolCallId, params, signal, onUpdate, ctx) {
      try {
        const result = execSync(
          `gorefactor create "${params.path}" -`,
          {
            encoding: "utf-8",
            input: params.content,
            cwd: ctx.workingDir,
          }
        );
        return {
          content: [{ type: "text", text: result }],
          details: { success: true, file: params.path },
        };
      } catch (error: any) {
        return {
          content: [
            {
              type: "text",
              text: `Error: ${error.message || String(error)}`,
            },
          ],
          details: { success: false, error: String(error) },
        };
      }
    },
  });

  pi.registerTool({
    name: "gorefactor_extract",
    label: "Extract Method",
    description:
      "Extract a code block into a new method (semantic-based extraction)",
    parameters: Type.Object({
      file: Type.String({ description: "Go file path" }),
      startLine: Type.Number({ description: "Start line number" }),
      endLine: Type.Number({ description: "End line number" }),
      methodName: Type.String({ description: "Name for extracted method" }),
    }),
    async execute(toolCallId, params, signal, onUpdate, ctx) {
      try {
        const cmd = `gorefactor extract "${params.file}" ${params.startLine} ${params.endLine} "${params.methodName}"`;
        const result = execSync(cmd, {
          encoding: "utf-8",
          cwd: ctx.workingDir,
        });
        return {
          content: [{ type: "text", text: result }],
          details: { success: true, method: params.methodName },
        };
      } catch (error: any) {
        return {
          content: [
            {
              type: "text",
              text: `Error: ${error.message || String(error)}`,
            },
          ],
          details: { success: false, error: String(error) },
        };
      }
    },
  });

  pi.registerTool({
    name: "gorefactor_delete",
    label: "Delete Declaration",
    description: "Delete a function, method, or type declaration (with safety check)",
    parameters: Type.Object({
      file: Type.String({ description: "Go file path" }),
      target: Type.String({
        description:
          'Declaration name or "Receiver:Method" (e.g., "Parser:Parse")',
      }),
      safe: Type.Optional(
        Type.Boolean({
          description:
            "Check for callers before deleting (default: true)",
        })
      ),
    }),
    async execute(toolCallId, params, signal, onUpdate, ctx) {
      try {
        const cmd = `gorefactor delete "${params.file}" "${params.target}"${params.safe !== false ? " --safe" : ""}`;
        const result = execSync(cmd, {
          encoding: "utf-8",
          cwd: ctx.workingDir,
        });
        return {
          content: [{ type: "text", text: result }],
          details: { success: true, deleted: params.target },
        };
      } catch (error: any) {
        return {
          content: [
            {
              type: "text",
              text: `Error: ${error.message || String(error)}`,
            },
          ],
          details: { success: false, error: String(error) },
        };
      }
    },
  });

  pi.registerTool({
    name: "gorefactor_move",
    label: "Move Declaration",
    description: "Move a function or method to a different file",
    parameters: Type.Object({
      source: Type.String({ description: "Source Go file" }),
      target: Type.String({
        description:
          'Declaration name or "Receiver:Method" (e.g., "Parser:Parse")',
      }),
      dest: Type.String({ description: "Destination Go file" }),
    }),
    async execute(toolCallId, params, signal, onUpdate, ctx) {
      try {
        const cmd = `gorefactor move "${params.source}" "${params.target}" "${params.dest}"`;
        const result = execSync(cmd, {
          encoding: "utf-8",
          cwd: ctx.workingDir,
        });
        return {
          content: [{ type: "text", text: result }],
          details: { success: true, moved: params.target },
        };
      } catch (error: any) {
        return {
          content: [
            {
              type: "text",
              text: `Error: ${error.message || String(error)}`,
            },
          ],
          details: { success: false, error: String(error) },
        };
      }
    },
  });

  pi.registerTool({
    name: "gorefactor_lint",
    label: "Lint Code Quality",
    description: "Check code quality and find issues (optional autofix)",
    parameters: Type.Object({
      path: Type.Optional(
        Type.String({
          description: "Path to check (default: current directory)",
        })
      ),
      fix: Type.Optional(
        Type.Boolean({
          description: "Automatically fix safe issues",
        })
      ),
    }),
    async execute(toolCallId, params, signal, onUpdate, ctx) {
      try {
        const cmd = `gorefactor lint "${params.path || "."}"${params.fix ? " --fix" : ""}`;
        const result = execSync(cmd, {
          encoding: "utf-8",
          cwd: ctx.workingDir,
        });
        return {
          content: [{ type: "text", text: result }],
          details: { success: true },
        };
      } catch (error: any) {
        return {
          content: [
            {
              type: "text",
              text: `Error: ${error.message || String(error)}`,
            },
          ],
          details: { success: false, error: String(error) },
        };
      }
    },
  });

  pi.registerTool({
    name: "gorefactor_doctor",
    label: "Quality Gate",
    description: "Run full quality gate: lint + build + test",
    parameters: Type.Object({
      dir: Type.Optional(
        Type.String({
          description: "Directory to check (default: current)",
        })
      ),
    }),
    async execute(toolCallId, params, signal, onUpdate, ctx) {
      try {
        const cmd = `gorefactor doctor${params.dir ? ` "${params.dir}"` : ""}`;
        const result = execSync(cmd, {
          encoding: "utf-8",
          cwd: ctx.workingDir,
        });
        return {
          content: [{ type: "text", text: result }],
          details: { success: true },
        };
      } catch (error: any) {
        return {
          content: [
            {
              type: "text",
              text: `Error: ${error.message || String(error)}`,
            },
          ],
          details: { success: false, error: String(error) },
        };
      }
    },
  });

  // Startup notification
  pi.on("session_start", async (_event, ctx) => {
    ctx.ui.notify(
      "GoRefactor tools loaded. Use for .go file mutations instead of Write/Edit.",
      "info"
    );
  });
}
