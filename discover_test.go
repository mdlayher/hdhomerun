package hdhomerun

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"net/url"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/fortytw2/leaktest"
	"github.com/google/go-cmp/cmp"
	"github.com/mdlayher/hdhomerun/internal/libhdhomerun"
)

var (
	// errNoReply is a sentinel value which informs testListener it should
	// not reply at all to a request.
	errNoReply = errors.New("no reply")

	// errMalformedReply is a sentinel value which informs testListener it should
	// send a malformed reply to a request.
	errMalformedReply = errors.New("malformed reply")
)

func TestParseDeviceID(t *testing.T) {
	tests := []struct {
		id string
		b  []byte
		ok bool
	}{
		{
			id: "",
		},
		{
			id: "bad",
		},
		{
			id: "nothex",
		},
		{
			id: "aaaaaa",
		},
		{
			id: "deadbeef",
			b:  []byte{0xde, 0xad, 0xbe, 0xef},
			ok: true,
		},
		{
			id: "01234567",
			b:  []byte{0x01, 0x23, 0x45, 0x67},
			ok: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.id, func(t *testing.T) {
			b, err := ParseDeviceID(tt.id)

			if err != nil && tt.ok {
				t.Fatalf("unexpected error: %v", err)
			}
			if err == nil && !tt.ok {
				t.Fatal("expected an error, but none occurred")
			}

			if !tt.ok {
				return
			}

			if diff := cmp.Diff(tt.b, b); diff != "" {
				t.Fatalf("unexpected device ID (-want +got):\n%s", diff)
			}
		})
	}
}

func TestDiscoverOneDevice(t *testing.T) {
	// Check for goroutine leaks.
	defer leaktest.Check(t)()

	// Expect a discovery packet.
	discover := &Packet{
		Type: libhdhomerun.TypeDiscoverReq,
		Tags: []Tag{
			{
				Type: libhdhomerun.TagDeviceType,
				Data: []byte{0xff, 0xff, 0xff, 0xff},
			},
			{
				Type: libhdhomerun.TagDeviceId,
				Data: []byte{0xff, 0xff, 0xff, 0xff},
			},
		},
	}

	wantDevice := &DiscoveredDevice{
		ID: "deadbeef",
		Addr: &net.UDPAddr{
			IP:   net.IPv4(127, 0, 0, 1),
			Port: 65002,
		},
		Type: DeviceTypeTuner,
		URL: &url.URL{
			Scheme: "http",
			Host:   "192.168.1.1:80",
		},
	}

	d, done := testListener(t, 1, func(req *Packet) (*Packet, error) {
		if diff := cmp.Diff(discover, req); diff != "" {
			return nil, fmt.Errorf("unexpected discover request (-want +got):\n%s", diff)
		}

		return &Packet{
			Type: libhdhomerun.TypeDiscoverRpy,
			Tags: []Tag{
				{
					Type: libhdhomerun.TagDeviceType,
					Data: []byte{0x00, 0x00, 0x00, 0x01},
				},
				{
					Type: libhdhomerun.TagDeviceId,
					Data: []byte{0xde, 0xad, 0xbe, 0xef},
				},
				{
					Type: libhdhomerun.TagBaseUrl,
					Data: []byte("http://192.168.1.1:80"),
				},
			},
		}, nil
	})
	defer done()

	ctx, cancel := context.WithTimeout(context.Background(), 1*time.Second)
	defer cancel()

	gotDevice, err := d.Discover(ctx)
	if err != nil {
		t.Fatalf("failed to discover: %v", err)
	}

	if diff := cmp.Diff(wantDevice, gotDevice); diff != "" {
		t.Fatalf("unexpected device (-want +got):\n%s", diff)
	}
}

func TestDiscoverOneHundredDevices(t *testing.T) {
	// Check for goroutine leaks.
	defer leaktest.Check(t)()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	const n = 100

	d, done := testListener(t, n, func(_ *Packet) (*Packet, error) {
		return &Packet{
			Type: libhdhomerun.TypeDiscoverRpy,
			Tags: []Tag{
				{
					Type: libhdhomerun.TagDeviceType,
					Data: []byte{0x00, 0x00, 0x00, 0x01},
				},
				{
					Type: libhdhomerun.TagDeviceId,
					Data: []byte{0xde, 0xad, 0xbe, 0xef},
				},
			},
		}, nil
	})
	defer done()

	for i := 0; i < n; i++ {
		// Context canceled in the middle of discovering devices.
		if i == n-1 {
			cancel()
		}

		if _, err := d.Discover(ctx); err != nil && err != io.EOF {
			t.Fatalf("[%02d] failed to discover: %v", i, err)
		}
	}
}

