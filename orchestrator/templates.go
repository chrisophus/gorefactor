package orchestrator

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"
)

// TemplateGenerator helps create JSON refactoring plans
type TemplateGenerator struct{}

// NewTemplateGenerator creates a new template generator
func NewTemplateGenerator() *TemplateGenerator {
	return &TemplateGenerator{}
}

// GenerateBasicTemplate creates a basic refactoring plan template
func (tg *TemplateGenerator) GenerateBasicTemplate(name, description string) *RefactoringPlan {
	return &RefactoringPlan{
		Version:     "1.0",
		Name:        name,
		Description: description,
		Created:     time.Now(),
		Author:      "Developer",
		Operations:  []*RefactoringOperation{},
		Metadata: map[string]interface{}{
			"tags":     []string{"refactoring"},
			"priority": "medium",
		},
	}
}

// GenerateExtractionTemplate creates a template for method extraction
func (tg *TemplateGenerator) GenerateExtractionTemplate() *RefactoringOperation {
	return &RefactoringOperation{
		Type:        "extract_method",
		Description: "Extract a code block into a new method",
		File:        "path/to/your/file.go",
		Target: &TargetSpecification{
			FunctionName:  "YourFunctionName",
			CodePattern:   "if err != nil",
			VariableNames: []string{"variable1", "variable2"},
			FunctionCalls: []string{"function1", "function2"},
		},
		Parameters: map[string]interface{}{
			"methodName": "newMethodName",
		},
		Conditions: []*Condition{
			{
				Type:     "complexity",
				Property: "controlStructures",
				Value:    2,
				Operator: "gte",
			},
		},
		Fallback: &FallbackStrategy{
			Type:        "use_default",
			Description: "Use the first function if target not found",
		},
	}
}

// GenerateInlineTemplate creates a template for method inlining
func (tg *TemplateGenerator) GenerateInlineTemplate() *RefactoringOperation {
	return &RefactoringOperation{
		Type:        "inline_method",
		Description: "Inline a method call",
		File:        "path/to/your/file.go",
		Target: &TargetSpecification{
			FunctionName: "YourFunctionName",
			CodePattern:  "methodCall(",
		},
		Parameters: map[string]interface{}{
			"methodName": "methodToInline",
		},
	}
}

// GenerateRenameTemplate creates a template for variable renaming
func (tg *TemplateGenerator) GenerateRenameTemplate() *RefactoringOperation {
	return &RefactoringOperation{
		Type:        "rename_variable",
		Description: "Rename a variable",
		File:        "path/to/your/file.go",
		Target: &TargetSpecification{
			FunctionName:  "YourFunctionName",
			VariableNames: []string{"oldVariableName"},
		},
		Parameters: map[string]interface{}{
			"oldName": "oldVariableName",
			"newName": "newVariableName",
		},
	}
}

// GenerateMoveTemplate creates a template for method moving
func (tg *TemplateGenerator) GenerateMoveTemplate() *RefactoringOperation {
	return &RefactoringOperation{
		Type:        "move_method",
		Description: "Move a method to a different receiver type",
		File:        "path/to/your/file.go",
		Target: &TargetSpecification{
			MethodName:   "MethodToMove",
			ReceiverType: "CurrentReceiver",
		},
		Parameters: map[string]interface{}{
			"newReceiverType": "NewReceiver",
			"newFile":         "path/to/new/file.go",
		},
	}
}

// GenerateInsertCodeTemplate creates a template for code insertion
func (tg *TemplateGenerator) GenerateInsertCodeTemplate() *RefactoringOperation {
	return &RefactoringOperation{
		Type:        "insert_code",
		Description: "Insert new code snippet into existing file",
		File:        "path/to/your/file.go",
		Target: &TargetSpecification{
			FunctionName: "YourFunctionName",
		},
		Parameters: map[string]interface{}{
			"codeSnippet": `// New function to add
func newFunction() {
    fmt.Println("Hello, World!")
}`,
			"location": map[string]interface{}{
				"type":         "after_function",
				"functionName": "YourFunctionName",
			},
		},
		Conditions: []*Condition{
			{
				Type:     "existence",
				Property: "functionExists",
				Value:    true,
				Operator: "eq",
			},
		},
		Fallback: &FallbackStrategy{
			Type:        "at_end",
			Description: "Insert at end of file if target function not found",
		},
	}
}

