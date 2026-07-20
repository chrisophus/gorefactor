# GoRefactor JSON Orchestration System

## Overview

The GoRefactor orchestration system provides a robust, resilient way to orchestrate refactoring operations through JSON configuration files. This system is designed to work even when the underlying code has changed, making it much more reliable than traditional line-based refactoring approaches.

## Key Features

### 🎯 Resilient Targeting
- **Semantic targeting**: Use function names, method names, code patterns, and variable usage instead of line numbers
- **Multiple targeting strategies**: Combine different targeting approaches for better accuracy
- **Context-aware**: Use surrounding code context to locate targets

### 🔄 Fallback Strategies
- **Skip operations**: Gracefully skip operations when targets can't be found
- **Use defaults**: Fall back to reasonable defaults when primary targets fail
- **Configurable behavior**: Define custom fallback logic

### ✅ Conditional Execution
- **Complexity checks**: Only execute operations on code that meets complexity criteria
- **Statement counts**: Target code blocks with specific statement counts
- **Control structure analysis**: Focus on code with specific control flow patterns

### 📊 Comprehensive Reporting
- **Detailed results**: Track every operation and its outcome
- **Statistics**: Monitor success rates, fallback usage, and change counts
- **Error tracking**: Capture and report detailed error information

### 🛠️ Template System
- **Pre-built templates**: Generate common refactoring patterns
- **Customizable**: Easy to modify and extend templates
- **Best practices**: Templates follow recommended patterns

## Architecture

### Core Components

1. **Orchestrator** (`orchestrator/orchestrator.go`)
   - Manages plan loading and execution
   - Handles target location and fallback strategies
   - Provides comprehensive result reporting

2. **Template Generator** (`orchestrator/templates.go`)
   - Creates JSON templates for common operations
   - Provides guidance on best practices
   - Supports custom template generation

3. **CLI integration** (`cmd/gorefactor/`)
   - `orchestrate` command for executing plans
   - `generate-templates` command for creating templates
   - `undo` rolls back snapshots under `.gorefactor/`
   - See also [orchestrator/README.md](orchestrator/README.md) and the root [README.md](README.md)

### Data Structures

```go
// RefactoringPlan - Complete refactoring plan
type RefactoringPlan struct {
    Version     string
    Name        string
    Description string
    Created     time.Time
    Author      string
    Operations  []*RefactoringOperation
    Metadata    map[string]interface{}
}

// RefactoringOperation - Single refactoring operation
type RefactoringOperation struct {
    Type        string
    Description string
    File        string
    Target      *TargetSpecification
    Parameters  map[string]interface{}
    Conditions  []*Condition
    Fallback    *FallbackStrategy
}

// TargetSpecification - How to locate the target
type TargetSpecification struct {
    // Line-based (traditional)
    StartLine *int
    EndLine   *int
    
    // Semantic targeting (resilient)
    FunctionName      string
    MethodName        string
    ReceiverType      string
    CodePattern       string
    VariableNames     []string
    FunctionCalls     []string
}
```

## Usage Examples

### 1. Basic Method Extraction

```json
{
  "version": "1.0",
  "name": "extract_validation",
  "description": "Extract validation logic into separate methods",
  "operations": [
    {
      "type": "extract_method",
      "description": "Extract user validation logic",
      "file": "user_service.go",
      "target": {
        "functionName": "CreateUser",
        "codePattern": "if err != nil",
        "variableNames": ["user", "err"]
      },
      "parameters": {
        "methodName": "validateUser"
      }
    }
  ]
}
```

### 2. Multi-Operation Plan with Conditions

```json
{
  "version": "1.0",
  "name": "comprehensive_refactoring",
  "description": "Refactor complex functions",
  "operations": [
    {
      "type": "extract_method",
      "description": "Extract error handling",
      "file": "api/handlers.go",
      "target": {
        "functionName": "handleRequest",
        "codePattern": "if err != nil {",
        "controlStructures": ["if", "return"]
      },
      "parameters": {
        "methodName": "handleError"
      },
      "conditions": [
        {
          "type": "complexity",
          "property": "errorHandlingPaths",
          "value": 1,
          "operator": "gte"
        }
      ],
      "fallback": {
        "type": "skip",
        "description": "Skip if no error handling found"
      }
    }
  ]
}
```

### 3. Resilient Targeting Example

