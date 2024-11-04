package decorators

import (
	"bytes"
	"container/list"
	"errors"
	"flag"
	"fmt"
	"io"
	"log/slog"
	"socks.it/proxy"
	"socks.it/utils/errs"
	"strconv"
	"strings"
	"time"
	"unsafe"
)

var enableGatherStat = flag.Bool("enableGatherStat", false, "Dump gather statistics periodically")

const headLen = int(unsafe.Sizeof(int64(0)))

type gatherTransport struct {
	*proxy.ReadonlyDecorator
	maxDelay     time.Duration
	maxPacketLen int // hard limit
	logger       *slog.Logger

	delayTimer   *time.Timer
	dumpTimer    *time.Timer
	lowerReader  io.Reader
	lowerWriter  io.WriteCloser
	wroteBytes   int
	gatherCount  int
	writePackets list.List
	readPackets  list.List

	timesPerGatherCount []int64
}

type eofReader struct{}

func (r *eofReader) Read([]byte) (int, error) {
	return 0, io.EOF
}

func NewGather(lower proxy.Transporter, maxDelay time.Duration, maxPacketLen int, logger *slog.Logger) proxy.TransportDecorator {
	d := gatherTransport{
		ReadonlyDecorator: proxy.NewReadonlyTransport(lower),
		maxDelay:          maxDelay,
		maxPacketLen:      maxPacketLen,
		logger:            logger,
		lowerReader:       &eofReader{},
	}

	d.delayTimer = time.AfterFunc(maxDelay, func() {
		d.ReadonlyDecorator.Submit(gatherDelay{})
	})

	if *enableGatherStat {
		const dumpPeriod = 5 * time.Minute
		d.dumpTimer = time.AfterFunc(dumpPeriod, func() {
			d.Submit(gatherDump{})
			d.dumpTimer.Reset(dumpPeriod)
		})
	}

	d.timesPerGatherCount = make([]int64, 8)

	return &d
}

func (d *gatherTransport) MetaLength() int {
	return headLen + d.ReadonlyDecorator.MetaLength()
}

func (d *gatherTransport) NextWriter() (w io.WriteCloser, err error) {
	//d.logger.Debug("limitWriter new")

	w = &limitWriter{
		gatherTransport: d,
	}
	return w, nil
}

type limitWriter struct {
	*gatherTransport

	reader   io.Reader
	commited int
}

func (w *limitWriter) Write(p []byte) (n int, err error) {
	//w.logger.Debug("limitWriter write", "ptr", fmt.Sprintf("%p", p), "data", string(p))

	w.commited += len(p)
	if headLen+w.commited > w.maxPacketLen {
		return 0, io.ErrShortBuffer
	}

	if w.reader == nil {
		w.reader = bytes.NewReader(p)
	} else {
		w.reader = io.MultiReader(w.reader, bytes.NewReader(p))
	}

	return len(p), nil
}

func (w *limitWriter) Close() error {
	//w.logger.Debug("limitWriter close")

	_, err := w.write(w.reader, w.commited)
	return err
}

func (d *gatherTransport) write(r io.Reader, dataLen int) (n int, err error) {
newWriter:
	if d.lowerWriter == nil {
		if d.lowerWriter, err = d.ReadonlyDecorator.NextWriter(); err != nil {
			return
		}
		d.wroteBytes = 0
		d.gatherCount = 0
		d.delayTimer.Reset(d.maxDelay)
	}

	// 如果 headLen+dataLen>d.MaxPacketLen，那么需要排除d.currentWrote为0的情况（无法发送报文）
	// if d.wroteBytes > 0 && d.wroteBytes+headLen+dataLen > d.MaxPacketLen {
	// limitWriteCloser 已经保证了单个数据包不能大于maxPacketLen
	if d.wroteBytes+headLen+dataLen > d.maxPacketLen {
		//d.logger.Debug("need flush", "size", d.wroteBytes+headLen+dataLen, "limit", d.maxPacketLen)
		n = d.wroteBytes

		if err = d.flush(); err != nil {
			return
		}
		goto newWriter
	}

	_, err = io.Copy(d.lowerWriter, strings.NewReader(fmt.Sprintf("%08X", dataLen)))
	if err != nil {
		return 0, errs.WithStack(err)
	}
	d.wroteBytes += headLen

	/** Debug purpose */
	//buf := new(bytes.Buffer)
	//r = io.TeeReader(r, buf)
	//defer func() {
	//	d.logger.Debug("write data", "data", buf.String())
	//}()

	_, err = io.Copy(d.lowerWriter, r)
	if err != nil {
		return
	}

	d.wroteBytes += dataLen
	d.gatherCount++

	if d.wroteBytes == d.maxPacketLen {
		return d.wroteBytes, d.flush()
	}

	if d.wroteBytes > d.maxPacketLen {
		panic("too many bytes")
	}

	return
}

func (d *gatherTransport) flush() (err error) {
	// may be nil on delayTimer triggerred flush 可能为空
	if d.lowerWriter != nil {
		//d.logger.Debug("lower flush")
		err = d.lowerWriter.Close()
		d.lowerWriter = nil
		for len(d.timesPerGatherCount) <= d.gatherCount {
			d.timesPerGatherCount = append(d.timesPerGatherCount, 0)
		}
		d.timesPerGatherCount[d.gatherCount]++
	}

	return
}

func (d *gatherTransport) NextReader() (io.Reader, error) {
	lenBuf := make([]byte, 8)
	if _, err := io.ReadFull(d.lowerReader, lenBuf); err != nil {
		if !errors.Is(err, io.EOF) {
			return nil, errs.WithStack(err)
		}

		//d.logger.Debug("lower NextReader")
		d.lowerReader, err = d.ReadonlyDecorator.NextReader()
		if err != nil {
			return nil, err
		}

		if _, err = io.ReadFull(d.lowerReader, lenBuf); err != nil {
			return nil, errs.WithStack(err)
		}
	}

	length, err := strconv.ParseInt(string(lenBuf), 16, 32)
	if err != nil {
		return nil, errs.WithStack(err)
	}

	return io.LimitReader(d.lowerReader, length), nil
}

type gatherDelay struct{}
type gatherDump struct{}

func (d *gatherTransport) Handle(event any) error {
	switch event.(type) {
	case gatherDelay:
		return d.flush()
	case gatherDump:
		d.dumpStat()
		return nil
	default:
		return d.ReadonlyDecorator.Handle(event)
	}
}

func (d *gatherTransport) Close() error {
	if *enableGatherStat {
		d.dumpTimer.Stop()
	}
	d.delayTimer.Stop()

	d.logger.Info("gather closed")
	d.dumpStat()

	return d.ReadonlyDecorator.Close()
}

func (d *gatherTransport) dumpStat() {
	const MaxRecord = 8

	type record struct {
		count int
		times int64
	}

	records := make([]record, 0, MaxRecord)
	for count, times := range d.timesPerGatherCount {
		if times > 0 {
			records = append(records, record{count, times})
		}
		if len(records) == MaxRecord {
			d.logger.Info("statistics", "value", fmt.Sprintf("{count times}%v", records))
			records = records[:0]
		}
	}
	if len(records) > 0 {
		d.logger.Info("statistics", "value", fmt.Sprintf("{count times}%v", records))
	}
}
