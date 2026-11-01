# Go

The original version of SuperDB was developed in Go so Go clients
can easily access client libraries from the packages that comprise
the Go implementation.

## Installation

SuperDB is currently structured as a standard Go module so it's
easy to import into other Go projects straight from the GitHub repo.

Some of the key packages are:

* [super](https://pkg.go.dev/github.com/superdb/super) - Super values and types
* [sup](https://pkg.go.dev/github.com/superdb/super/sup) - SUP support
* [sio](https://pkg.go.dev/github.com/superdb/super/sio) - I/O interfaces for Super data following the Reader/Writer patterns
* [sio/bsupio](https://pkg.go.dev/github.com/superdb/super/sio/bsupio) - BSUP reader/writer
* [sio/supio](https://pkg.go.dev/github.com/superdb/super/sio/supio) - SUP reader/writer
* [dbconn](https://pkg.go.dev/github.com/superdb/super/dbconn) - interact with a SuperDB database
  either locally ([dbconn/local](https://pkg.go.dev/github.com/superdb/super/dbconn/local)) or
  remotely via a SuperDB service ([dbconn/remote](https://pkg.go.dev/github.com/superdb/super/dbconn/remote))

To install in your local Go project, simply run:
```
go get github.com/superdb/super@main
```

## Examples

### SUP Reader

Read SUP from stdin, dereference field `s`, and print results:
```mdtest-go-example
package main

import (
	"fmt"
	"log"
	"os"

	"github.com/superdb/super"
	"github.com/superdb/super/sio/supio"
	"github.com/superdb/super/sup"
)

func main() {
	sctx := super.NewContext()
	reader := supio.NewReader(sctx, os.Stdin)
	for {
		val, err := reader.Read()
		if err != nil {
			log.Fatalln(err)
		}
		if val == nil {
			return
		}
		s := val.Deref("s")
		if s.IsMissing() {
			s = super.Missing.Ptr()
		}
		fmt.Println(sup.String(s))
	}
}
```
To build, create a directory for the main package, initialize it,
copy the above code into `main.go`, and fetch the required Super packages.
```
mkdir example
cd example
go mod init example
cat > main.go
# [paste from above]
go get github.com/superdb/super@main
go mod tidy
```
To run type:
```
echo '{s:"hello"}{x:123}{s:"world"}' | go run .
```
which produces
```
"hello"
error("missing")
"world"
```

### Local Database Reader

This example interacts with a SuperDB database.  Note that it is straightforward
to support both direct access to a database via the file system (or S3 URL) as well
as access via a service endpoint.

First, we'll use `super` to create a database and load the example data:
```
super db init scratch
super db attach scratch
super db create Demo
echo '{s:"hello, world"}{x:1}{s:"good bye"}' | super db load -use Demo -
```
Now replace `main.go` with this code:
```mdtest-go-example
package main

import (
	"context"
	"fmt"
	"log"
	"os"

	"github.com/superdb/super"
	"github.com/superdb/super/compiler/srcfiles"
	"github.com/superdb/super/dbconn/remote"
	"github.com/superdb/super/pkg/storage"
	"github.com/superdb/super/sup"
	"github.com/superdb/super/sbuf"
)

func main() {
	if len(os.Args) != 2 {
		log.Fatalln("URI of database not provided")
	}
	uri, err := storage.ParseURI(os.Args[1])
	if err != nil {
		log.Fatalln(err)
	}
	ctx := context.TODO()
	db := remote.NewConn(uri.String())
	q, err := db.Query(ctx, srcfiles.Plain("from Demo"))
	if err != nil {
		log.Fatalln(err)
	}
	defer q.Close()
	defer q.Pull(true)
	reader := sbuf.PullerReader(sbuf.NewMaterializer(q))
	for {
		val, err := reader.Read()
		if err != nil {
			log.Fatalln(err)
		}
		if val == nil {
			return
		}
		s := val.Deref("s")
		if s.IsMissing() {
			s = super.Missing.Ptr()
		}
		fmt.Println(sup.String(s))
	}
}
```
After a re-run of `go mod tidy`, run this command to interact with the database via
the local file system:
```
go run . ./scratch
```
which should output
```
"hello, world"
"good bye"
error("missing")
```
Note that the order of data has changed because the database stores data
in a sorted order.  Since we did not specify a "sort key" when we created
the database, it ends up sorting the data by `this`.

### Database Service Reader

We can use the same code above to talk to a SuperDB servuce.  All we do is
give it the URI of the service, which by default is on port 9867.

To try this out, first run a SuperDB service on the scratch database we created
above:
```
super db serve -db ./scratch
```
Finally, in another local shell, run the Go program and specify the service
endpoint we just created:
```
go run . http://localhost:9867
```
and you should again get this result:
```
"hello, world"
"good bye"
error("missing")
```