// SaveTemplate saves a template to a JSON file
func (tg *TemplateGenerator) SaveTemplate(template interface{}, filePath string) error {
	data, err := json.MarshalIndent(template, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal template: %w", err)
	}

	if err := os.WriteFile(filePath, data, 0644); err != nil {
		return fmt.Errorf("failed to write template file: %w", err)
	}

	return nil
}

// GenerateAllTemplates generates all available templates
func (tg *TemplateGenerator) GenerateAllTemplates(outputDir string) error {
	// Create output directory if it doesn't exist
	if err := os.MkdirAll(outputDir, 0755); err != nil {
		return fmt.Errorf("failed to create output directory: %w", err)
	}

	// Generate basic plan template
	basicPlan := tg.GenerateBasicTemplate("my_refactoring_plan", "Description of your refactoring plan")
	if err := tg.SaveTemplate(basicPlan, filepath.Join(outputDir, "basic_plan_template.json")); err != nil {
		return err
	}

	// Generate operation templates
	operations := map[string]*RefactoringOperation{
		"extract_method":  tg.GenerateExtractionTemplate(),
		"inline_method":   tg.GenerateInlineTemplate(),
		"rename_variable": tg.GenerateRenameTemplate(),
		"move_method":     tg.GenerateMoveTemplate(),
		"insert_code":     tg.GenerateInsertCodeTemplate(),
	}

	for opType, operation := range operations {
		plan := tg.GenerateBasicTemplate(fmt.Sprintf("%s_template", opType), fmt.Sprintf("Template for %s operation", opType))
		plan.Operations = []*RefactoringOperation{operation}

		filename := fmt.Sprintf("%s_template.json", opType)
		if err := tg.SaveTemplate(plan, filepath.Join(outputDir, filename)); err != nil {
			return err
		}
	}

	// Generate comprehensive example
	comprehensivePlan := &RefactoringPlan{
		Version:     "1.0",
		Name:        "comprehensive_example",
		Description: "Example plan with multiple operations and different targeting strategies",
		Created:     time.Now(),
		Author:      "Developer",
		Operations: []*RefactoringOperation{
			tg.GenerateExtractionTemplate(),
			tg.GenerateRenameTemplate(),
		},
		Metadata: map[string]interface{}{
			"tags":          []string{"example", "comprehensive"},
			"priority":      "high",
			"estimatedTime": "1 hour",
		},
	}

	if err := tg.SaveTemplate(comprehensivePlan, filepath.Join(outputDir, "comprehensive_example.json")); err != nil {
		return err
	}

	return nil
}

// PrintTemplateHelp prints help information about templates
func (tg *TemplateGenerator) PrintTemplateHelp() {
	fmt.Println("Available Template Types:")
	fmt.Println("  1. basic_plan_template.json - Basic refactoring plan structure")
	fmt.Println("  2. extract_method_template.json - Method extraction operation")
	fmt.Println("  3. inline_method_template.json - Method inlining operation")
	fmt.Println("  4. rename_variable_template.json - Variable renaming operation")
	fmt.Println("  5. move_method_template.json - Method moving operation")
	fmt.Println("  6. insert_code_template.json - Code insertion operation")
	fmt.Println("  7. comprehensive_example.json - Complete example with multiple operations")
	fmt.Println()
	fmt.Println("Targeting Strategies:")
	fmt.Println("  - Line-based: Use startLine and endLine for precise targeting")
	fmt.Println("  - Function-based: Use functionName to target specific functions")
	fmt.Println("  - Method-based: Use methodName and receiverType for methods")
	fmt.Println("  - Pattern-based: Use codePattern to match code patterns")
	fmt.Println("  - Variable-based: Use variableNames to match variable usage")
	fmt.Println("  - Call-based: Use functionCalls to match function calls")
	fmt.Println()
	fmt.Println("Fallback Strategies:")
	fmt.Println("  - skip: Skip the operation if target not found")
	fmt.Println("  - use_default: Use the first available target")
	fmt.Println()
	fmt.Println("Conditions:")
	fmt.Println("  - complexity: Check code complexity metrics")
	fmt.Println("  - statementCount: Check number of statements")
	fmt.Println("  - controlStructures: Check number of control structures")
}
