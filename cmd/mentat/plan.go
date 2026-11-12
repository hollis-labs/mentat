package main

import (
	"slices"
	"strings"

	"github.com/hollis-labs/mentat/internal/persistence"
)

// A command line starts only the modules it needs, and only the commands that must write may create anything. Even
// `mentat --help` used to boot the framework, which creates a signing key and a database in the data directory; run on
// a host with a live installation that fills the live directory. See plan.
type runPlan struct {
	modules []string
	// prepare creates the data directory (0700). False for everything that only reads.
	prepare bool
	// open is what the database may be asked to do; ignored when the plan has no database module.
	open persistence.Open
	// app is true when the command needs the store, embedder and job service built over the started framework.
	app bool
}

var (
	// helpModules start nothing that opens or creates a file.
	helpModules = []string{"config", "logging", "health", "cli"}
	// dataModules are what the data commands (repo, finding, schedule, ...) need: the database (which must already
	// exist), the job queue and scheduler tables over it, and secrets for an embedding key reference. No listener, no
	// identity (the signing key is not made here).
	dataModules = []string{"config", "logging", "secrets", "database", "queue", "scheduler", "cli"}
)

// plan decides, from the command line, which modules to start and what may be created. Only `serve`, `mcp` (which needs
// an existing database), `migrate-legacy`, `identity setup` and `identity key create` start the full set; the first,
// third, fourth and fifth may create the database, the signing key and the directory. Help at every level, version,
// config, health, completion and an unknown word start no module that opens a file. Every other first word is a data
// command: it needs the new-layout database to exist and creates nothing.
//
// Every argument that does not start with "-" counts as a command word. That is right while the root command has no
// global flag that takes a value; TestRootHasNoValueFlags fails if one is added, and plan must then learn it.
func plan(args []string, dataCommands []string) runPlan {
	help := runPlan{modules: pick(helpModules)}
	var words []string
	for _, a := range args {
		if a == "-h" || a == "--help" || a == "--version" || a == "help" && len(words) == 0 {
			return help
		}
		if !strings.HasPrefix(a, "-") {
			words = append(words, a)
		}
	}
	if len(words) == 0 {
		return help
	}
	full := func(open persistence.Open, prepare bool) runPlan {
		return runPlan{modules: substrateModules, prepare: prepare, open: open, app: true}
	}
	switch {
	case words[0] == "serve":
		return full(persistence.Create, true)
	case words[0] == "migrate-legacy":
		return full(persistence.CreateForMigration, true)
	case words[0] == "mcp":
		return full(persistence.ReadExisting, false)
	case words[0] == "identity" && len(words) > 1 && (words[1] == "setup" || (words[1] == "key" && len(words) > 2 && words[2] == "create")):
		return runPlan{modules: substrateModules, prepare: true, open: persistence.Create}
	case slices.Contains(dataCommands, words[0]):
		return runPlan{modules: pick(dataModules), open: persistence.ReadExisting, app: true}
	}
	return help
}

// pick keeps the start order of substrateModules.
func pick(want []string) []string {
	var out []string
	for _, m := range substrateModules {
		if slices.Contains(want, m) {
			out = append(out, m)
		}
	}
	return out
}
