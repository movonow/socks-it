package test

import (
	"bytes"
	"io"
	"socks.it/proxy"
	"socks.it/utils/errs"
)

func readText(t proxy.Transporter) (string, error) {
	r, err := t.NextReader()
	if err != nil {
		return "", err
	}

	buf := new(bytes.Buffer)
	if _, err = io.Copy(buf, r); err != nil {
		return "", errs.WithStack(err)
	}

	return buf.String(), nil
}

func writeText(t proxy.Transporter, texts ...string) error {
	w, err := t.NextWriter()
	if err != nil {
		return err
	}

	for _, data := range texts {
		if _, err = w.Write([]byte(data)); err != nil {
			return err
		}
	}

	return w.Close()
}

//func Test_transport_Composite(t *testing.T) {
//	var logger *slog.Logger
//	var flush func()
//	logger, flush = logs.GetLogger("transport.log", "Debug")
//	defer flush()
//
//	testCases := []struct {
//		id   int
//		data string
//	}{
//		{1, "hello"},
//	}
//	for _, tc := range testCases {
//		t.Run(fmt.Sprintf("%d in %s", tc.id, tc.data), func(t *testing.T) {
//			ch1 := make(chan []byte)
//			ch2 := make(chan []byte)
//			defer close(ch2)
//			defer close(ch1)
//
//			var counter int64
//
//			{
//				counter++
//				transport := NewConfirm(ConfirmOption{
//					Delegate:  newMockTransport(ch1, ch2),
//					MaxWindow: 10,
//					Logger:    logger,
//					UID:       counter,
//				})
//				defer func(transport proxy.Transporter) {
//					_ = transport.Close()
//				}(transport)
//
//				transport = NewGather(
//					transport,
//					time.Minute,
//					headLen+len(tc.data),
//					logger,
//				)
//				defer func(transport proxy.Transporter) {
//					_ = transport.Close()
//				}(transport)
//
//				if err := writeText(transport, tc.data); err != nil {
//					t.Fatal("transport.Write:", err)
//				}
//			}
//
//			timer := time.AfterFunc(3*time.Second, func() {
//				logger.Error("read packet timeout")
//				close(ch1)
//				close(ch2)
//			})
//			defer timer.Stop()
//
//			{
//				counter++
//				transport := NewConfirm(ConfirmOption{
//					Delegate:  newMockTransport(ch2, ch1),
//					MaxWindow: 10,
//					Logger:    logger,
//					UID:       counter,
//				})
//				defer func(transport proxy.Transporter) {
//					_ = transport.Close()
//				}(transport)
//
//				transport = NewGather(
//					transport,
//					time.Minute,
//					headLen+len(tc.data),
//					logger,
//				)
//				defer func(transport proxy.Transporter) {
//					_ = transport.Close()
//				}(transport)
//
//				got, err := readText(transport)
//				if err != nil {
//					t.Fatal("transport.Read:", err)
//				}
//
//				if got != tc.data {
//					t.Fatalf("transport.Read: want %s, got %s", tc.data, got)
//				}
//			}
//		})
//	}
//}