```json
{
  "target": {
    "functionName": "ProcessData",
    "codePattern": "for i, item := range",
    "variableNames": ["result", "processed"],
    "functionCalls": ["append", "len"]
  }
}
```

This target will find the `ProcessData` function and look for a loop that processes items, even if the exact line numbers change.

## CLI Commands

### Generate Templates

```bash
# Generate all available templates
./gorefactor generate-templates ./templates

# This creates:
# - basic_plan_template.json
# - extract_method_template.json
# - inline_method_template.json
# - rename_declaration_template.json
# - move_method_template.json
# - comprehensive_example.json
```

### Execute Plans

```bash
# Execute plan and output results to stdout
./gorefactor orchestrate my_plan.json

# Execute plan and save results to file
./gorefactor orchestrate my_plan.json results.json
```

## Targeting Strategies

### 1. Line-based Targeting
```json
{
  "target": {
    "startLine": 45,
    "endLine": 67
  }
}
```
**Use when**: You need precise targeting and code is stable
**Avoid when**: Code changes frequently

### 2. Function-based Targeting
```json
{
  "target": {
    "functionName": "CreateUser"
  }
}
```
**Use when**: You want to target entire functions
**Pros**: Resilient to internal code changes

### 3. Pattern-based Targeting
```json
{
  "target": {
    "functionName": "CreateUser",
    "codePattern": "if err != nil"
  }
}
```
**Use when**: You want to find specific code patterns
**Pros**: Finds code by content, not position

### 4. Variable-based Targeting
```json
{
  "target": {
    "functionName": "ProcessData",
    "variableNames": ["user", "config", "result"]
  }
}
```
**Use when**: You want to find code that uses specific variables
**Pros**: Resilient to code reorganization

### 5. Call-based Targeting
```json
{
  "target": {
    "functionName": "LoadConfig",
    "functionCalls": ["viper.ReadInConfig", "viper.Unmarshal"]
  }
}
```
**Use when**: You want to find code that calls specific functions
**Pros**: Good for finding specific functionality

## Fallback Strategies

### Skip Operation
```json
{
  "fallback": {
    "type": "skip",
    "description": "Skip if target not found"
  }
}
```

### Use Default Target
```json
{
  "fallback": {
    "type": "use_default",
    "description": "Use the first function if target not found"
  }
}
```

## Conditions

Conditions allow you to only execute operations when certain criteria are met:

```json
{
  "conditions": [
    {
      "type": "complexity",
      "property": "controlStructures",
      "value": 2,
      "operator": "gte"
    },
    {
      "type": "complexity",
      "property": "statementCount",
      "value": 5,
      "operator": "gte"
    }
  ]
}
```

### Available Properties
- `controlStructures`: Number of if, for, switch statements
- `statementCount`: Total number of statements
- `errorHandlingPaths`: Number of error handling paths
- `returnCount`: Number of return statements
- `logicalOperators`: Number of &&, || operators

### Available Operators
- `eq`: Equal to
- `ne`: Not equal to
- `gt`: Greater than
- `gte`: Greater than or equal to
- `lt`: Less than
- `lte`: Less than or equal to
- `contains`: Contains substring
- `regex`: Matches regex pattern

## Operation Types

### 1. Extract Method
Extracts a code block into a new method.

```json
{
  "type": "extract_method",
  "parameters": {
    "methodName": "validateUser"
  }
}
```

### 2. Inline Method
Inlines a method call.

```json
{
  "type": "inline_method",
  "parameters": {
    "methodName": "validateUser"
  }
}
```

### 3. Rename Declaration
Renames a top-level declaration across the package.

```json
{
  "type": "rename_declaration",
  "target": {
    "functionName": "oldFunctionName"
  },
  "parameters": {
    "newName": "newFunctionName"
  }
}
```

### 4. Move Method
Moves a method to a different receiver type.

```json
{
  "type": "move_method",
  "parameters": {
    "newReceiverType": "UserValidator",
    "newFile": "validator.go"
  }
}
```

### 5. Insert Code
Inserts new code snippets into existing files.

```json
{
  "type": "insert_code",
  "parameters": {
    "codeSnippet": "func newFunction() {\n    fmt.Println(\"Hello, World!\")\n}",
    "location": {
      "type": "after_function",
      "functionName": "existingFunction"
    }
  }
}
```

#### Insertion Location Types

