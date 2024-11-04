package proxy

import "io"

type EventSubmitter interface {
	// Attach provides a channel for Transport to handle asynchronous events, allowing it to deliver events from
	// its routine to the send thread for execution, which can avoid some data locking.
	// When using multiple layers of Transporter, it is only necessary to configure a Watcher for the top layer.
	// Events are processed from the top layer downward, and unhandled events are forwarded to the lower layer.
	Attach(chan<- any)

	Submit(event any)
}

type EventHandler interface {
	// Handle handles event triggered by myself, but in the writing routine.
	Handle(event any) error
}

type EventTrigger struct {
	eventCh chan<- any
}

func (e *EventTrigger) Attach(eventCh chan<- any) {
	e.eventCh = eventCh
}

func (e *EventTrigger) Submit(event any) {
	e.eventCh <- event
}

type Transporter interface {
	// NextWriter returns a Writer, Close must be called after one or multiple writes.
	// It is recommended to use the defer function to ensure this is not overlooked.
	// The caller should NOT modify the buffer given to writer.Write.
	NextWriter() (io.WriteCloser, error)

	// NextReader returns a reader which can be read multiple times, but it must ultimately be fully consumed?
	// (At least WebSocket does not require it to be fully read.)
	// Once a Read operation fails, the Reader cannot be used again.
	NextReader() (io.Reader, error)
	io.Closer
}

// TransportDecorator is used to stack multiple layers of Transporter, simplifying and decomposing data processing logic.
// Transport is divided into modifying classes (for data encryption, encoding) and read-only classes (which only add data).
// Stacking must adhere to the following constraints:
// 1. Modifying classes must be placed inside or below read-only classes;
// 2. The order of classes within the same category can be combined in any way;
type TransportDecorator interface {
	Transporter

	EventSubmitter
	EventHandler

	// MetaLength tells length of meta of the decorator plus all the lowers.
	MetaLength() int
}

type transportDecorator struct {
	EventTrigger
	lower Transporter
}

// The decorator takes ownership of lower transport. Never use lower from now on, especially DO NOT call lower.Close.
func newDecorator(lower Transporter) *transportDecorator {
	return &transportDecorator{
		lower: lower,
	}
}

func (d *transportDecorator) MetaLength() int {
	if lower, ok := d.lower.(interface{ MetaLength() int }); ok {
		return lower.MetaLength()
	}
	return 0
}

// NextWriter make type can check in NewTransformTransport work
func (d *transportDecorator) NextWriter() (io.WriteCloser, error) {
	return d.lower.NextWriter()
}

// NextReader make type can check in NewTransformTransport work
func (d *transportDecorator) NextReader() (io.Reader, error) {
	return d.lower.NextReader()
}

// Close make type can check in NewTransformTransport work
func (d *transportDecorator) Close() error {
	return d.lower.Close()
}

func (d *transportDecorator) Attach(ch chan<- any) {
	d.EventTrigger.Attach(ch)
	if lower, ok := d.lower.(EventSubmitter); ok {
		lower.Attach(ch)
	}
}

func (d *transportDecorator) Handle(event any) error {
	if lower, ok := d.lower.(EventHandler); ok {
		return lower.Handle(event)
	}
	return nil
}

type ReadonlyDecorator struct {
	*transportDecorator
}

func NewReadonlyTransport(lower Transporter) *ReadonlyDecorator {
	return &ReadonlyDecorator{
		transportDecorator: newDecorator(lower),
	}
}

type TransformDecorator struct {
	*transportDecorator
}

func NewTransformTransport(lower Transporter) *TransformDecorator {
	if _, readonly := lower.(ReadonlyDecorator); readonly {
		panic("Don't stack TransformDecorator over NewReadonlyTransport")
	}

	return &TransformDecorator{
		transportDecorator: newDecorator(lower),
	}
}
