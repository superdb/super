package main

import (
	"fmt"
	"os"

	_ "github.com/superdb/super/cmd/super/compile"
	_ "github.com/superdb/super/cmd/super/db/auth"
	_ "github.com/superdb/super/cmd/super/db/branch"
	_ "github.com/superdb/super/cmd/super/db/compact"
	_ "github.com/superdb/super/cmd/super/db/compile"
	_ "github.com/superdb/super/cmd/super/db/create"
	_ "github.com/superdb/super/cmd/super/db/delete"
	_ "github.com/superdb/super/cmd/super/db/drop"
	_ "github.com/superdb/super/cmd/super/db/init"
	_ "github.com/superdb/super/cmd/super/db/load"
	_ "github.com/superdb/super/cmd/super/db/log"
	_ "github.com/superdb/super/cmd/super/db/ls"
	_ "github.com/superdb/super/cmd/super/db/manage"
	_ "github.com/superdb/super/cmd/super/db/merge"
	_ "github.com/superdb/super/cmd/super/db/rename"
	_ "github.com/superdb/super/cmd/super/db/revert"
	_ "github.com/superdb/super/cmd/super/db/serve"
	_ "github.com/superdb/super/cmd/super/db/use"
	_ "github.com/superdb/super/cmd/super/db/vacate"
	_ "github.com/superdb/super/cmd/super/db/vacuum"
	_ "github.com/superdb/super/cmd/super/dev"
	_ "github.com/superdb/super/cmd/super/dev/bsup"
	_ "github.com/superdb/super/cmd/super/dev/vector/copy"
	_ "github.com/superdb/super/cmd/super/dev/vector/project"
	_ "github.com/superdb/super/cmd/super/dev/vector/search"
	"github.com/superdb/super/cmd/super/root"
)

func main() {
	if err := root.Super.Exec(os.Args[1:]); err != nil {
		fmt.Fprintf(os.Stderr, "%s\n", err)
		os.Exit(1)
	}
}