func TestDiscoverContextCanceled(t *testing.T) {
	d, done := testListener(t, 1, noReply)
	defer done()

	// Cancel before discovery even starts.
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	if _, err := d.Discover(ctx); err != io.EOF {
		t.Fatalf("failed to discover: %v", err)
	}
}

func TestDiscoverContextTimeout(t *testing.T) {
	d, done := testListener(t, 1, noReply)
	defer done()

	// Cancel after timeout expires.
	ctx, cancel := context.WithTimeout(context.Background(), 1*time.Millisecond)
	defer cancel()

	if _, err := d.Discover(ctx); err != io.EOF {
		t.Fatalf("failed to discover: %v", err)
	}
}

func TestDiscoverClosedListener(t *testing.T) {
	d, done := testListener(t, 1, noReply)
	defer done()

	// Close listener to force an error.
	_ = d.c.Close()

	// Verify that Discover does not block indefinitely due to the
	// background goroutine.
	if _, err := d.Discover(context.Background()); err == nil {
		t.Fatal("expected error due to closed listener")
	}
}

var _ context.Context = &errContext{}

type errContext struct {
	err error
}

func (*errContext) Deadline() (time.Time, bool)         { return time.Time{}, false }
func (ctx *errContext) Done() <-chan struct{}           { return nil }
func (ctx *errContext) Err() error                      { return ctx.err }
func (ctx *errContext) Value(_ interface{}) interface{} { return nil }

func TestDiscoverUnhandledContextError(t *testing.T) {
	d, done := testListener(t, 1, noReply)
	defer done()

	// Close listener to force an error, but also return an unhandled
	// context error which will take priority.
	_ = d.c.Close()
	errFoo := errors.New("foo")
	ctx := &errContext{err: errFoo}

	// Verify that Discover does not block indefinitely due to the
	// background goroutine.
	if _, err := d.Discover(ctx); err != errFoo {
		t.Fatalf("failed to discover: %v", err)
	}
}

func TestDiscoverRetryBadDevice(t *testing.T) {
	const wantID = "deadbeef"

	var i int
	d, done := testListener(t, 2, func(req *Packet) (*Packet, error) {
		// First device reply is invalid, second is valid.
		i++
		if i < 2 {
			return &Packet{
				Type: libhdhomerun.TypeDiscoverReq,
			}, nil
		}

		return &Packet{
			Type: libhdhomerun.TypeDiscoverRpy,
			Tags: []Tag{
				{
					Type: libhdhomerun.TagDeviceType,
					Data: []byte{0x00, 0x00, 0x00, 0x01},
				},
				{
					Type: libhdhomerun.TagDeviceId,
					Data: []byte{0xde, 0xad, 0xbe, 0xef},
				},
			},
		}, nil
	})
	defer done()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	device, err := d.Discover(ctx)
	if err != nil {
		t.Fatalf("failed to discover: %v", err)
	}

	if diff := cmp.Diff(wantID, device.ID); diff != "" {
		t.Fatalf("unexpected device ID (-want +got):\n%s", diff)
	}
}

func TestDiscoverBadDeviceReplies(t *testing.T) {
	tests := []struct {
		name   string
		modify func(p *Packet)
		err    error
	}{
		{
			name: "reply type",
			modify: func(p *Packet) {
				p.Type = libhdhomerun.TypeDiscoverReq
			},
		},
		{
			name: "bad device type",
			modify: func(p *Packet) {
				for i, t := range p.Tags {
					if t.Type == libhdhomerun.TagDeviceType {
						p.Tags[i].Data = []byte{}
					}
				}
			},
		},
		{
			name: "bad device ID",
			modify: func(p *Packet) {
				for i, t := range p.Tags {
					if t.Type == libhdhomerun.TagDeviceId {
						p.Tags[i].Data = []byte{}
					}
				}
			},
		},
		{
			name: "empty device type",
			modify: func(p *Packet) {
				for i, t := range p.Tags {
					if t.Type == libhdhomerun.TagDeviceType {
						p.Tags[i].Type = 0xff
					}
				}
			},
		},
		{
			name: "empty device ID",
			modify: func(p *Packet) {
				for i, t := range p.Tags {
					if t.Type == libhdhomerun.TagDeviceId {
						p.Tags[i].Type = 0xff
					}
				}
			},
		},
		{
			name: "malformed reply",
			err:  errMalformedReply,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			reply := &Packet{
				Type: libhdhomerun.TypeDiscoverRpy,
				Tags: []Tag{
					{
						Type: libhdhomerun.TagDeviceType,
						Data: []byte{0xff, 0xff, 0xff, 0xff},
					},
					{
						Type: libhdhomerun.TagDeviceId,
						Data: []byte{0xff, 0xff, 0xff, 0xff},
					},
				},
			}

			if tt.modify != nil {
				tt.modify(reply)
			}

			ctx, cancel := context.WithTimeout(context.Background(), 10*time.Millisecond)
			defer cancel()

			d, done := testListener(t, 1, func(req *Packet) (*Packet, error) {
				return reply, tt.err
			})
			defer done()

			// Use the internal discover method so we can access the retryableError.
			_, err := d.discover(ctx)
			if err == nil {
				t.Fatal("expected an error, but none occurred")
			}
			if _, ok := err.(*retryableError); !ok {
				t.Fatalf("error is not a retryableError: %v", err)
			}
		})
	}
}

