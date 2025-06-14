# GoRefactor

GoRefactor is a command-line tool for refactoring Go code, with a focus on method extraction. It provides functionality to analyze Go code and suggest or perform refactoring operations.

## Features

- Parse Go files and output their structure
- List all functions and methods in a file
- Recommend code blocks for method extraction
- Extract methods from code blocks

## Installation

```bash
go install github.com/yourusername/gorefactor@latest
```

## Usage

### Parse a Go File

```bash
gorefactor parse path/to/file.go
```

This command parses a Go file and outputs its structure in JSON format, including:
- Package name
- Imports
- Functions
- Methods
- Structs
- Interfaces

### List Functions

```bash
gorefactor list-functions path/to/file.go
```

This command lists all functions and methods in the specified file.

### Recommend Extractions

```bash
gorefactor recommend path/to/file.go
```

This command analyzes the file and recommends code blocks that could be extracted into methods. The output includes:
- Start and end lines of the block
- Variables used in the block
- Complexity score
- Whether the block is extractable

### Extract Method

```bash
gorefactor extract path/to/file.go start_line end_line method_name
```

This command extracts a code block into a new method. It:
1. Analyzes the variables used in the block
2. Creates a new method with appropriate parameters
3. Replaces the original block with a call to the new method

## Example

Given a file `example.go`:

```go
package example

func processData(data []int) int {
    sum := 0
    for i := 0; i < len(data); i++ {
        if data[i] > 0 {
            sum += data[i]
        }
    }
    return sum
}
```

To extract the loop into a new method:

```bash
gorefactor extract example.go 5 9 calculateSum
```

This will create:

```go
package example

func processData(data []int) int {
    return calculateSum(data)
}

func calculateSum(data []int) int {
    sum := 0
    for i := 0; i < len(data); i++ {
        if data[i] > 0 {
            sum += data[i]
        }
    }
    return sum
}
```

## Development

### Project Structure

- `main.go`: Command-line interface and main entry point
- `parser/`: Package for parsing Go files
- `analyzer/`: Package for analyzing code blocks
- `extractor/`: Package for method extraction

### Running Tests

```bash
go test ./...
```

## License

MIT License 