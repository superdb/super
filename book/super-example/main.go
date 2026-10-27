//go:build js && wasm

package main

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"runtime/debug"
	"slices"
	"strings"
	"syscall/js"

	"github.com/superdb/super"
	"github.com/superdb/super/compiler"
	"github.com/superdb/super/compiler/parser"
	"github.com/superdb/super/pkg/storage"
	"github.com/superdb/super/runtime"
	"github.com/superdb/super/sio"
	"github.com/superdb/super/sio/anyio"
	"github.com/superdb/super/vector/vio"
	"github.com/teamortix/golang-wasm/wasm"
)

func main() {
	wasm.Expose("run", run)
	wasm.Expose("parse", parse)
	wasm.Ready()
	<-make(chan struct{})
}

type opts struct {
	Program      string   `wasm:"program"`
	Input        js.Value `wasm:"input"`
	InputFormat  string   `wasm:"inputFormat"`
	OutputFormat string   `wasm:"outputFormat"`
}

// chunk represents a chunk in a ReadableStream
type chunk struct {
	Done  bool     `wasm:"done"`
	Value js.Value `wasm:"value"`
}

var errInvalidInput = errors.New("only string or ReadableStream accept as input")

func run(opts opts) wasm.Promise {
	return wasm.NewPromise(func() (v interface{}, err error) {
		defer func() {
			if r := recover(); r != nil {
				err = fmt.Errorf("panic: %+v\n\n%s\n", r, debug.Stack())
			}
		}()
		flowgraph, err := parser.ParseText(opts.Program)
		if err != nil {
			return "", err
		}
		var r io.Reader
		switch typ := opts.Input.Type(); typ {
		case js.TypeString:
			if s := opts.Input.String(); strings.TrimSpace(s) != "" {
				r = strings.NewReader(s)
			}
		case js.TypeObject:
			if !opts.Input.InstanceOf(js.Global().Get("ReadableStream")) {
				return nil, errInvalidInput
			}
			r = readableStream(opts.Input)
		default:
			return "", errInvalidInput
		}
		var buf bytes.Buffer
		zwc, err := anyio.NewWriter(sio.NopCloser(&buf), anyio.WriterOpts{Format: opts.OutputFormat})
		if err != nil {
			return "", err
		}
		defer zwc.Close()
		eng := storage.NewInternalEngine()
		if r != nil {
			flowgraph.PrependFileScan([]string{"stdio:stdin"})
			eng.AddReader("stdio:stdin", r)
		}
		comp := compiler.NewCompiler(eng)
		query, err := runtime.CompileQuery(context.Background(), super.NewContext(), comp, flowgraph)
		if err != nil {
			return "", err
		}
		defer query.Pull(true)
		if err := vio.Copy(zwc, query); err != nil {
			return "", err
		}
		if err := zwc.Close(); err != nil {
			return "", err
		}
		return buf.String(), nil
	})
}

func parse(program string) (interface{}, error) {
	ast, err := parser.ParseText(program)
	result := ParseResult{AST: ast, Error: err}

	return result, nil
}

type ParseResult struct {
	AST   interface{} `wasm:"ast"`
	Error error       `wasm:"error"`
}

func readableStream(readable js.Value) io.Reader {
	pr, pw := io.Pipe()
	go func() {
		reader := readable.Call("getReader")
		var buf []byte
		for {
			var ch chunk
			if err := await(reader.Call("read"), &ch); ch.Done || err != nil {
				pw.CloseWithError(err)
				return
			}
			n := ch.Value.Length()
			buf = slices.Grow(buf, n)
			b := buf[:n]
			js.CopyBytesToGo(b, ch.Value)
			pw.Write(b)
		}
	}()
	return pr
}

func await(prom js.Value, v interface{}) error {
	err := make(chan error)
	prom.Call("then", js.FuncOf(func(this js.Value, args []js.Value) interface{} {
		if len(args) > 0 && v != nil {
			err <- wasm.FromJSValue(args[0], v)
			return nil
		}
		err <- nil
		return nil
	}))
	prom.Call("catch", js.FuncOf(func(this js.Value, args []js.Value) interface{} {
		err <- errors.New(args[0].Call("toString").String())
		return nil
	}))
	return <-err
}
