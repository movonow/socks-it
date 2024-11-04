package test

import (
	"bytes"
	"context"
	"fmt"
	"math/rand"
	"socks.it/proxy"
	"socks.it/proxy/decorators"
	"socks.it/utils/logs"
	"strings"
	"testing"
	"time"
)

var headLen int

func init() {
	headLen = decorators.NewGather(nil, time.Duration(0), 0, nil).MetaLength()
}

func Test_gatherTransport_WriteNow(t *testing.T) {
	logger := logs.GetLogger("transport.log", "Debug")
	//nopLogger := logs.GetLogger("transport.log", "Off")

	testCases := []struct {
		limit int
		data  string
	}{
		{headLen + len("hello"), "hello"},
	}
	for _, tc := range testCases {
		t.Run(fmt.Sprintf("%d in %s", tc.limit, tc.data), func(t *testing.T) {
			want := fmt.Sprintf("%08X%s", len(tc.data), tc.data)

			ch1 := make(chan []byte, 1)
			ch2 := make(chan []byte, 1)
			defer close(ch1)
			defer close(ch2)

			transport := decorators.NewGather(
				newMockTransport(ch1, ch2),
				time.Minute,
				tc.limit,
				logger)
			defer func() {
				_ = transport.Close()
			}()

			if err := writeText(transport, tc.data); err != nil {
				t.Fatal("transport.Write:", err)
			}

			ctx, cancel := context.WithDeadline(context.Background(), time.Now().Add(time.Second))
			defer cancel()
			select {
			case data := <-ch2:
				if string(data) != want {
					t.Fatalf("transport.Read: want %s, got %s", want, string(data))
				}
			case <-ctx.Done():
				t.Fatal("Read result timeout")
			}
		})
	}
}

func Test_gatherTransport_WriteDelay(t *testing.T) {
	logger := logs.GetLogger("transport.log", "Debug")

	testCases := []struct {
		limit int
		data  string
	}{
		{headLen + len("hello") + 1, "hello"}, // 10
	}

	for _, tc := range testCases {
		t.Run(fmt.Sprintf("%d in %s", tc.limit, tc.data), func(t *testing.T) {
			want := fmt.Sprintf("%08X%s", len(tc.data), tc.data)

			ch1 := make(chan []byte, 1)
			ch2 := make(chan []byte, 1)
			defer close(ch1)
			defer close(ch2)

			maxDelay := time.Second
			transport := decorators.NewGather(
				newMockTransport(ch1, ch2),
				maxDelay,
				tc.limit,
				logger,
			)
			defer func() {
				_ = transport.Close()
			}()

			go func() {
				eventCh := make(chan any)
				transport.Attach(eventCh)

				if err := transport.Handle(<-eventCh); err != nil {
					logger.Error("transport.HandleEvent", "err", err)
				}
			}()

			if err := writeText(transport, tc.data); err != nil {
				t.Fatal("transport.Write:", err)
			}

			writeTime := time.Now()

			ctx, cancel := context.WithDeadline(context.Background(), time.Now().Add(2*maxDelay))
			defer cancel()

			select {
			case data := <-ch2:
				if string(data) != want {
					t.Fatalf("transport.Read: want %s, got %s", want, string(data))
				}
				if time.Now().Before(writeTime.Add(maxDelay)) {
					t.Fatal("Read result too soon, transport not working?")
				}
			case <-ctx.Done():
				t.Fatal("Read result timeout")
			}
		})
	}
}

