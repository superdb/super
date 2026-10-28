package api

import (
	"context"
	"uuid"

	"github.com/superdb/super/compiler/srcfiles"
	"github.com/superdb/super/order"
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

type TablePostRequest struct {
	Name      string         `json:"name"`
	SortKeys  order.SortKeys `json:"sort_keys"`
	ObjectCap uint64         `json:"object_cap"`
	FrameCap  uint64         `json:"frame_cap"`
	Type      string         `json:"type,omitempty"`
	SQL       bool           `json:"sql,omitempty"`
}

type TablePutRequest struct {
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
	Warnings []string  `super:"warnings"`
}

type EventBranchCommit struct {
	CommitID uuid.UUID `super:"commit_id"`
	TableID  uuid.UUID `super:"table_id"`
	Branch   string    `super:"branch"`
	Parent   string    `super:"parent"`
}

type EventTable struct {
	TableID uuid.UUID `super:"table_id"`
}

type EventBranch struct {
	TableID uuid.UUID `super:"table_id"`
	Branch  string    `super:"branch"`
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
