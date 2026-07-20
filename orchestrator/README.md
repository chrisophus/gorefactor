# GoRefactor Orchestration System

The orchestration system allows you to define refactoring operations in JSON files that are resilient to underlying code changes. This makes refactoring operations more robust and less likely to break when the codebase evolves.

See also the root [README.md](../README.md) (CLI overview) and [ORCHESTRATION_SYSTEM.md](../ORCHESTRATION_SYSTEM.md) (full schema and examples).

## Key Features

- **Resilient Targeting**: Use semantic information instead of line numbers to locate code targets
- **Fallback Strategies**: Define what to do when targets can't be found
- **Conditional Execution**: Only execute operations when certain conditions are met
- **Comprehensive Reporting**: Detailed execution results with statistics
- **Template System**: Generate JSON templates to get started quickly

## Quick Start

### 1. Generate Templates

```bash
# Generate all available templates
./gorefactor generate-templates ./templates

# This creates:
# - basic_plan_template.json
# - extract_method_template.json
# - inline_method_template.json
# - rename_variable_template.json
# - move_method_template.json
# - comprehensive_example.json
```

### 2. Create a Refactoring Plan

Edit one of the generated templates or create your own JSON file:

```json
{
  "version": "1.0",
  "name": "my_refactoring_plan",
  "description": "Extract validation logic into separate methods",
  "created": "2024-01-15T10:00:00Z",
  "author": "Developer",
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
        "methodName": "validateUserInput"
      }
    }
  ]
}
```

### 3. Execute the Plan

```bash
# Execute the plan and output results to stdout
./gorefactor orchestrate my_plan.json

# Execute the plan and save results to a file
./gorefactor orchestrate my_plan.json results.json
```

## Targeting Strategies

### 1. Line-based Targeting (Traditional)

```json
{
  "target": {
    "startLine": 45,
    "endLine": 67
  }
}
```

**Pros**: Precise targeting
**Cons**: Breaks when code changes

### 2. Function-based Targeting

```json
{
  "target": {
    "functionName": "CreateUser"
  }
}
```

**Pros**: Resilient to code changes within the function
**Cons**: Less precise

### 3. Method-based Targeting

```json
{
  "target": {
    "methodName": "Validate",
    "receiverType": "User"
  }
}
```

**Pros**: Targets specific methods on specific types
**Cons**: Requires exact method and receiver names

### 4. Pattern-based Targeting

```json
{
  "target": {
    "functionName": "CreateUser",
    "codePattern": "if err != nil"
  }
}
```

**Pros**: Finds code by content patterns
**Cons**: Pattern must be unique enough

### 5. Variable-based Targeting

```json
{
  "target": {
    "functionName": "ProcessData",
    "variableNames": ["user", "config", "result"]
  }
}
```

**Pros**: Finds code by variable usage
**Cons**: Variables must be distinctive

### 6. Call-based Targeting

```json
{
  "target": {
    "functionName": "LoadConfig",
    "functionCalls": ["viper.ReadInConfig", "viper.Unmarshal"]
  }
}
```

**Pros**: Finds code by function calls
**Cons**: Function calls must be distinctive

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

### Available Condition Types

- `complexity`: Check code complexity metrics
- `statementCount`: Check number of statements
- `controlStructures`: Check number of control structures
- `errorHandlingPaths`: Check number of error handling paths
- `returnCount`: Check number of return statements

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

```json
{
  "type": "extract_method",
  "description": "Extract a code block into a new method",
  "file": "service.go",
  "target": {
    "functionName": "ProcessData"
  },
  "parameters": {
    "methodName": "validateInput"
  }
}
```

### 2. Inline Method

```json
{
  "type": "inline_method",
  "description": "Inline a method call",
  "file": "service.go",
  "target": {
    "functionName": "ProcessData",
    "codePattern": "validateInput("
  },
  "parameters": {
    "methodName": "validateInput"
  }
}
```

### 3. Rename Variable

```json
{
  "type": "rename_variable",
  "description": "Rename a variable",
  "file": "service.go",
  "target": {
    "functionName": "ProcessData",
    "variableNames": ["oldName"]
  },
  "parameters": {
    "oldName": "oldVariableName",
    "newName": "newVariableName"
  }
}
```

### 4. Move Method

```json
{
  "type": "move_method",
  "description": "Move a method to a different receiver type",
  "file": "user.go",
  "target": {
    "methodName": "Validate",
    "receiverType": "User"
  },
  "parameters": {
    "newReceiverType": "UserValidator",
    "newFile": "validator.go"
  }
}
```

## Execution Results

The orchestrator provides detailed execution results:

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
          "description": "Extracted method 'validateInput'",
          "newCode": "Method 'validateInput' extracted with parameters: [user, config]"
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

## Advanced Examples

### Complex Multi-Operation Plan

```json
{
  "version": "1.0",
  "name": "api_refactoring",
  "description": "Comprehensive API refactoring",
  "operations": [
    {
      "type": "extract_method",
      "description": "Extract error handling",
      "file": "api/handlers.go",
      "target": {
        "functionName": "handleRequest",
        "codePattern": "if err != nil {"
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
      ]
    },
    {
      "type": "extract_method",
      "description": "Extract validation logic",
      "file": "models/user.go",
      "target": {
        "methodName": "Validate",
        "receiverType": "User",
        "variableNames": ["user", "errors"]
      },
      "parameters": {
        "methodName": "validateUserData"
      },
      "fallback": {
        "type": "skip"
      }
    }
  ]
}
```

### Conditional Refactoring

```json
{
  "version": "1.0",
  "name": "conditional_refactoring",
  "description": "Only refactor complex functions",
  "operations": [
    {
      "type": "extract_method",
      "description": "Extract complex logic",
      "file": "service.go",
      "target": {
        "functionName": "ProcessComplexData"
      },
      "parameters": {
        "methodName": "processDataStep1"
      },
      "conditions": [
        {
          "type": "complexity",
          "property": "statementCount",
          "value": 10,
          "operator": "gte"
        },
        {
          "type": "complexity",
          "property": "controlStructures",
          "value": 3,
          "operator": "gte"
        }
      ]
    }
  ]
}
```

## Troubleshooting

### Common Issues

1. **Target not found**: Use more specific targeting or add fallback strategies
2. **Operation fails**: Check that the target file exists and is valid Go code
3. **Unexpected results**: Review the execution results and adjust targeting

### Debug Mode

Add debug information to your plans:

```json
{
  "metadata": {
    "debug": true,
    "verbose": true
  }
}
```

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

This system provides a robust, resilient way to orchestrate refactoring operations that can survive code changes and provide detailed feedback about what was executed. 