// Package companion is the shared app runtime for a MeshCore device:
// transport selection (BLE by default, USB serial, or TCP over Wi-Fi/
// Ethernet), the startup stored-message sync, a live forwarder into an
// Inbox, and the keepalive/reconnect loop. Each application runs its own
// Service — one companion connection per physical device. The wire protocol
// itself lives in github.com/stempit/meshcore-go.
package companion

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"sync"
	"time"

	"github.com/stempit/meshcore-go"
)

// ConnectOptions selects the device transport. It mirrors the Connection
// config section of the applications so they can map their own config onto
// this package without a shared config dependency.
type ConnectOptions struct {
	// Type selects the transport: "bt" (Bluetooth LE, default), "serial"
	// (USB/UART) or "tcp" (alias "wifi": Wi-Fi/Ethernet companion firmware).
	// Any other non-empty value is a configuration error.
	Type string
	// SerialPort is the OS device path used when Type is "serial"
	// (e.g. "/dev/ttyUSB0", "/dev/tty.usbmodem14101", "COM3").
	SerialPort string
	// SerialBaud sets the baud rate for the serial transport (default 115200).
	SerialBaud int
	// TCPAddress is the device address used when Type is "tcp" — the Wi-Fi or
	// Ethernet companion firmware serves the same protocol on TCP. Either
	// "host:port" or a bare host, which gets the firmware default port 5000.
	TCPAddress string
	// DeviceName selects the BLE device by exact advertised name (e.g.
	// "MeshCore-AB12") when Type is "bt". Empty scans any MeshCore device and
	// picks the strongest signal. The portable identifier on macOS, where the
	// address is a per-machine CoreBluetooth UUID.
	DeviceName string
	// DeviceAddress selects the BLE device by platform address when Type is
	// "bt" (MAC on Linux/Windows, CoreBluetooth UUID on macOS). Combine with
	// DeviceName or use alone.
	DeviceAddress string
}

// Connect opens the meshcore companion device per opts — BLE scan by default,
// USB serial or TCP (Wi-Fi/Ethernet) when asked — and returns a ready client.
// The underlying constructors log and panic when the transport itself cannot
// be established; only configuration errors come back as an error.
func Connect(opts ConnectOptions) (*meshcore.MeshCoreClient, error) {
	switch opts.Type {
	case "serial":
		if opts.SerialPort == "" {
			return nil, errors.New(`Connection.serial_port must be set when Connection.type is "serial"`)
		}
		baud := opts.SerialBaud
		if baud == 0 {
			baud = 115200
		}
		return meshcore.CreateSerialClient(opts.SerialPort, baud), nil
	case "tcp", "wifi":
		if opts.TCPAddress == "" {
			return nil, errors.New(`Connection.tcp_address must be set when Connection.type is "tcp"`)
		}
		return meshcore.CreateTCPClient(tcpDialAddr(opts.TCPAddress)), nil
	case "", "bt":
		var ble []meshcore.BLEOption
		if opts.DeviceName != "" {
			ble = append(ble, meshcore.WithDeviceName(opts.DeviceName))
		}
		if opts.DeviceAddress != "" {
			ble = append(ble, meshcore.WithDeviceAddress(opts.DeviceAddress))
		}
		return meshcore.CreateClient(ble...), nil
	default:
		return nil, fmt.Errorf(`unknown Connection.type %q: want "bt", "serial" or "tcp"`, opts.Type)
	}
}

// tcpDialAddr accepts a bare host and appends the companion firmware's
// default TCP port; "host:port" passes through unchanged. A bare IPv6
// literal is bracketed, since a port cannot be appended to it directly.
func tcpDialAddr(addr string) string {
	if _, _, err := net.SplitHostPort(addr); err == nil {
		return addr
	}
	return net.JoinHostPort(addr, "5000")
}

// Service pairs a connected client with an Inbox and owns the device-side
// runtime: startup sync, live forwarding into the inbox, keepalive.
type Service struct {
	Client *meshcore.MeshCoreClient
	Inbox  *Inbox

	log *slog.Logger
}

// NewService binds a connected client to a fresh Inbox.
func NewService(client *meshcore.MeshCoreClient, logger *slog.Logger) *Service {
	return &Service{Client: client, Inbox: &Inbox{}, log: logger}
}

const (
	syncWait       = 15 * time.Second // bound on the device stored-message sync
	postSyncSettle = 2 * time.Second  // BLE settle time after sync before sending
)

// Start runs the device-side runtime. It blocks while stored messages are
// synced from the device into the inbox (bounded by syncWait), then returns;
// the live forwarder and keepalive loops run until ctx is cancelled.
func (s *Service) Start(ctx context.Context) {
	s.syncStored(ctx)
	s.startForwarder(ctx)
	go s.keepalive(ctx)
}

