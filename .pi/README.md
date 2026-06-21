# Pi Integration for GoRefactor

This directory contains pi coding agent configuration for the GoRefactor project.

## Quick Start

When you start pi in this directory, it automatically loads:

1. **GoRefactor Skill** (`.pi/skills/gorefactor/SKILL.md`)
   - Commands: `/skill:gorefactor` to view the skill
   - Teaches best practices for using gorefactor vs direct edits
   - Reference tables for all common operations

2. **GoRefactor Extension** (`.pi/extensions/gorefactor.ts`)
   - Registers gorefactor commands as pi tools
   - Allows the LLM to call gorefactor directly
   - Tools: `gorefactor_parse`, `gorefactor_recommend`, `gorefactor_extract`, etc.

3. **Project Context** (`AGENTS.md` in project root)
   - Loaded automatically at startup
   - Provides agent-specific rules and guidelines
   - Instructs to prefer gorefactor for `.go` file mutations

## Usage

### Interactive Mode

Start pi in the gorefactor directory:

```bash
cd /path/to/gorefactor
pi
```

Pi will automatically:
- Load the GoRefactor skill
- Register GoRefactor tools
- Load AGENTS.md context

### In Your Requests

**Instead of asking the LLM to write Go code:**

❌ **Don't:** "Create a new file src/helper.go with this function..."
✅ **Do:** "Extract the validation logic from parseOrder into a new validateOrder helper"

The LLM will use gorefactor to:
1. Analyze the code (`gorefactor recommend`)
2. Identify the block to extract
3. Extract it (`gorefactor extract`)
4. Verify it works (`gorefactor doctor`)

**Token savings:** The above costs ~250 tokens total instead of 1000+

### Common Tasks

**Find where a function is called:**
```
Find all callers of PaymentValidator
```
→ Uses `gorefactor find-callers PaymentValidator`

**Understand a function:**
```
Analyze the complexity of ProcessOrder function
```
→ Uses `gorefactor recommend` to get extraction candidates

**Refactor safely:**
```
Refactor PaymentService to split into smaller methods
```
→ Uses analysis → plan generation → orchestration with semantic targeting

**Move related code:**
```
Move the validator helpers to validators.go
```
→ Uses `gorefactor move` for each function

## Configuration Files

| File | Purpose |
|------|---------|
| `settings.json` | Enable/configure extensions and skills |
| `extensions/gorefactor.ts` | Register gorefactor commands as pi tools |
| `skills/gorefactor/SKILL.md` | Detailed reference and workflows |

## Extension: What Tools Are Available

The extension registers these tools (the LLM can call them directly):

### Analysis (Read-Only)
- `gorefactor_parse` - Parse a Go file → JSON
- `gorefactor_recommend` - Find extraction candidates
- `gorefactor_find_callers` - Find all callers
- `gorefactor_find_uses` - Find all uses
- `gorefactor_find_implementations` - Find implementations

### Mutations (Structural)
- `gorefactor_create` - Create new .go file
- `gorefactor_extract` - Extract code block to method
- `gorefactor_move` - Move function/method to file
- `gorefactor_delete` - Delete declaration

### Quality
- `gorefactor_lint` - Check code quality
- `gorefactor_doctor` - Full gate (lint + build + test)

## When the LLM Uses GoRefactor

Pi is configured to **prefer gorefactor commands** over Write/Edit for `.go` files because:

1. **Safety** - GoRefactor validates syntax, infers types, handles imports
2. **Token efficiency** - 80-95% cheaper than LLM-generated code
3. **Semantic targeting** - Works even when code changes slightly
4. **Deterministic** - No hallucinations, guaranteed valid Go

## Further Reading

- **AGENTS.md** - Project rules and guidelines for all agents (including pi)
- **CLAUDE.md** - Detailed architecture and advanced workflows
- **README.md** (project root) - GoRefactor features and user guide
- `/skill:gorefactor` - In pi, type this command to load the skill

## Advanced: Loading Other Context

If you have additional project instructions or tools:

1. Place `.md` files in `.pi/skills/` (for on-demand skills)
2. Create more extensions in `.pi/extensions/` (will auto-load)
3. Add to `settings.json` to enable them

All paths are relative to this directory.

## Disabling GoRefactor Integration

To temporarily disable the GoRefactor tools/skill:

```bash
# Disable extensions only
pi --no-extensions

# Disable extensions and skills
pi --no-extensions --no-skills

# Disable GoRefactor extension specifically
pi --no-extensions -e other-extension.ts
```

To disable by default, edit `settings.json` and remove the extension/skill entries.
