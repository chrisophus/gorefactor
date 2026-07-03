package main

// agent_tasks.go: the evaluation corpus for gorefactor-agent (Slice 2).
//
// Each task is a self-contained fixture (a tiny Go module written to a temp
// dir + git-init'd) plus a spec handed to the agent and the outcome the corpus
// PREDICTS. The runner (agent_corpus.go) executes each against a real junior
// and tallies predicted-vs-actual, turning ad-hoc dogfooding into a regression
// suite: after a capability ships, its task must flip friction/fail →
// efficient, and that flip is the "zero recurrence" signal.
//
// Difficulty spans the capability frontier on purpose:
//   - easy   → efficient today (regression guard; catches FALSE friction)
//   - medium → efficient now that Slice 3 shipped change_signature/replace_body/
//              the structural primitives (were friction before)
//   - hard   → still friction/fail: probes agent tools NOT yet wired
//              (add_field, change_receiver, extract_interface) — the corpus
//              documents the remaining Slice-3 backlog.

type agentTask struct {
	ID         string
	Difficulty string          // easy | medium | hard
	Probes     string          // the capability this task exercises
	Expected   expectedOutcome // efficient | friction | fail
	Spec       string
	Fixture    map[string]string // filename -> content; must include go.mod
}

// gomod is the minimal module every fixture ships.
const gomod = "module fixture\n\ngo 1.21\n"

func agentTasks() []agentTask {
	return []agentTask{
		// ── easy: regression guards, should be EFFICIENT ────────────────────
		{
			ID: "easy-rename", Difficulty: "easy", Probes: "rename_declaration",
			Expected: outEfficient,
			Spec:     "In calc.go, rename the unexported function helper to compute (it is only used within this package).",
			Fixture: map[string]string{
				"go.mod": gomod,
				"calc.go": "package fixture\n\nfunc helper(a, b int) int { return a + b }\n\n" +
					"func Total(xs []int) int {\n\ts := 0\n\tfor _, x := range xs {\n\t\ts = helper(s, x)\n\t}\n\treturn s\n}\n",
			},
		},
		{
			ID: "easy-setdoc", Difficulty: "easy", Probes: "set_doc",
			Expected: outEfficient,
			Spec:     "Add a doc comment to the exported function Total in calc.go saying it returns the sum of xs.",
			Fixture: map[string]string{
				"go.mod":  gomod,
				"calc.go": "package fixture\n\nfunc Total(xs []int) int {\n\ts := 0\n\tfor _, x := range xs {\n\t\ts += x\n\t}\n\treturn s\n}\n",
			},
		},
		{
			ID: "easy-wraperr", Difficulty: "easy", Probes: "wrap_errors",
			Expected: outEfficient,
			Spec:     "In load.go, wrap the bare 'return err' inside the Load function with fmt.Errorf so the error is contextualized.",
			Fixture: map[string]string{
				"go.mod": gomod,
				"load.go": "package fixture\n\nimport \"os\"\n\nfunc Load(p string) ([]byte, error) {\n" +
					"\tb, err := os.ReadFile(p)\n\tif err != nil {\n\t\treturn nil, err\n\t}\n\treturn b, nil\n}\n",
			},
		},

		// ── medium: EFFICIENT now that Slice 3 shipped ──────────────────────
		{
			ID: "med-changesig", Difficulty: "medium", Probes: "change_signature",
			Expected: outEfficient,
			Spec:     "Add a new first parameter 'scale int' to the Scale function in calc.go, multiply its result by scale, and update all call sites to pass 1.",
			Fixture: map[string]string{
				"go.mod": gomod,
				"calc.go": "package fixture\n\nfunc Scale(x int) int { return x }\n\n" +
					"func Use() int {\n\ta := Scale(2)\n\tb := Scale(3)\n\treturn a + b\n}\n",
			},
		},
		{
			ID: "med-replacebody", Difficulty: "medium", Probes: "replace_body",
			Expected: outEfficient,
			Spec:     "Replace the body of the Max function in math.go so it correctly returns the larger of a and b.",
			Fixture: map[string]string{
				"go.mod":  gomod,
				"math.go": "package fixture\n\nfunc Max(a, b int) int {\n\treturn a\n}\n",
			},
		},
		{
			ID: "med-switchcase", Difficulty: "medium", Probes: "insert_switch_case",
			Expected: outEfficient,
			Spec:     "In route.go, add a case to the switch in Route so that when name is \"delete\" it returns 3. Keep the default.",
			Fixture: map[string]string{
				"go.mod": gomod,
				"route.go": "package fixture\n\nfunc Route(name string) int {\n\tswitch name {\n" +
					"\tcase \"get\":\n\t\treturn 1\n\tcase \"put\":\n\t\treturn 2\n\tdefault:\n\t\treturn 0\n\t}\n}\n",
			},
		},

		// ── hard: now EFFICIENT after the remaining Slice-3 tools were wired ─
		{
			ID: "hard-addfield", Difficulty: "hard", Probes: "add_field",
			Expected: outEfficient,
			Spec:     "Add a field 'Timeout int' to the Config struct in config.go and update the existing keyed literal in NewConfig to set it to 30.",
			Fixture: map[string]string{
				"go.mod": gomod,
				"config.go": "package fixture\n\ntype Config struct {\n\tName string\n}\n\n" +
					"func NewConfig() Config {\n\treturn Config{Name: \"x\"}\n}\n",
			},
		},
		{
			ID: "hard-recv", Difficulty: "hard", Probes: "change_receiver",
			Expected: outEfficient,
			Spec:     "Convert the value receiver on the Counter.Inc method in counter.go to a pointer receiver so it mutates the counter.",
			Fixture: map[string]string{
				"go.mod": gomod,
				"counter.go": "package fixture\n\ntype Counter struct{ n int }\n\n" +
					"func (c Counter) Inc() { c.n++ }\n",
			},
		},
		{
			ID: "hard-iface", Difficulty: "hard", Probes: "extract_interface",
			Expected: outEfficient,
			Spec:     "Extract an interface named Store from the exported methods of the *DB type in db.go.",
			Fixture: map[string]string{
				"go.mod": gomod,
				"db.go": "package fixture\n\ntype DB struct{}\n\n" +
					"func (d *DB) Get(k string) string { return \"\" }\n\nfunc (d *DB) Put(k, v string) {}\n",
			},
		},
	}
}