// syncStored pulls messages stored on the device while the process was down
// into the inbox, so they arrive as unread on the first read.
func (s *Service) syncStored(ctx context.Context) {
	// "No more messages" can arrive more than once (the device reports it at
	// the end of every sync cycle, and meshcore-go also re-syncs on its own),
	// so the close is guarded: a second callback must not panic.
	var once sync.Once
	syncDone := make(chan struct{})
	s.Client.OnNoMoreMessages(func() { once.Do(func() { close(syncDone) }) })
	if err := s.Client.SyncNextMessage(); err != nil {
		// The device won't answer the sync request (e.g. the transport just
		// died); proceed with the stored-message dump below.
		s.log.Error("Failed to sync stored messages", "error", err)
	} else {
		select {
		case <-syncDone:
			s.log.Info("Device sync complete")
		case <-time.After(syncWait):
			s.log.Warn("Device sync timed out, proceeding with messages received so far")
		case <-ctx.Done():
			return
		}
	}
	// Neutralize the callback: a late "no more messages" during live operation
	// is already harmless (guarded close) but has no business running.
	s.Client.OnNoMoreMessages(func() {})

	// Give the BLE device time to settle after the sync loop before any send.
	time.Sleep(postSyncSettle)

drainLoop:
	for {
		select {
		case <-s.Client.IncomingMessages():
		default:
			break drainLoop
		}
	}

	for _, m := range s.Client.GetAllMessages() {
		s.Inbox.Add(dmMessage(s.Client, m))
	}
	for _, m := range s.Client.GetAllChannelMessages() {
		s.Inbox.Add(channelMessage(s.Client, m))
	}
	s.log.Info("Stored messages queued as unread", "count", s.Inbox.Len())
}

// lostPoll is how often the forwarder checks the connection state. It only
// bounds how quickly a lost connection is noticed — the keepalive loop does
// the actual reconnecting.
const lostPoll = time.Second

// startForwarder pumps live arrivals into the inbox for as long as the
// service runs.
func (s *Service) startForwarder(ctx context.Context) {
	go forwardLoop(ctx, lostPoll, s.Client.IncomingMessages, s.Client.IsConnected,
		func(msg interface{}) {
			switch m := msg.(type) {
			case *meshcore.Message:
				s.Inbox.Add(dmMessage(s.Client, m))
			case *meshcore.ChannelMessage:
				s.Inbox.Add(channelMessage(s.Client, m))
			}
		})
}

// forwardLoop dispatches messages from the client's incoming channel until ctx
// is cancelled. The channel must be re-acquired per connection generation:
// meshcore-go publishes a fresh channel on every (re)connect, and after a
// connection loss the previous one stays open but silent (markDisconnected),
// so neither closure nor silence alone marks the end of a generation. When
// the current channel goes stale — closed, or connected() turns false — the
// loop waits for channel() to return a different channel: Connect publishes
// the new one before setting connected=true, so a fresh read after
// IsConnected()=true is always the next generation.
func forwardLoop(ctx context.Context, poll time.Duration, channel func() <-chan interface{}, connected func() bool, handle func(interface{})) {
	ticker := time.NewTicker(poll)
	defer ticker.Stop()
	for {
		ch := channel()
	readLoop:
		for {
			select {
			case msg, ok := <-ch:
				if !ok {
					break readLoop // Disconnect() closed this generation
				}
				handle(msg)
			case <-ctx.Done():
				return
			case <-ticker.C:
				if !connected() {
					break readLoop // connection lost; this generation goes silent
				}
			}
		}
		// Wait for the next generation to be published (or for shutdown; an
		// app that Disconnects without cancelling ctx just idles here).
		for channel() == ch {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
			}
		}
	}
}

// keepalive polls device stats; a failed request on a disconnected link
// (BLE, serial or TCP) triggers a reconnect.
func (s *Service) keepalive(ctx context.Context) {
	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-ticker.C:
			if err := s.Client.RequestAllStats(); err != nil {
				s.log.Error("Failed to request periodic stats", "error", err)
				if !s.Client.IsConnected() {
					s.log.Warn("Connection lost, reconnecting...")
					time.Sleep(5 * time.Second)
					if err := s.Client.Reconnect(); err != nil {
						s.log.Error("Reconnect failed", "error", err)
					} else {
						s.log.Info("Reconnected to device")
					}
				}
			}
		case <-ctx.Done():
			return
		}
	}
}

// dmMessage converts a mesh DM into the consumer shape, resolving the
// sender's contact name from the 6-byte key prefix carried by the message.
func dmMessage(client *meshcore.MeshCoreClient, m *meshcore.Message) Message {
	prefix := min(6, len(m.SenderKey))
	sender := fmt.Sprintf("?%x", m.SenderKey[:prefix])
	// An empty key must not reach the lookup: FindContactByKeyPrefix("")
	// matches every contact (an empty prefix is equal to any key prefix),
	// so the first contact would be misreported as the sender.
	if prefix > 0 {
		if c := client.FindContactByKeyPrefix(m.SenderKey[:prefix]); c != nil {
			sender = c.Name
		}
	}
	return Message{
		Type:         "dm",
		Sender:       sender,
		SenderKey:    fmt.Sprintf("%x", m.SenderKey),
		ChannelIndex: -1,
		Text:         m.Text,
		Timestamp:    m.Timestamp,
	}
}

// channelMessage converts a mesh channel message into the consumer shape.
func channelMessage(client *meshcore.MeshCoreClient, m *meshcore.ChannelMessage) Message {
	channel := ""
	if ch, ok := client.GetChannels()[m.ChannelIndex]; ok && !ch.IsEmpty {
		channel = ch.Name
	}
	return Message{
		Type:         "channel",
		Sender:       m.SenderName,
		Channel:      channel,
		ChannelIndex: m.ChannelIndex,
		Text:         m.Text,
		Timestamp:    m.Timestamp,
	}
}
