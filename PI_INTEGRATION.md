# Pi Integration for GoRefactor

This document summarizes how pi (coding agent harness) has been configured to use GoRefactor for Go editing tasks.

## Overview

Pi is now configured to **prefer GoRefactor commands over Write/Edit for all `.go` file mutations**. This integration includes:

1. **AGENTS.md** - Project context file (auto-loaded by pi)
2. **GoRefactor Skill** - Reference guide for best practices
3. **GoRefactor Extension** - Registers gorefactor as pi tools
4. **Project Settings** - Enables skill and extension by default

## Files Added/Modified

```
gorefactor/
├── AGENTS.md                              ← NEW: Agent rules for all LLM harnesses
├── .pi/
│   ├── README.md                          ← NEW: Pi integration guide
│   ├── settings.json                      ← NEW: Project configuration
│   ├── extensions/
│   │   └── gorefactor.ts                  ← NEW: Pi extension
│   └── skills/
│       └── gorefactor/
│           └── SKILL.md                   ← NEW: Detailed reference
└── CLAUDE.md                              ← EXISTING: Detailed architecture guide
```

## How It Works

### 1. Auto-Loaded Context (AGENTS.md)

When you start pi in the gorefactor directory:
```bash
cd /path/to/gorefactor
pi
```

Pi automatically loads `AGENTS.md`, which provides:
- **Default rule:** Prefer gorefactor for `.go` file mutations
- **Decision matrix:** When to use gorefactor vs Claude
- **Token efficiency guide:** Why gorefactor saves 80-95% tokens
- **Command reference:** Common edits → gorefactor commands

### 2. GoRefactor Skill

Available via `/skill:gorefactor` in pi interactive mode:
- Detailed reference tables for all operations
- Token efficiency examples
- Workflow recommendations
- Analysis command catalog
- Orchestration plan examples

### 3. GoRefactor Extension

Registers these tools so the LLM can call gorefactor directly:

**Analysis tools (read-only):**
- `gorefactor_parse` - Parse Go file structure
- `gorefactor_recommend` - Find extraction candidates
- `gorefactor_find_callers` - Find all callers of a function
- `gorefactor_find_uses` - Find all uses of a symbol
- `gorefactor_find_implementations` - Find implementations of an interface

**Mutation tools (structural):**
- `gorefactor_create` - Create new .go file
- `gorefactor_extract` - Extract code block to method
- `gorefactor_delete` - Delete declaration (with safety check)
- `gorefactor_move` - Move function/method between files

**Quality tools:**
- `gorefactor_lint` - Check code quality
- `gorefactor_doctor` - Full gate (lint + build + test)

### 4. Project Settings