func Test_gatherTransport_WriteGather(t *testing.T) {
	logger := logs.GetLogger("transport.log", "Debug")

	testCases := []struct {
		limit int
		d1    string
		d2    string
	}{
		{headLen + len("hello") + headLen + len("world"), "hello", "world"}, // 10
	}

	for _, tc := range testCases {
		t.Run(fmt.Sprintf("%d in %s%s", tc.limit, tc.d1, tc.d2), func(t *testing.T) {
			want := fmt.Sprintf("%08X%s%08X%s", len(tc.d1), tc.d1, len(tc.d2), tc.d2)

			ch1 := make(chan []byte, 2)
			ch2 := make(chan []byte, 2)
			defer close(ch1)
			defer close(ch2)

			maxDelay := time.Minute
			transport := decorators.NewGather(
				newMockTransport(ch1, ch2),
				maxDelay,
				tc.limit,
				logger,
			)
			defer func() {
				_ = transport.Close()
			}()

			if err := writeText(transport, tc.d1); err != nil {
				t.Fatal("transport.Write:", err)
			}

			writeD1 := time.Now()

			ctx, cancel := context.WithDeadline(context.Background(), time.Now().Add(time.Second))
			defer cancel()

			select {
			case _ = <-ch2:
				t.Fatal("Read result in 1s, transport not working?")
			case <-ctx.Done():
				logger.Info("1s elapsed")
			}

			if err := writeText(transport, tc.d2); err != nil {
				t.Fatal("transport.Write:", err)
			}

			ctx2, cancel2 := context.WithDeadline(context.Background(), time.Now().Add(time.Second))
			defer cancel2()
			select {
			case data := <-ch2:
				if string(data) != want {
					t.Fatalf("transport.Read: want %s, got %s", want, string(data))
				}
				if time.Now().After(writeD1.Add(maxDelay)) {
					t.Fatal("Read result too soon, transport not working?")
				}
			case <-ctx2.Done():
				t.Fatal("Read result timeout")
			}
		})
	}
}

func Test_gatherTransport_WriteFlush(t *testing.T) {
	logger := logs.GetLogger("transport.log", "Debug")

	testCases := []struct {
		id int
		d1 string
		d2 string
	}{
		{1, "hello", "helloX"}, // 10
	}

	for _, tc := range testCases {
		t.Run(fmt.Sprintf("%d ", tc.id), func(t *testing.T) {
			ch1 := make(chan []byte)
			ch2 := make(chan []byte, 2)
			defer close(ch1)
			defer close(ch2)

			maxDelay := 500 * time.Millisecond
			transport := decorators.NewGather(
				newMockTransport(ch1, ch2),
				maxDelay,
				headLen+len(tc.d1)+1, //第一次报文会缓存
				logger,
			)
			defer func() {
				_ = transport.Close()
			}()

			//writeTime := time.Now()
			if err := writeText(transport, tc.d1); err != nil {
				t.Fatal("transport.Write:", err)
			}

			ctx, cancel := context.WithDeadline(context.Background(), time.Now().Add(100*time.Millisecond))
			defer cancel()
			select {
			case _ = <-ch2:
				t.Fatalf("Read result in 1s, should delay %dms", maxDelay/time.Millisecond)
			case <-ctx.Done():
				logger.Info("1s elapsed")
			}

			if err := writeText(transport, tc.d2); err != nil {
				t.Fatal("transport.Write:", err)
			}

			ctx, cancel = context.WithDeadline(context.Background(), time.Now().Add(100*time.Millisecond))
			defer cancel()
			for _, data := range []string{tc.d1, tc.d2} {
				select {
				case got := <-ch2:
					logger.Debug("got packet", "content", string(got))
					if !strings.HasSuffix(string(got), data) {
						logger.Error("assert failed", "got", string(got), "want", data)
						t.FailNow()
					}
				case <-ctx.Done():
					t.Fatal("read result timeout")
				}
			}
		})
	}
}

func Test_gatherTransport_Read(t *testing.T) {
	logger := logs.GetLogger("transport.log", "Debug")
	nopLogger := logs.GetLogger("transport.log", "Off")

	testCases := []struct {
		limit int
		data  []string
	}{
		{32, []string{"hello"}},
		{32, []string{"hello", "world"}},
	}
	for _, tc := range testCases {
		t.Run(fmt.Sprintf("%d in %s", tc.limit, tc.data), func(t *testing.T) {
			closed := false
			ch1 := make(chan []byte, 4)
			ch2 := make(chan []byte, 4)
			defer func() {
				if !closed {
					close(ch1)
					close(ch2)
				}
			}()

			// write packet
			{
				transport := decorators.NewGather(
					newMockTransport(ch1, ch2, withWriteAction(func(i int, buffer *bytes.Buffer) (terminate bool) {
						logger.Debug("Write packet", "content", buffer.String())
						return
					})),
					10*time.Millisecond,
					tc.limit,
					logger,
				)
				defer func() {
					_ = transport.Close()
				}()

				go func() {
					eventCh := make(chan any)
					transport.Attach(eventCh)

					if err := transport.Handle(<-eventCh); err != nil {
						logger.Error("transport.HandleEvent", "err", err)
					}
				}()

				if err := writeText(transport, tc.data...); err != nil {
					t.Fatal("transport.Write:", err)
				}
			}

			timer := time.AfterFunc(3*time.Second, func() {
				logger.Error("read packet timeout")
				closed = true
				close(ch1)
				close(ch2)
			})
			defer timer.Stop()

			//logger.Debug("ch2", "content", string(<-ch2))
			// read packet
			{
				transport := decorators.NewGather(
					newMockTransport(ch2, ch1, withReadAction(func(i int, buffer *bytes.Buffer) (terminate bool) {
						logger.Debug("Read packet", "content", buffer.String())
						return
					})),
					time.Second,
					tc.limit,
					nopLogger,
				)
				defer func() {
					_ = transport.Close()
				}()

				builder := new(strings.Builder)
				for _, data := range tc.data {
					builder.WriteString(data)
				}
				want := builder.String()

				got, err := readText(transport)
				//logger.Debug("got", "content", got)
				if err != nil {
					logger.Debug("transport.Read:", "error", err)
				}

				if got != want {
					logger.Debug("assert failed", "want", want, "got", got)
				}
			}
		})
	}
}

