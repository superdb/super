package client

import (
	"bytes"
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptrace"
	"time"

	"github.com/superdb/super"
	"github.com/superdb/super/api"
	"github.com/superdb/super/sio"
	"github.com/superdb/super/sio/bsupio"
)

type Request struct {
	Header http.Header
	Method string
	Path   string
	Body   any

	host     string
	ctx      context.Context
	recorder *recordReader

	// trace fields
	dnsStartTime  time.Time
	firstByteTime time.Time
	getConnTime   time.Time
	gotConnInfo   httptrace.GotConnInfo
}

func newRequest(ctx context.Context, host string, h http.Header) *Request {
	if requestID := api.RequestIDFromContext(ctx); requestID != "" {
		h.Set(api.RequestIDHeader, requestID)
	}
	req := &Request{
		Header: h,
		host:   host,
	}
	// use trace to track timing
	req.ctx = httptrace.WithClientTrace(ctx, &httptrace.ClientTrace{
		DNSStart:             func(httptrace.DNSStartInfo) { req.dnsStartTime = time.Now() },
		GotConn:              func(g httptrace.GotConnInfo) { req.gotConnInfo = g },
		GetConn:              func(string) { req.getConnTime = time.Now() },
		GotFirstResponseByte: func() { req.firstByteTime = time.Now() },
	})
	return req
}

func (r *Request) HTTPRequest() (*http.Request, error) {
	body, err := r.getBody()
	if err != nil {
		return nil, err
	}
	u := r.host + r.Path
	req, err := http.NewRequestWithContext(r.ctx, r.Method, u, body)
	if err != nil {
		return nil, err
	}
	// Set GetBody so posted data is preserved on redirects or requests with
	// expired access tokens.
	req.GetBody = r.getBody
	req.Header = r.Header
	return req, nil
}

func (r *Request) Context() context.Context {
	return r.ctx
}

func (r *Request) getBody() (io.ReadCloser, error) {
	body, err := r.reader()
	if err != nil {
		return nil, err
	}
	if r.recorder == nil {
		r.recorder = &recordReader{
			Reader: body,
			limit:  1024 * 1024 * 16,
		}
		return io.NopCloser(r.recorder), nil
	}
	if r.recorder.noreplay {
		return nil, errors.New("request cannot be replayed: read buffer exceeded size limit")
	}
	replay := &replayReader{
		Reader: body,
		buf:    bytes.NewReader(r.recorder.buf.Bytes()),
	}
	return io.NopCloser(replay), nil
}

func (r *Request) reader() (io.Reader, error) {
	if b, ok := r.Body.(io.Reader); ok {
		return b, nil
	}
	m := super.NewMarshaler(super.NewContext())
	m.Decorate(super.StylePackage)
	val, err := m.Marshal(r.Body)
	if err != nil {
		return nil, err
	}
	var buf bytes.Buffer
	zw := bsupio.NewNewRowWriter(sio.NopCloser(&buf))
	if err := zw.Write(val); err != nil {
		return nil, err
	}
	if err := zw.Close(); err != nil {
		return nil, err
	}
	return &buf, nil
}

type recordReader struct {
	io.Reader
	buf      bytes.Buffer
	limit    int
	noreplay bool
}

func (r *recordReader) Read(b []byte) (int, error) {
	n, err := r.Reader.Read(b)
	if remaining := r.limit - r.buf.Len(); remaining > 0 {
		cc := min(n, remaining)
		r.buf.Write(b[:cc])
	} else {
		// Set noreplay to true so we know that replay has exceeded the buffer
		// range and therefore the request cannot be replayed.
		r.noreplay = true
	}
	return n, err
}

type replayReader struct {
	io.Reader
	buf *bytes.Reader
}

func (r *replayReader) Read(b []byte) (int, error) {
	if r.buf.Len() > 0 {
		return r.buf.Read(b)
	}
	return r.Reader.Read(b)
}

func (r *Request) Duration() time.Duration {
	if r.gotConnInfo.Reused {
		return r.firstByteTime.Sub(r.getConnTime)
	}
	return r.firstByteTime.Sub(r.dnsStartTime)
}