// A handleFunc is a function which can be used to reply to a request
// with testListener.
type handleFunc func(req *Packet) (*Packet, error)

// noReply is a handleFunc which returns no reply to a request.
func noReply(req *Packet) (*Packet, error) {
	// Return nothing.
	return nil, errNoReply
}

// testListener creates a listener that emulates a HDHomeRun device, and
// provides a Discoverer which can discover devices from it.  Invoke the
// done closure to clean up resources.
func testListener(t *testing.T, devices int, handle handleFunc) (*Discoverer, func()) {
	const (
		localAddr = "127.0.0.1:0"
		// TODO(mdlayher): use a different address?
		multicastAddr = "224.0.0.1:65002"
	)

	multicastUDPAddr, err := net.ResolveUDPAddr("udp", multicastAddr)
	if err != nil {
		t.Fatalf("failed to resolve multicast UDP listener address: %v", err)
	}

	lo, err := net.InterfaceByName("lo")
	if err != nil {
		t.Fatalf("failed to get loopback device: %v", err)
	}

	c, err := net.ListenMulticastUDP("udp", lo, multicastUDPAddr)
	if err != nil {
		t.Fatalf("failed to open multicast listener: %v", err)
	}

	var wg sync.WaitGroup
	wg.Add(1)

	go func() {
		defer wg.Done()

		var reqp Packet
		b := make([]byte, 2048)
		for {
			n, addr, err := c.ReadFrom(b)
			if err != nil {
				if strings.Contains(err.Error(), "use of closed network connection") {
					return
				}

				panicf("failed to read: %v", err)
			}

			if err := reqp.UnmarshalBinary(b[:n]); err != nil {
				panicf("failed to unmarshal request packet from %v: %v", addr, err)
			}

			for i := 0; i < devices; i++ {
				handleRequest(c, addr, reqp, handle)
			}
		}
	}()

	// Look for any device type with any ID, but use the predefined
	// constants for the local UDP listener and UDP multicast group.
	d, err := NewDiscoverer(
		discoverLocalUDPAddr("udp", localAddr),
		discoverMulticastUDPAddr("udp", multicastAddr),
	)
	if err != nil {
		t.Fatalf("failed to start discovery: %v", err)
	}

	return d, func() {
		// Although the tests may have already closed the discovery listener
		// due to a context timeout or cancelation, we ensure the listener
		// is closed to avoid any potential file descriptor leaks.
		_ = d.c.Close()
		_ = c.Close()
		wg.Wait()
	}
}

// handleRequest handles a single request, and may issue a response to it
// if needed.
func handleRequest(c net.PacketConn, addr net.Addr, reqp Packet, handle handleFunc) {
	resp, err := handle(&reqp)
	switch err {
	case nil:
	case errNoReply:
		// Send no reply to a request.
		return
	case errMalformedReply:
		// Send a malformed reply to a request.
		if _, err := c.WriteTo([]byte{0xff}, addr); err != nil {
			panicf("failed to write malformed response to %v: %v", addr, err)
		}
		return
	default:
		panicf("error while handling request: %v", err)
	}

	pb, err := resp.MarshalBinary()
	if err != nil {
		panicf("failed to marshal response packet: %v", err)
	}

	if _, err := c.WriteTo(pb, addr); err != nil {
		panicf("failed to write response to %v: %v", addr, err)
	}
}
