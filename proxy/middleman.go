package proxy

import (
	"time"
)

/// Tunnel is built upon Transporter.
///
/// Send Procedure:
/// Client/Server Reader(binding to Tunnel) -> Transporter&Decorator -> Send by Middleman
///
/// Receive Procedure:
/// Received by Middleman -> Transporter&Decorator -> Client/Server Writer(binding to Tunnel)

const (
	PullChanSize = 512
	PushChanSize = 512

	TunnelIdleTimeout = 10 * time.Minute
)

type Middleman interface {
	Setup() error
	Teardown() error
	NewTransport() (Transporter, error)
	WriteSpace() int

	// 2024/10/22:
	//Ordered() bool
}