func Test_gatherTransport_ReadGathered(t *testing.T) {
	logger := logs.GetLogger("transport.log", "Debug")
	nopLogger := logs.GetLogger("transport.log", "Off")

	testCases := []struct {
		limit int
		data  []string
	}{
		{32, []string{"hello", "world"}},
	}
	for _, tc := range testCases {
		t.Run(fmt.Sprintf("%d in %s", tc.limit, tc.data), func(t *testing.T) {
			closed := false
			ch1 := make(chan []byte, 4)
			ch2 := make(chan []byte, 4)
			defer func() {
				if !closed {
					close(ch1)
					close(ch2)
				}
			}()

			// write packet
			{
				transport := decorators.NewGather(
					newMockTransport(ch1, ch2, withWriteAction(func(i int, buffer *bytes.Buffer) (terminate bool) {
						logger.Debug("Write packet", "content", buffer.String())
						return
					})),
					100*time.Millisecond,
					tc.limit,
					logger,
				)
				defer func() {
					_ = transport.Close()
				}()

				go func() {
					eventCh := make(chan any)
					transport.Attach(eventCh)

					if err := transport.Handle(<-eventCh); err != nil {
						logger.Error("transport.HandleEvent", "err", err)
					}
				}()

				for _, data := range tc.data {
					if err := writeText(transport, data); err != nil {
						t.Fatal("transport.Write:", err)
					}
				}
			}

			timer := time.AfterFunc(3*time.Second, func() {
				logger.Error("read packet timeout")
				closed = true
				close(ch1)
				close(ch2)
			})
			defer timer.Stop()

			//logger.Debug("ch2", "content", string(<-ch2))
			// read packet
			{
				transport := decorators.NewGather(
					newMockTransport(ch2, ch1, withReadAction(func(i int, buffer *bytes.Buffer) (terminate bool) {
						logger.Debug("Read packet", "content", buffer.String())
						return
					})),
					time.Second,
					tc.limit,
					nopLogger,
				)
				defer func() {
					_ = transport.Close()
				}()

				for _, data := range tc.data {
					got, err := readText(transport)
					//logger.Debug("got", "content", got)
					if err != nil {
						t.Fatal("transport.Read:", err)
					}

					if got != data {
						logger.Debug("assert failed", "want", data, "got", got)
					}
				}
			}
		})
	}
}

