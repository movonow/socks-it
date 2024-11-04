package test

import (
	"bytes"
	"io"
	"socks.it/proxy"
)

type mockTransport struct {
	readIndex  int
	writeIndex int
	writeSpace int
	readCh     <-chan []byte
	writeCh    chan<- []byte
	onRead     actionFunc
	onWrite    actionFunc
}

func newMockTransport(r <-chan []byte, w chan<- []byte, options ...interception) proxy.Transporter {
	m := mockTransport{readCh: r, writeCh: w, writeSpace: 1000}
	for _, o := range options {
		o(&m)
	}
	return &m
}

type actionFunc func(int, *bytes.Buffer) (terminate bool)

type interception func(*mockTransport)

func withReadAction(action actionFunc) interception {
	return func(ch *mockTransport) {
		ch.onRead = action
	}
}

func withWriteAction(action actionFunc) interception {
	return func(ch *mockTransport) {
		ch.onWrite = action
	}
}

func withWriteSpace(writeSpace int) interception {
	return func(ch *mockTransport) {
		ch.writeSpace = writeSpace
	}
}

func (m *mockTransport) WithWatcher(_ chan<- any) {
	//panic("come on, I am mock")
}

func (m *mockTransport) HandleEvent(_ any) error {
	return nil
}

func (m *mockTransport) NextWriter() (io.WriteCloser, error) {
	m.writeIndex++
	return &channelWriter{ch: m.writeCh, writeSpace: m.writeSpace, writeIndex: m.writeIndex, onWrite: m.onWrite}, nil
}

type channelWriter struct {
	ch  chan<- []byte
	buf bytes.Buffer

	writeSpace int
	writeIndex int
	onWrite    actionFunc
}

func (m *channelWriter) Write(p []byte) (n int, err error) {
	return m.buf.Write(p)
}

func (m *channelWriter) Close() error {
	if m.writeSpace < m.buf.Len() {
		return io.ErrShortBuffer
	}
	if m.onWrite != nil && m.onWrite(m.writeIndex, &m.buf) {
		return nil
	}
	m.ch <- m.buf.Bytes()
	return nil
}

func (m *mockTransport) NextReader() (io.Reader, error) {
	m.readIndex++
	return &channelReader{ch: m.readCh, readIndex: m.readIndex, onRead: m.onRead}, nil
}

func (m *mockTransport) Keepalive() error {
	return nil
}

func (m *mockTransport) Close() error {
	return nil
}

type channelReader struct {
	ch  <-chan []byte
	buf *bytes.Buffer

	readIndex int
	onRead    actionFunc
}

func (m *channelReader) Read(p []byte) (n int, err error) {
	if m.buf == nil {
		m.buf = bytes.NewBuffer(<-m.ch)
		if m.onRead != nil && m.onRead(m.readIndex, m.buf) {
			return
		}
	}

	return m.buf.Read(p)
}
