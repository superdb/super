package api

import (
	"context"

	"uuid"
	"github.com/superdb/super/compiler/srcfiles"
	"github.com/superdb/super/order"
	"github.com/superdb/super/pkg/field"
	"github.com/superdb/super/pkg/nano"
	"github.com/superdb/super/vector/vio"
)

const RequestIDHeader = "X-Request-ID"

func RequestIDFromContext(ctx context.Context) string {
	if v := ctx.Value(RequestIDHeader); v != nil {
		return v.(string)
	}
	return ""
}

type Error struct {
	Type              string             `json:"type"`
	Kind              string             `json:"kind"`
	Message           string             `json:"error"`
	CompilationErrors srcfiles.ErrorList `json:"compilation_errors,omitempty"`
}

func (e Error) Error() string {
	return e.Message
}

type VersionResponse struct {
	Version string `json:"version"`
}

type PoolPostRequest struct {
	Name     string   `json:"name"`
	SortKeys SortKeys `json:"layout"`
	Thresh   int64    `json:"thresh"`
}

type SortKeys struct {
	Order order.Which `json:"order" super:"order"`
	Keys  field.List  `json:"keys" super:"keys"`
}

type PoolPutRequest struct {
	Name string `json:"name"`
}

type BranchPostRequest struct {
	Name   string `json:"name"`
	Commit string `json:"commit"`
}

type BranchMergeRequest struct {
	At string `json:"at"`
}

type CompactRequest struct {
	ObjectIDs []uuid.UUID `super:"object_ids"`
}

type DeleteRequest struct {
	ObjectIDs []string `super:"object_ids"`
	Where     string   `super:"where"`
}

type CommitMessage struct {
	Author string `super:"author"`
	Body   string `super:"body"`
	Meta   string `super:"meta"`
}

type CommitResponse struct {
	Commit   uuid.UUID `super:"commit"`
	Warnings []string    `super:"warnings"`
}

type EventBranchCommit struct {
	CommitID uuid.UUID `super:"commit_id"`
	PoolID   uuid.UUID `super:"pool_id"`
	Branch   string      `super:"branch"`
	Parent   string      `super:"parent"`
}

type EventPool struct {
	PoolID uuid.UUID `super:"pool_id"`
}

type EventBranch struct {
	PoolID uuid.UUID `super:"pool_id"`
	Branch string      `super:"branch"`
}

type QueryRequest struct {
	Query string `json:"query"`
}

type QueryChannelSet struct {
	Channel string `json:"channel" super:"channel"`
}

type QueryChannelEnd struct {
	Channel string `json:"channel" super:"channel"`
}

type QueryError struct {
	Error string `json:"error" super:"error"`
}

type QueryStats struct {
	StartTime  nano.Ts `json:"start_time" super:"start_time"`
	UpdateTime nano.Ts `json:"update_time" super:"update_time"`
	vio.Progress
}

type QueryWarning struct {
	Warning string `json:"warning" super:"warning"`
}

type VacateResponse struct {
	CommitIDs []uuid.UUID `super:"commit_ids"`
}

type VacuumResponse struct {
	ObjectIDs []uuid.UUID `super:"object_ids"`
}

type VectorRequest struct {
	ObjectIDs []uuid.UUID `super:"object_ids"`
}
