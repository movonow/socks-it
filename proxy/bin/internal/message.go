package internal

import (
	"bytes"
	"encoding/base64"
	"encoding/gob"
	"github.com/things-go/go-socks5/statute"
	"net"
	"os"
	"socks.it/utils/errs"
	"strings"
	"syscall"
)

const (
	CommandBegin = iota
	Connect
	ConnectAck
	Execute
	ExecuteAck
	Forward
	Close
	CommandEnd
)

type Command int

func (cmd Command) String() string {
	switch cmd {
	case Connect:
		return "Connect"
	case ConnectAck:
		return "ConnectAck"
	case Execute:
		return "Execute"
	case ExecuteAck:
		return "ExecuteAck"
	case Forward:
		return "Forward"
	case Close:
		return "Close"
	default:
		return "unknown command"
	}
}

type Bundle struct {
	*Tunnel // fixme: Is it safe leave it in Push channel after it was Closed?
	Command
	Data []byte
}

type OpenRequest struct {
	// ClientAddr is observational information for debugging:
	// the client and server are associated with the same TCP connection details.
	ClientAddr net.Addr

	// ServerAddr compared to net.Addr, this additionally supports domain names.
	// Typically, clients cannot resolve internal network services.
	ServerAddr statute.AddrSpec
}

type OpenResponse struct {
	// BindAddr is the local address used by the server proxy to initiate a TCP connection.
	BindAddr net.Addr

	// ServerAddr is the resolved OpenRequest.ServerAddr.
	ServerAddr net.Addr
	Error      error
}

type Disconnect struct {
	Error error
}

func init() {
	gob.Register(&net.TCPAddr{})
	gob.Register(&net.OpError{})
	gob.Register(&os.SyscallError{})
	gob.Register(syscall.Errno(0))
}

func (r *OpenRequest) Encode() (string, error) {
	return encode(r)
}

func (r *OpenRequest) Decode(data string) error {
	return decode(data, r)
}

func (r *OpenResponse) Encode() (string, error) {
	return encode(r)
}

func (r *OpenResponse) Decode(data string) error {
	return decode(data, r)
}

func (r *Disconnect) Encode() (string, error) {
	return encode(r)
}

func (r *Disconnect) Decode(data string) error {
	return decode(data, r)
}

func encode(message any) (string, error) {
	buf := new(bytes.Buffer)
	encoder := base64.NewEncoder(base64.StdEncoding, buf)
	if err := gob.NewEncoder(encoder).Encode(message); err != nil {
		return "", errs.WithStack(err)
	}
	if err := encoder.Close(); err != nil {
		return "", errs.WithStack(err)
	}
	return buf.String(), nil
}

func decode(data string, value any) error {
	decoder := base64.NewDecoder(base64.StdEncoding, strings.NewReader(data))
	return errs.WithStack(gob.NewDecoder(decoder).Decode(value))
}
