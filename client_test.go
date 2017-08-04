package hdhomerun

import (
	"fmt"
	"io"
	"net"
	"sync"
	"testing"
	"time"

	"github.com/google/go-cmp/cmp"
	"github.com/mdlayher/hdhomerun/internal/libhdhomerun"
)

func TestClientQuery(t *testing.T) {
	const (
		query = "/test"
		value = "test"
	)

	var (
		queryb = strBytes(query)
		valueb = strBytes(value)
	)

	// Expect a get/set request packet.
	getSet := &Packet{
		Type: libhdhomerun.TypeGetsetReq,
		Tags: []Tag{
			{
				Type: libhdhomerun.TagGetsetName,
				Data: queryb,
			},
		},
	}

	reply := &Packet{
		Type: libhdhomerun.TypeGetsetRpy,
		Tags: []Tag{
			{
				Type: libhdhomerun.TagGetsetName,
				Data: queryb,
			},
			{
				Type: libhdhomerun.TagGetsetValue,
				Data: valueb,
			},
		},
	}

	c, done := testClient(t, func(req *Packet) (*Packet, error) {
		if diff := cmp.Diff(getSet, req); diff != "" {
			return nil, fmt.Errorf("unexpected query request (-want +got):\n%s", diff)
		}

		return reply, nil
	})
	defer done()

	got, err := c.Query(query)
	if err != nil {
		t.Fatalf("failed to query: %v", err)
	}

	if diff := cmp.Diff(valueb, got); diff != "" {
		t.Fatalf("unexpected query reply value (-want +got):\n%s", diff)
	}
}

func TestClientSetTimeout(t *testing.T) {
	c, done := testClient(t, noReply)
	defer done()

	// Fail as fast as possible, though the emulated device will not reply anyway.
	c.SetTimeout(1 * time.Nanosecond)

	_, err := c.Query("help")
	if nerr, ok := err.(net.Error); !ok || !nerr.Timeout() {
		t.Fatalf("expected timeout error, but got: %v", err)
	}
}

func TestClientQueryBadReplies(t *testing.T) {
	tests := []struct {
		name   string
		modify func(p *Packet)
	}{
		{
			name: "reply type",
			modify: func(p *Packet) {
				p.Type = libhdhomerun.TypeGetsetReq
			},
		},
		{
			name: "bad getset name",
			modify: func(p *Packet) {
				for i, t := range p.Tags {
					if t.Type == libhdhomerun.TagGetsetName {
						p.Tags[i].Data = []byte{}
					}
				}
			},
		},
		{
			name: "empty getset name",
			modify: func(p *Packet) {
				for i, t := range p.Tags {
					if t.Type == libhdhomerun.TagGetsetName {
						p.Tags[i].Type = 0xff
					}
				}
			},
		},
		{
			name: "empty getset value",
			modify: func(p *Packet) {
				for i, t := range p.Tags {
					if t.Type == libhdhomerun.TagGetsetValue {
						p.Tags[i].Type = 0xff
					}
				}
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			const query = "/test"

			reply := &Packet{
				Type: libhdhomerun.TypeGetsetRpy,
				Tags: []Tag{
					{
						Type: libhdhomerun.TagGetsetName,
						Data: strBytes(query),
					},
					{
						Type: libhdhomerun.TagGetsetValue,
						Data: strBytes("test"),
					},
				},
			}

			if tt.modify != nil {
				tt.modify(reply)
			}

			c, done := testClient(t, func(req *Packet) (*Packet, error) {
				return reply, nil
			})
			defer done()

			if _, err := c.Query(query); err == nil {
				t.Fatal("expected an error, but none occurred")
			}
		})
	}
}

// testClient creates a listener that emulates an HDHomeRun device, and
// provides a Client which is configured to query it. Invoke the done closure
// to clean up resources.
func testClient(t *testing.T, handle handleFunc) (*Client, func()) {
	l, err := net.Listen("tcp", "localhost:0")
	if err != nil {
		t.Fatalf("failed to start TCP listener: %v", err)
	}

	var wg sync.WaitGroup
	wg.Add(1)

	go func() {
		defer wg.Done()

		// Accept a single connection and immediately close the listener to
		// prevent any additional connections.
		c, err := l.Accept()
		if err != nil {
			panicf("failed to accept: %v", err)
		}
		_ = l.Close()
		defer c.Close()

		b := make([]byte, libhdhomerun.MaxPacketSize)
		for {
			n, err := c.Read(b)
			if err != nil {
				if err == io.EOF {
					return
				}

				panicf("failed to read request: %v", err)
			}

			var req Packet
			if err := req.UnmarshalBinary(b[:n]); err != nil {
				panicf("failed to unmarshal request: %v", err)
			}

			res, err := handle(&req)
			if err != nil {
				panicf("error while handling request: %v", err)
			}

			pb, err := res.MarshalBinary()
			if err != nil {
				panicf("failed to marshal response: %v", err)
			}

			if _, err := c.Write(pb); err != nil {
				panicf("failed to write response: %v", err)
			}
		}
	}()

	c, err := Dial(l.Addr().String())
	if err != nil {
		t.Fatalf("failed to dial device: %v", err)
	}

	return c, func() {
		_ = l.Close()
		_ = c.Close()
		wg.Wait()
	}
}
