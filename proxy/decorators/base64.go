package decorators

import (
	"encoding/base64"
	"io"
	"log/slog"
	"socks.it/proxy"
	"socks.it/utils/errs"
)

type base64Transport struct {
	*proxy.TransformDecorator
	logger *slog.Logger
}

func NewBase64Transport(lower proxy.Transporter, logger *slog.Logger) proxy.TransportDecorator {
	return &base64Transport{proxy.NewTransformTransport(lower), logger}
}

func (t *base64Transport) MetaLength() int {
	return t.TransformDecorator.MetaLength()
}

func (t *base64Transport) NextWriter() (io.WriteCloser, error) {
	lower, err := t.TransformDecorator.NextWriter()
	if err != nil {
		return nil, err
	}
	encoder := base64.NewEncoder(base64.StdEncoding, lower)
	return &base64Writer{lower: lower, encoder: encoder}, nil
}

type base64Writer struct {
	lower   io.WriteCloser
	encoder io.WriteCloser
}

func (w *base64Writer) Write(p []byte) (n int, err error) {
	return w.encoder.Write(p)
}

func (w *base64Writer) Close() error {
	if err := w.encoder.Close(); err != nil {
		return errs.WithStack(err)
	}

	return errs.WithStack(w.lower.Close())
}

func (t *base64Transport) NextReader() (io.Reader, error) {
	lower, err := t.TransformDecorator.NextReader()
	if err != nil {
		return nil, err
	}

	return base64.NewDecoder(base64.StdEncoding, lower), nil
}

func (t *base64Transport) Close() error {
	t.logger.Info("base64 closed")

	return t.TransformDecorator.Close()
}