`.pi/settings.json` enables:
- The GoRefactor extension by default
- The GoRefactor skill by default
- Project trust (so .pi/* resources load automatically)

## Usage Examples

### Example 1: Extract a Method

**Without gorefactor (expensive):**
```
Claude, extract the validation logic from parseOrder into a validateOrder helper.
Here's the code: [1000+ tokens of file content]
[Claude generates new code: 500+ tokens]
Total: ~1500+ tokens, plus risk of syntax errors
```

**With gorefactor (efficient):**
```
Extract the validation logic from parseOrder into validateOrder
```
→ Pi uses gorefactor analysis + extraction:
1. `gorefactor recommend` to find candidates (~50 tokens context)
2. `gorefactor extract` to perform the extraction (instant)
3. `gorefactor doctor` to verify (instant)

Total: ~200 tokens, guaranteed valid Go

### Example 2: Find Callers

**Without gorefactor:**
```
Claude, find all places that call PaymentValidator.
[Claude reads entire codebase, outputs results]
Total: 100-200+ tokens
```

**With gorefactor:**
```
Find all callers of PaymentValidator
```
→ Pi uses `gorefactor find-callers PaymentValidator`
Total: ~20 tokens

### Example 3: Batch Refactoring

**Task:** Move 5 related helper functions to a new file

```
Move validateEmail, validatePhone, formatPhone, hashPassword, and 
generateToken to a new utils.go file
```

→ Pi could:
1. Propose 5 `gorefactor_move` operations
2. Execute them one by one (or via orchestration plan)
3. Run `gorefactor_doctor` to verify

Each move: instant, guaranteed valid Go

## Decision Matrix: GoRefactor vs Claude

| Task | Tool | Why |
|------|------|-----|
| Extract a method | GoRefactor | Identify block (~50 tokens), tool infers signature & handles extraction (99% savings) |
| Move function to new file | GoRefactor | Target by name, semantic handling of imports (99% savings) |
| Rename unexported symbol | GoRefactor | Find-replace across package (99% savings) |
| Delete unused code | GoRefactor | Just needs location, checks callers first (99% savings) |
| Find all callers | GoRefactor | Semantic analysis, no code generation (95% savings) |
| Find implementations | GoRefactor | Graph traversal (95% savings) |
| Rewrite algorithm | Claude | Requires understanding of intent, tradeoffs |
| Fix bug | Claude | Needs semantic understanding of what's wrong |
| Add feature | Claude | Logic-level change |
| Error handling | Claude | Domain-specific knowledge |

## When to Use Each

### ✅ Use GoRefactor for:
- Structural changes (move, rename, extract, delete)
- Analysis (find callers, find uses, complexity analysis)
- Batch operations (multiple coordinated changes)
- Safety-critical edits (code that must not break)

### ✅ Use Claude for:
- Algorithm changes
- Bug fixes
- Feature additions
- Domain-specific logic

## Workflow: Maximizing Token Efficiency

1. **Start with analysis** (free):
   ```
   Analyze file.go for extraction candidates
   ```
   → Uses `gorefactor recommend`

2. **Review and decide** (~50 tokens):
   - Pi shows extraction candidates
   - You pick which ones to do

3. **Request refactoring** (~100 tokens):
   ```
   Extract the validateOrder logic into its own method
   ```
   → Uses `gorefactor extract`

4. **Verify** (instant):
   - Pi runs `gorefactor doctor`
   - Shows build/test results

5. **Total:** ~250 tokens for complex refactoring
   - Manual approach: 1000+ tokens

## Getting Started

### First Time with Pi in GoRefactor

```bash
cd /path/to/gorefactor
pi
```

You'll see:
- Startup message about GoRefactor tools loaded
- Available commands in header (if verbose)
- AGENTS.md context loaded

### Request Types

**Analysis:**
```
What extraction opportunities exist in parser/parse.go?
```
→ Uses `gorefactor recommend`

**Direct mutation:**
```
Extract the validation block from lines 45-60 in parser.go into validateNode
```
→ Uses `gorefactor extract`

**Complex refactoring:**
```
Analyze orchestrator.go and suggest a refactoring plan to reduce duplication
```
→ Uses `gorefactor recommend` + `gorefactor analyze-dir` + manual planning

### Viewing the Skill

```
/skill:gorefactor
```

Shows the full skill with:
- Reference tables for all operations
- Token efficiency examples
- Detailed workflows
- All commands with syntax

### Checking Available Tools

Pi shows registered tools in the header. GoRefactor tools start with `gorefactor_`.

## Configuration

### Location of Config Files

```
gorefactor/.pi/
├── settings.json              # Enables extension + skill
├── extensions/
│   └── gorefactor.ts          # Registers tools
└── skills/
    └── gorefactor/
        └── SKILL.md           # Reference guide
```

### Customizing

**To disable tools temporarily:**
```bash
pi --no-extensions
```

**To load custom extension in addition:**
```bash
pi -e ./my-extension.ts
```

**To change settings:**
Edit `.pi/settings.json` to enable/disable resources.

## Architecture

The integration follows pi's design philosophy:

1. **Progressive disclosure** - Full skill loads only on-demand (`/skill:gorefactor`)
2. **Context files** - AGENTS.md auto-loads without bloating prompt
3. **Tools first** - Extension registers gorefactor as proper tools (LLM can discover)
4. **Settings-driven** - `.pi/settings.json` controls what loads

## Further Reference

- **AGENTS.md** - Agent rules (go here for guidance)
- **CLAUDE.md** - Detailed architecture (reference for complex tasks)
- **README.md** (project root) - GoRefactor features
- **ORCHESTRATION_SYSTEM.md** - JSON plan specification
- `/skill:gorefactor` - In pi, type this to load the skill

## Verifying Setup

To verify pi is picking up the configuration:

```bash
cd /path/to/gorefactor
pi -v        # Show pi version and extensions/skills loaded
/skill:gorefactor  # In interactive mode, load skill
```

You should see:
- GoRefactor extension listed
- GoRefactor skill available
- AGENTS.md context loaded (in startup messages)

## Example Session

```
$ cd /path/to/gorefactor
$ pi

[Pi loads with AGENTS.md context, extension, and skill]

> Analyze parser.go for extraction opportunities

[Pi calls `gorefactor recommend parser.go`]
[Shows complexity analysis and candidates]

> Extract the parseExpression logic into validateExpression, lines 120-145

[Pi calls `gorefactor extract parser.go 120 145 validateExpression`]
[Returns result with new method]

> Run the quality gate

[Pi calls `gorefactor doctor`]
[Shows lint/build/test results]
```

## Troubleshooting

**Extension not loading?**
- Check `.pi/settings.json` has the extension path
- Run `/reload` in pi to reload extensions
- Check `.pi/extensions/gorefactor.ts` exists

**Skill not found?**
- Run `/skill:gorefactor` in pi to load it
- Check `.pi/settings.json` has skill path
- Skill must be in `.pi/skills/gorefactor/SKILL.md`

**Tools not showing?**
- Run pi in the gorefactor directory
- Check project trust: pi might be asking to trust `.pi/` resources
- Run `pi -a` to auto-trust for one session

**AGENTS.md not loaded?**
- Pi loads `AGENTS.md` from cwd and parent directories
- Make sure you're in the gorefactor directory when starting pi
- Check with `--no-context-files` to disable for debugging

## See Also

- [Pi Documentation](https://pi.dev/)
- [Agent Skills Specification](https://agentskills.io/)
- [GoRefactor README](README.md)
- [GoRefactor Architecture](CLAUDE.md)