func Test_gatherTransport_WriteSpaceLimit(t *testing.T) {
	logger := logs.GetLogger("transport.log", "Debug")
	nopLogger := logs.GetLogger("transport.log", "Off")

	const dataLength = 5
	testCases := []struct {
		limit int
		data  []string
	}{
		{headLen + dataLength, []string{strings.Repeat("X", dataLength), strings.Repeat("O", dataLength)}},
	}
	for _, tc := range testCases {
		t.Run(fmt.Sprintf("%d in %s", tc.limit, tc.data), func(t *testing.T) {
			closed := false
			ch1 := make(chan []byte, 4)
			ch2 := make(chan []byte, 4)
			defer func() {
				if !closed {
					close(ch1)
					close(ch2)
				}
			}()

			// write packet
			{
				transport := decorators.NewGather(
					newMockTransport(ch1, ch2,
						withWriteSpace(headLen+dataLength),
						withWriteAction(func(i int, buffer *bytes.Buffer) (terminate bool) {
							logger.Debug("Write packet", "content", buffer.String())
							return
						})),
					100*time.Millisecond,
					tc.limit,
					logger,
				)
				defer func() {
					_ = transport.Close()
				}()

				go func() {
					eventCh := make(chan any)
					transport.Attach(eventCh)

					//if err := transport.Handle(<-eventCh); err != nil {
					//	logger.Error("transport.HandleEvent", "err", err)
					//}
				}()

				for _, data := range tc.data {
					if err := writeText(transport, data); err != nil {
						t.Fatal("transport.Write:", err)
					}
				}
			}

			timer := time.AfterFunc(3*time.Second, func() {
				logger.Error("read packet timeout")
				closed = true
				close(ch1)
				close(ch2)
			})
			defer timer.Stop()

			//logger.Debug("ch2", "content", string(<-ch2))
			// read packet
			{
				transport := decorators.NewGather(
					newMockTransport(ch2, ch1, withReadAction(func(i int, buffer *bytes.Buffer) (terminate bool) {
						logger.Debug("Read packet", "content", buffer.String())
						return
					})),
					time.Second,
					tc.limit,
					nopLogger,
				)
				defer func() {
					_ = transport.Close()
				}()

				for _, data := range tc.data {
					got, err := readText(transport)
					//logger.Debug("got", "content", got)
					if err != nil {
						t.Fatal("transport.Read:", err)
					}

					if got != data {
						logger.Debug("assert failed", "want", data, "got", got)
					}
				}
			}
		})
	}
}

func Test_gatherTransport_Statistics(t *testing.T) {
	t.Skip("测试时间太长了.")

	rand.NewSource(time.Now().UnixNano())

	logger := logs.GetLogger("transport.log", "Debug")
	//nopLogger := logs.GetLogger("transport.log", "Off")

	var closed bool
	ch1 := make(chan []byte)
	ch2 := make(chan []byte)
	defer func() {
		if !closed {
			logger.Error("test finished, close channels")
			close(ch1)
			close(ch2)
		}
	}()

	var counter int64
	errCh := make(chan error)

	const maxPacketLen = 32
	wroteData := new(strings.Builder)
	wroteTimes := 0

	{
		counter++

		transport := decorators.NewGather(
			newMockTransport(ch1, ch2),
			200*time.Millisecond,
			maxPacketLen,
			logger,
		)
		defer func(transport proxy.Transporter) {
			_ = transport.Close()
		}(transport)

		go func() {
			begin := time.Now()

			for i := 1; ; i++ {
				if time.Now().After(begin.Add(10 * time.Second)) {
					errCh <- nil
					break
				}

				time.Sleep(time.Duration(rand.Int()%200+50) * time.Millisecond)
				alphabet := "abcdefghijklmnopqrstuvwxyz"
				offset := i % len(alphabet)
				data := strings.Repeat(alphabet[offset:offset+1], rand.Int()%(maxPacketLen-headLen)+1)
				if err := writeText(transport, data); err != nil {
					errCh <- err
					break
				}
				//logger.Debug("write", "index", i, "data", data)
				wroteData.WriteString(data)
				wroteTimes++
			}
		}()
	}

	timer := time.AfterFunc(time.Minute, func() {
		logger.Error("readData timeout, close channels")
		closed = true
		close(ch1)
		close(ch2)
	})
	defer timer.Stop()

	readData := new(strings.Builder)
	readTimes := 0
	{
		counter++
		transport := decorators.NewGather(
			newMockTransport(ch2, ch1),
			time.Minute,
			maxPacketLen,
			logger,
		)
		defer func(transport proxy.Transporter) {
			_ = transport.Close()
		}(transport)

		writeDone := false
	loop:
		for {
			got, err := readText(transport)
			if err != nil {
				logger.Error("read text failed", "error", err)
				t.FailNow()
			}
			readData.WriteString(got)
			readTimes++

			select {
			case err = <-errCh:
				if err != nil {
					logger.Error("write text failed", "error", err)
					writeDone = true
				}
				break loop
			default:
			}

			if writeDone && readTimes == wroteTimes {
				break loop
			}
		}
	}

	if wroteData.String() != readData.String() {
		logger.Error("assert failed", "wrote", wroteData.Len(), "read", readData.Len())
		t.Log(wroteData.String())
		t.Log(readData.String())
		t.FailNow()
	}
}
