package errs

import (
	"fmt"
	"log/slog"
	"runtime"
)

const (
	stackSkip = 1
	stackMax  = 6 // Should not be greater than 10
)

type wrapper struct {
	err error
}

func (w *wrapper) Error() string {
	return w.err.Error()
}

func (w *wrapper) Unwrap() error {
	return w.err
}

type withSource struct {
	wrapper
	file string
	line int
}

func WithSource(err error) error {
	if err == nil {
		return nil
	}

	_, file, line, _ := runtime.Caller(stackSkip)
	return &withSource{
		wrapper{err: err},
		file,
		line,
	}
}

func (e *withSource) LogValue() slog.Value {
	return slog.GroupValue(
		slog.String("reason", e.err.Error()),
		slog.String("source", fmt.Sprintf("%s:%d", e.file, e.line)))
}

type withStack struct {
	wrapper
	frames *runtime.Frames
}

func WithStack(err error) error {
	if err == nil {
		return nil
	}

	pcs := make([]uintptr, stackMax+1)
	n := runtime.Callers(stackSkip+1, pcs)

	return &withStack{
		wrapper{err: err},
		runtime.CallersFrames(pcs[:n]), // CallersFrames is cool with nil/empty slice.
	}
}

func (e *withStack) LogValue() slog.Value {
	attrs := make([]slog.Attr, 1, 4)

	attrs[0] = slog.String("reason", e.err.Error())

	i := 0
	for {
		frame, more := e.frames.Next()
		if !more {
			break
		}
		attrs = append(attrs,
			slog.String(
				fmt.Sprintf("frame%d", i),
				fmt.Sprintf("%s:%d", frame.File, frame.Line)))
		//fmt.Sprintf("%s (%s:%d)", frame.Function, frame.File, frame.Line)))
		i++
	}

	return slog.GroupValue(attrs...)
}
