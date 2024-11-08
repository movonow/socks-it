package internal

import (
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"socks.it/proxy"
	"socks.it/utils/errs"
)

type multiplexDecorator struct {
	*proxy.ReadonlyDecorator
	logger     *slog.Logger
	writeHead  tunnelHead
	readHead   tunnelHead
	metaLength int
}

func newMultiplexer(lower proxy.Transporter, logger *slog.Logger) *multiplexDecorator {
	return &multiplexDecorator{
		ReadonlyDecorator: proxy.NewReadonlyTransport(lower),
		logger:            logger,
	}
}

func (d *multiplexDecorator) setWriteHead(h *tunnelHead) {
	d.writeHead = *h
}

func (d *multiplexDecorator) ReadHead() tunnelHead {
	return d.readHead
}

func (d *multiplexDecorator) MetaLength() int {
	if d.metaLength == 0 {
		var longestCommand string
		for i := CommandBegin + 1; i < CommandEnd; i++ {
			if len(longestCommand) < len(Command(i).String()) {
				longestCommand = Command(i).String()
			}
		}

		maxHead := tunnelHead{
			From:      "A reasonable long name",
			To:        "A reasonable long name",
			MessageID: ConnectAck,
			TunnelID:  fmt.Sprintf("%v", maxTunnelID-1),
			Command:   longestCommand,
		}
		data, err := json.Marshal(&maxHead)
		if err != nil {
			panic(err)
		}

		d.metaLength = len(data) + d.ReadonlyDecorator.MetaLength()
	}

	return d.metaLength
}

func (d *multiplexDecorator) NextWriter() (io.WriteCloser, error) {
	lower, err := d.ReadonlyDecorator.NextWriter()
	if err != nil {
		return nil, err
	}

	data, err := json.Marshal(&d.writeHead)
	if err != nil {
		return nil, errs.WithStack(err)
	}

	if len(data) > d.metaLength {
		panic("head length exceeds meta length")
	}

	if _, err = lower.Write(data); err != nil {
		return nil, errs.WithStack(err)
	}

	return lower, nil
}

func (d *multiplexDecorator) NextReader() (io.Reader, error) {
	r, err := d.ReadonlyDecorator.NextReader()
	if err != nil {
		return nil, err
	}

	//buf := new(bytes.Buffer)
	//r = io.TeeReader(r, buf)
	//defer func() {
	//	d.logger.Debug("decode route", "data", buf.String())
	//}()

	decoder := json.NewDecoder(r)
	if err = decoder.Decode(&d.readHead); err != nil {
		return nil, errs.WithStack(err)
	}

	r = io.MultiReader(decoder.Buffered(), r)

	//json.NewEncoder() 在尾部写入了换行符，需要吃掉
	//newline := make([]byte, 1)
	//if _, err = io.ReadFull(r, newline); err != nil {
	//	return nil, errs.WithStack(err)
	//}

	return r, nil
}

func (d *multiplexDecorator) Close() error {
	d.logger.Info("route closed")
	return d.ReadonlyDecorator.Close()
}
