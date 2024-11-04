package internal

import (
	"container/list"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/things-go/go-socks5/statute"
	"io"
	"log/slog"
	"net"
	"socks.it/proxy"
	"socks.it/utils/errs"
	"strings"
	"sync/atomic"
	"time"
)

const (
	listenerName = "listener"
	maxTunnelID  = 1_000_000
)

var (
	tunnelIDCounter = atomic.Uint32{}
)

func nextTunnelID() string {
	return fmt.Sprintf("%06d", tunnelIDCounter.Add(1)%maxTunnelID)
	//return uuid.New().String()
}

type Tunnel struct {
	id string // Client and Server share the same ID in the tunnel.

	messageID int

	nextPullID  int
	messageList list.List

	pushChan chan *Bundle
	pullChan chan []byte // resides in each Tunnel

	// debug, this is identical for both client and server sides.
	clientAddr net.Addr
	serverAddr statute.AddrSpec

	logger *slog.Logger
}

type openRequest struct {
	TunnelID   string `json:"tunnel"`
	Connection string `json:"socket"`
}

func (t *Tunnel) OpenAndServe(_ context.Context, request *OpenRequest, reply func(net.Addr, error) error, exchange func(*Tunnel, *slog.Logger) error) error {
	t.logger = t.logger.With("from", request.ClientAddr.String(), "to", request.ServerAddr.String())

	connection, err := request.Encode()
	if err != nil {
		t.logger.Error("encode request error:", "error", err)
		return err
	}

	var response = new(OpenResponse)
	connectErr := func() error {
		data, err := json.Marshal(&openRequest{
			TunnelID:   t.id,
			Connection: connection,
		})

		if err != nil {
			return errs.WithStack(err)
		}

		t.pushChan <- &Bundle{t, Connect, data}

		timer := time.NewTimer(30 * time.Second)

		select {
		case <-timer.C:
			return errs.WithStack(errors.New("open TunnelID timeout"))
		case d, ok := <-t.pullChan:
			if !ok {
				return errs.WithStack(io.ErrClosedPipe)
			}
			data = d
		}

		if err = response.Decode(string(data)); err != nil {
			return errs.WithStack(err)
		}

		return response.Error
	}()

	if err := reply(response.BindAddr, connectErr); connectErr != nil || err != nil {
		if connectErr != nil {
			t.logger.Warn("open failed", "error", connectErr)
			return connectErr
		}
		return err
	}

	return exchange(t, t.logger)
}

func (t *Tunnel) ListenAndServe(create func(string) *Tunnel, exchange func(*Tunnel, io.ReadWriter, *slog.Logger) error, remove func(*Tunnel)) error {
	for {
		select {
		case data, ok := <-t.pullChan:
			if !ok {
				return errs.WithStack(io.ErrClosedPipe)
			}

			go t.serve(data, create, exchange, remove)
		}
	}
}

func (t *Tunnel) serve(data []byte, create func(string) *Tunnel, exchange func(*Tunnel, io.ReadWriter, *slog.Logger) error, remove func(*Tunnel)) {
	cr := new(openRequest)
	if err := json.Unmarshal(data, cr); err != nil {
		t.logger.Error("unmarshal request failed", "data", string(data), "error", err)
		return
	}

	var request = new(OpenRequest)
	if err := request.Decode(cr.Connection); err != nil {
		t.logger.Error("parse open request failed", "body", cr.Connection, "error", err)
		return
	}

	newTunnel := create(cr.TunnelID)
	defer func() {
		remove(newTunnel)
		_ = newTunnel.Close()
	}()

	newTunnel.clientAddr = request.ClientAddr
	newTunnel.serverAddr = request.ServerAddr
	newTunnel.logger = newTunnel.logger.With("from", request.ClientAddr.String(), "to", request.ServerAddr.String())

	conn, err := net.Dial("tcp", request.ServerAddr.String())
	if err != nil {
		newTunnel.logger.Warn("dial failed", "error", err)
		response := &OpenResponse{Error: err}
		encoded, err := response.Encode()
		if err != nil {
			t.logger.Error("encode response failed", "error", err)
			return
		}
		newTunnel.pushChan <- &Bundle{newTunnel, ConnectAck, []byte(encoded)}
		return
	}

	response := &OpenResponse{
		BindAddr:   conn.LocalAddr(),
		ServerAddr: conn.RemoteAddr(),
	}
	encoded, err := response.Encode()
	if err != nil {
		t.logger.Error("encode response failed", "error", err)
		return
	}
	newTunnel.pushChan <- &Bundle{newTunnel, ConnectAck, []byte(encoded)}

	_ = exchange(newTunnel, conn, newTunnel.logger)
}

func (t *Tunnel) Pusher() chan<- *Bundle {
	return t.pushChan
}

func (t *Tunnel) Puller() <-chan []byte {
	return t.pullChan
}

type bufferedPacket struct {
	head tunnelHead
	data []byte
}

func (t *Tunnel) pull(head *tunnelHead, data []byte) {
	// limit packets count.
	if t.messageList.Len() > proxy.PullChanSize {
		t.logger.Warn("pull buffer queue is full")
		return
	}

	// discard duplicate
	if head.MessageID < t.nextPullID {
		return
	}

	value := &bufferedPacket{*head, data}

	// reorder
	if t.nextPullID < head.MessageID {
		t.logger.Debug("out of order", "want", t.nextPullID, "read", head.MessageID)

		node := t.messageList.Front()
		for ; node != nil; node = node.Next() {
			if node.Value.(*bufferedPacket).head.MessageID > head.MessageID {
				break
			}
		}

		if node != nil {
			t.messageList.InsertBefore(value, node)
		} else {
			t.messageList.PushBack(value)
		}

		return
	}

	t.messageList.PushFront(value)

	node := t.messageList.Front()
loop:
	for node != nil {
		message := node.Value.(*bufferedPacket)
		if message.head.MessageID > t.nextPullID {
			break
		}
		select {
		case t.pullChan <- message.data:
			//t.logger.Debug("pulled packet", "id", message.head.MessageID)
			next := node.Next()
			t.messageList.Remove(node)
			node = next
			t.nextPullID++
		default:
			t.logger.Warn("pullChan is full")
			break loop
		}
	}
}

func (t *Tunnel) newHead(from, to string, command Command) *tunnelHead {
	t.messageID++

	return &tunnelHead{
		From:      from,
		To:        to,
		MessageID: t.messageID,
		TunnelID:  t.id,
		Command:   command.String(),
	}
}

func (t *Tunnel) wait() error {
	// todo: wait my go routines quit.
	return nil
}

func (t *Tunnel) Close() error {
	//close(t.pullChan)  // 因为要同步，在chatroom.Remove中关闭
	return nil
}

type tunnelHead struct {
	From string `json:"from,omitempty"`
	To   string `json:"to"`

	TunnelID  string `json:"tid"` // client and server share the same id in a tunnel.
	Command   string `json:"cmd"`
	MessageID int    `json:"mid"`
}

func (receiver *tunnelHead) LogValue() slog.Value {
	data, err := json.Marshal(receiver)
	if err != nil {
		return slog.StringValue("marshal error")
	}

	var builder strings.Builder
	for _, c := range data {
		if c != '"' {
			if c == ':' {
				c = '='
			}
			builder.WriteByte(c)
		}
	}

	return slog.StringValue(builder.String())
}
