# GoRefactor + Pi: Quick Start

## Tl;dr

Start pi in the gorefactor directory and it will prefer gorefactor commands over Write/Edit for `.go` files.

```bash
cd /path/to/gorefactor
pi
```

## One-Minute Overview

| Aspect | What Happens |
|--------|--------------|
| **AGENTS.md** | Loaded automatically—tells pi to use gorefactor for `.go` files |
| **Extension** | Registers gorefactor tools that pi can call |
| **Skill** | Available via `/skill:gorefactor` for reference |
| **Cost** | 80-95% token savings vs LLM-generated code |
| **Safety** | Gorefactor validates syntax, infers types, runs goimports |

## What to Ask Pi

✅ **Good (uses gorefactor):**
```
Extract the validation logic from parseOrder into validateOrder
```

✅ **Also good (analysis):**
```
Find all callers of PaymentService
```

❌ **Avoid (have Claude write it instead):**
```
Create a new payment processing service with these features...
```

## Common Tasks

| Task | Say | Tool Used |
|------|-----|-----------|
| Extract a method | "Extract lines 45-60 of parser.go into validateNode" | `gorefactor_extract` |
| Find who calls it | "Find all callers of PaymentValidator" | `gorefactor_find_callers` |
| Move to new file | "Move Helper1 and Helper2 to helpers.go" | `gorefactor_move` |
| Delete code | "Delete the unused ParseLegacy function" | `gorefactor_delete` |
| Find issues | "Run quality checks on the analyzer package" | `gorefactor_lint` |
| Refactor plan | "Analyze parser.go for optimization opportunities" | `gorefactor_recommend` |

## Pro Tips

1. **Use analysis first:**
   ```
   Analyze orchestrator.go for complexity
   ```
   → See what can be extracted before committing

2. **Then refactor:**
   ```
   Extract the complex condition into isValidTarget
   ```
   → Pi uses the analysis results to refactor

3. **Verify with gate:**
   ```
   Run the quality gate
   ```
   → Pi runs `gorefactor doctor` (lint + build + test)

4. **Load the skill:**
   ```
   /skill:gorefactor
   ```
   → Full reference with examples

## Tools Available

All start with `gorefactor_`:

**Analysis:** `parse`, `recommend`, `find_callers`, `find_uses`, `find_implementations`
**Mutation:** `create`, `extract`, `move`, `delete`
**Quality:** `lint`, `doctor`

## When to Use Claude Instead

- **Algorithms:** "Optimize this search to use binary search"
- **Bugs:** "Fix the race condition in the cache"
- **Features:** "Add async operation support"

## Settings

In `.pi/settings.json` (already configured):
- Extension: enabled
- Skill: enabled
- Project trust: auto-trust `.pi/` resources

To disable: `pi --no-extensions`

## Files

| File | Purpose |
|------|---------|
| `AGENTS.md` | Rules for all agents (pi loads this) |
| `.pi/extensions/gorefactor.ts` | Pi tools |
| `.pi/skills/gorefactor/SKILL.md` | Reference guide |
| `.pi/settings.json` | Configuration |
| `PI_INTEGRATION.md` | Full integration guide (detailed) |

## Verification

```bash
# Start pi
pi

# In pi, check:
# 1. Startup says "GoRefactor tools loaded"
# 2. Type: /skill:gorefactor (should load skill)
# 3. Ask: Find all callers of Parser
#    (should use gorefactor_find_callers)
```

## Next Steps

1. **Now:** Start pi in the gorefactor directory
2. **Try:** Ask "Analyze parser.go for extraction candidates"
3. **Then:** Ask "Extract lines 50-75 into a new validateNode function"
4. **Finally:** Ask "Run the quality gate"

## Questions?

- **Full integration details:** See `PI_INTEGRATION.md`
- **Architecture:** See `CLAUDE.md` in project root
- **Pi docs:** Run `/help` in pi or visit pi.dev
- **GoRefactor reference:** Type `/skill:gorefactor` in pi
