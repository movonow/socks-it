package errs

import (
	"errors"
	"fmt"
	"log/slog"
	"os"
	"testing"
)

func Test_WithSource(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{
		AddSource: true,
	}))

	err := func() error {
		return func() error {
			return WithSource(fmt.Errorf("bombed"))
		}()
	}()

	logger.Warn("Test_WithStack WithStack", "error", err)
}

type myError struct {
	//error
	code int
}

func (e *myError) Error() string {
	return fmt.Sprintf("myError:%d", e.code)
}

func Test_WithStack(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{
		AddSource: true,
	}))

	err := func() error {
		return func() error {
			return WithStack(&myError{code: 1})
		}()
	}()
	logger.Warn("Test_WithStack", "error", err)
	var myErr *myError
	fmt.Println(errors.As(err, &myErr))
}

func Test_WithStack_extern_function(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{
		AddSource: true,
	}))

	err := a()
	fmt.Println("errors.Is(err, errBoomed)", errors.Is(err, errBoomed))
	logger.Warn("Test_WithStack", "error", err)
}

var errBoomed = errors.New("boomed")

func a() error {
	return b()
}

func b() error {
	return WithStack(errBoomed)
}
