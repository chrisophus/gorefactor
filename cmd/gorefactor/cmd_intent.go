package main

import (
	"fmt"

	"github.com/chrisophus/gorefactor/doctor"
)

func init() {
	registerCommand(Command{
		Name:        "intent",
		ReadOnly:    true,
		Description: "Declare a deliberate API change so the doctor gate passes it: intent api-change <scope> <reason>. Scope is a package dir or symbol prefix (e.g. analyzer or analyzer.ComputeAPIDiff). [--list] [--clear]",
		Usage:       "intent api-change <scope> <reason> | intent --list | intent --clear",
		MinArgs:     0,
		MaxArgs:     3,
		Flags:       map[string]bool{"--list": false, "--clear": false},
		Run:         intentCommand,
	})
}

func intentCommand(args []string) error {
	var positional []string
	list, clear := false, false
	for _, a := range args {
		switch a {
		case "--list":
			list = true
		case "--clear":
			clear = true
		default:
			positional = append(positional, a)
		}
	}
	switch {
	case list:
		return listIntents(".")
	case clear:
		if err := doctor.ClearIntents("."); err != nil {
			return fmt.Errorf("clear intents: %w", err)
		}
		fmt.Println("intents cleared")
		return nil
	}
	if len(positional) < 3 || positional[0] != doctor.IntentAPIChange {
		return fmt.Errorf("usage: intent api-change <scope> <reason> (or --list / --clear)")
	}
	in := doctor.Intent{Type: doctor.IntentAPIChange, Scope: positional[1], Reason: positional[2]}
	if err := doctor.AddIntent(".", in); err != nil {
		return fmt.Errorf("record intent: %w", err)
	}
	fmt.Printf("declared %s for scope %q: %s\n", in.Type, in.Scope, in.Reason)
	return nil
}

func listIntents(root string) error {
	intents, err := doctor.LoadIntents(root)
	if err != nil {
		return fmt.Errorf("load intents: %w", err)
	}
	if len(intents) == 0 {
		fmt.Println("no intents declared")
		return nil
	}
	for _, in := range intents {
		fmt.Printf("%s  %s  scope=%s  %s\n", in.Created.Format("2006-01-02 15:04"), in.Type, in.Scope, in.Reason)
	}
	return nil
}
