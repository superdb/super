package remote

import (
	"fmt"
	"io"

	"github.com/superdb/super"
	"github.com/superdb/super/service/api"
	"github.com/superdb/super/sup"
)

type EventsClient struct {
	rc          io.ReadCloser
	unmarshaler *super.Unmarshaler
}

func newEventsClient(resp *Response) *EventsClient {
	unmarshaler := super.NewUnmarshaler()
	unmarshaler.Bind(
		api.EventTable{},
		api.EventBranch{},
		api.EventBranchCommit{},
	)
	return &EventsClient{
		rc:          resp.Body,
		unmarshaler: unmarshaler,
	}
}

func (l *EventsClient) Recv() (string, any, error) {
	var kind, data string
	_, err := fmt.Fscanf(l.rc, "event: %s\ndata: %s\n\n\n", &kind, &data)
	if err != nil {
		return "", nil, err
	}
	val, err := sup.ParseValue(super.NewContext(), data)
	if err != nil {
		return "", nil, err
	}
	var v any
	if err := l.unmarshaler.Unmarshal(val, &v); err != nil {
		return "", nil, err
	}
	return kind, v, err
}

func (l *EventsClient) Close() error {
	return l.rc.Close()
}
