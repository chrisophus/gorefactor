package orchestrator

import "time"

// RefactoringOperation represents a single refactoring operation
type RefactoringOperation struct {
	Type        string `json:"type"`
	Description string `json:"description"`

	// TargetSpecification defines how to locate the target for refactoring
	File   string `json:"file"`
	Target *

	// Line-based targeting (traditional)
	TargetSpecification `json:"target"`
	Parameters map[string]interface{} `json:"parameters,omitempty"`
	Conditions []*Condition           `json:"conditions,omitempty"`

	// Semantic targeting (resilient to code changes)
	Fallback *FallbackStrategy `json:"fallback,omitempty"`
}

type TargetSpecification struct {
	StartLine *int `json:"startLine,omitempty"`
	EndLine   *int `json:"endLine,omitempty"`

	FunctionName      string   `json:"functionName,omitempty"`
	MethodName        string   `json:"methodName,omitempty"`
	ReceiverType      string   `json:"receiverType,omitempty"`
	CodePattern       string   `json:"codePattern,omitempty"`
	VariableNames     []string `json:"variableNames,omitempty"`
	FunctionCalls     []string `json:"functionCalls,omitempty"`
	ControlStructures []string `json:"controlStructures,omitempty"`
	Comments          []string `json:"comments,omitempty"`

	// Declaration-level targeting
	TypeName  string `json:"typeName,omitempty"`  // For type declarations
	ConstName string `json:"constName,omitempty"` // For const declarations
	VarName   string `json:"varName,omitempty"`   // For var declarations

	// Context-based targeting

	// Condition represents a condition that must be met for the operation

	// eq, ne, gt, lt, contains, regex

	// FallbackStrategy defines what to do if the primary target cannot be found

	// RefactoringPlan represents a complete refactoring plan

	// ExecutionResult represents the result of executing a refactoring plan

	// OperationResult represents the result of a single operation

	// CodeChange represents a specific change made to the code

	// ExecutionStatistics provides metrics about the execution

	// Orchestrator manages the execution of refactoring plans

	BeforePattern   string            `json:"beforePattern,omitempty"`
	AfterPattern    string            `json:"afterPattern,omitempty"`
	SurroundingCode map[string]string `json:"surroundingCode,omitempty"`
}

type Condition struct {
	Type     string      `json:"type"`
	Property string      `json:"property"`
	Value    interface{} `json:"value"`
	Operator string      `json:"operator,omitempty"`
}

type FallbackStrategy struct {
	Type        string                 `json:"type"`
	Description string                 `json:"description"`
	Parameters  map[string]interface{} `json:"parameters,omitempty"`
}

type RefactoringPlan struct {
	Version     string                  `json:"version"`
	Name        string                  `json:"name"`
	Description string                  `json:"description"`
	Created     time.Time               `json:"created"`
	Author      string                  `json:"author,omitempty"`
	Operations  []*RefactoringOperation `json:"operations"`
	Metadata    map[string]interface{}  `json:"metadata,omitempty"`
}

type ExecutionResult struct {
	PlanName   string               `json:"planName"`
	Executed   time.Time            `json:"executed"`
	Success    bool                 `json:"success"`
	Operations []*OperationResult   `json:"operations"`
	Errors     []string             `json:"errors,omitempty"`
	Warnings   []string             `json:"warnings,omitempty"`
	Statistics *ExecutionStatistics `json:"statistics,omitempty"`
}

type OperationResult struct {
	Operation    *RefactoringOperation `json:"operation"`
	Success      bool                  `json:"success"`
	Message      string                `json:"message"`
	Applied      bool                  `json:"applied"`
	FallbackUsed bool                  `json:"fallbackUsed,omitempty"`
	Changes      []*CodeChange         `json:"changes,omitempty"`
	Error        string                `json:"error,omitempty"`
}

type CodeChange struct {
	Type        string `json:"type"`
	File        string `json:"file"`
	StartLine   int    `json:"startLine"`
	EndLine     int    `json:"endLine"`
	Description string `json:"description"`
	OldCode     string `json:"oldCode,omitempty"`
	NewCode     string `json:"newCode,omitempty"`
}

type ExecutionStatistics struct {
	TotalOperations      int `json:"totalOperations"`
	SuccessfulOperations int `json:"successfulOperations"`
	FailedOperations     int `json:"failedOperations"`
	SkippedOperations    int `json:"skippedOperations"`
	FallbackUsed         int `json:"fallbackUsed"`
	TotalChanges         int `json:"totalChanges"`
}

type Orchestrator struct {
	plans map[string]*RefactoringPlan
}
