package main

func init() {
	registerCommand(Command{
		Name:        "format",
		Description: "Format Go files (gofmt + goimports) in-place; pass dir/file paths or default '.'",
		Usage:       "format [path ...]",
		MinArgs:     0,
		MaxArgs:     -1,
		Run:         formatCommand,
	})
}
