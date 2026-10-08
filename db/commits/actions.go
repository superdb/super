package commits

import (
	"fmt"
	"uuid"

	"github.com/superdb/super"
	"github.com/superdb/super/db/data"
	"github.com/superdb/super/pkg/nano"
)

type Action interface {
	CommitID() uuid.UUID
	fmt.Stringer
}

var ActionTypes = []any{
	Add{},
	Delete{},
	Commit{},
}

type Add struct {
	Commit uuid.UUID   `super:"commit"`
	Object data.Object `super:"object"`
}

var _ Action = (*Add)(nil)

func (a *Add) CommitID() uuid.UUID {
	return a.Commit
}

func (a *Add) String() string {
	return fmt.Sprintf("ADD %s", a.Object)
}

// Note that we store the number of retries in the final commit
// object.  This will allow easily introspection of optimistic
// locking problems under high commit load by simply issuing
// a meta-query and looking at the retry count in the persisted
// commit objects.  If/when this is a problem, we could add
// pessimistic locking mechanisms alongside the optimistic approach.

type Commit struct {
	ID      uuid.UUID   `super:"id"`
	Parent  uuid.UUID   `super:"parent"`
	Retries uint8       `super:"retries"`
	Author  string      `super:"author"`
	Date    nano.Ts     `super:"date"`
	Message string      `super:"message"`
	Meta    super.Value `super:"meta"`
}

func (c *Commit) CommitID() uuid.UUID {
	return c.ID
}

func (c *Commit) String() string {
	//XXX need to format Message field for single line
	return fmt.Sprintf("COMMIT %s -> %s %s %s %s", c.ID, c.Parent, c.Date, c.Author, c.Message)
}

type Delete struct {
	Commit uuid.UUID `super:"commit"`
	ID     uuid.UUID `super:"id"`
}

func (d *Delete) CommitID() uuid.UUID {
	return d.Commit
}

func (d *Delete) String() string {
	return "DEL " + d.ID.String()
}