- `before_function`: Insert before a specific function
- `after_function`: Insert after a specific function  
- `inside_function`: Insert at the beginning of a function body
- `at_end`: Insert at the end of the file
- `at_beginning`: Insert at the beginning of the file (after package declaration)

## Execution Results

The system provides detailed execution results:

```json
{
  "planName": "my_refactoring_plan",
  "executed": "2024-01-15T10:30:00Z",
  "success": true,
  "operations": [
    {
      "operation": { /* operation details */ },
      "success": true,
      "message": "Operation completed successfully",
      "applied": true,
      "fallbackUsed": false,
      "changes": [
        {
          "type": "extract_method",
          "file": "service.go",
          "startLine": 45,
          "endLine": 67,
          "description": "Extracted method 'validateUser'",
          "newCode": "Method 'validateUser' extracted with parameters: [user, config]"
        }
      ]
    }
  ],
  "statistics": {
    "totalOperations": 1,
    "successfulOperations": 1,
    "failedOperations": 0,
    "skippedOperations": 0,
    "fallbackUsed": 0,
    "totalChanges": 1
  }
}
```

## Best Practices

### 1. Use Semantic Targeting
Prefer semantic targeting over line-based targeting:

```json
// Good - resilient to code changes
{
  "target": {
    "functionName": "CreateUser",
    "codePattern": "if err != nil"
  }
}

// Avoid - breaks when code changes
{
  "target": {
    "startLine": 45,
    "endLine": 67
  }
}
```

### 2. Combine Multiple Targeting Strategies
Use multiple targeting strategies for better accuracy:

```json
{
  "target": {
    "functionName": "ProcessData",
    "codePattern": "if err != nil",
    "variableNames": ["data", "err"],
    "functionCalls": ["validate", "save"]
  }
}
```

### 3. Always Include Fallback Strategies
```json
{
  "fallback": {
    "type": "skip",
    "description": "Skip if target not found"
  }
}
```

### 4. Use Conditions for Safety
```json
{
  "conditions": [
    {
      "type": "complexity",
      "property": "controlStructures",
      "value": 2,
      "operator": "gte"
    }
  ]
}
```

### 5. Test Plans on Sample Code
Always test your plans on sample code before running them on production code.

## Integration with CI/CD

You can integrate refactoring plans into your CI/CD pipeline:

```yaml
# GitHub Actions example
- name: Run Refactoring Plan
  run: |
    ./gorefactor orchestrate refactoring_plan.json results.json
    if [ $? -ne 0 ]; then
      echo "Refactoring failed"
      exit 1
    fi
```

## Benefits Over Traditional Approaches

### 1. Resilience to Code Changes
- Traditional line-based refactoring breaks when code changes
- Semantic targeting adapts to code evolution
- Fallback strategies handle missing targets gracefully

### 2. Comprehensive Planning
- Define complex refactoring sequences in JSON
- Version control your refactoring plans
- Share and reuse refactoring patterns

### 3. Safety and Control
- Conditional execution prevents unwanted changes
- Detailed reporting shows exactly what was changed
- Fallback strategies provide predictable behavior

### 4. Scalability
- Execute multiple operations in sequence
- Apply the same plan to multiple files
- Integrate with automated workflows

## Future Enhancements

### Planned Features
1. **More operation types**: Extract class, move field, etc.
2. **Advanced targeting**: AST-based pattern matching
3. **Dependency analysis**: Understand code dependencies
4. **Conflict resolution**: Handle overlapping changes
5. **Visualization**: Show planned changes before execution

### Extension Points
1. **Custom operations**: Plugin system for new refactoring types
2. **Custom targeting**: User-defined targeting strategies
3. **Custom conditions**: User-defined condition types
4. **Custom fallbacks**: User-defined fallback strategies

## Conclusion

The GoRefactor orchestration system provides a powerful, resilient approach to automated refactoring. By using semantic targeting, fallback strategies, and conditional execution, it can handle code changes gracefully while providing comprehensive control and reporting.

This system is particularly valuable for:
- **Large codebases** that need systematic refactoring
- **Teams** that want to share and version control refactoring plans
- **CI/CD pipelines** that need reliable automated refactoring
- **Code evolution** where traditional line-based approaches fail

The JSON-based approach makes refactoring plans human-readable, version-controllable, and easily shareable across teams and projects. 