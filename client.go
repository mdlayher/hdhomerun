package hdhomerun

import (
	"bytes"
	"errors"
	"fmt"
	"net"
	"sync"
	"time"

	"github.com/mdlayher/hdhomerun/internal/libhdhomerun"
)

// A Client is an HDHomeRun client. It can be used to perform various operations
// on HDHomeRun devices. Clients are safe for concurrent use.
type Client struct {
	mu      sync.Mutex
	c       net.Conn
	b       []byte
	timeout time.Duration
}

// Dial dials a TCP connection to an HDHomeRun device.
//
// For more control over the Client, use a net.Conn with NewClient instead.
func Dial(addr string) (*Client, error) {
	conn, err := net.Dial("tcp", addr)
	if err != nil {
		return nil, err
	}

	return NewClient(conn)
}

// NewClient wraps an existing net.Conn to create a Client.
func NewClient(conn net.Conn) (*Client, error) {
	c := &Client{
		c: conn,
		b: make([]byte, libhdhomerun.MaxPacketSize),
	}

	return c, nil
}

// SetTimeout sets a per-request timeout for a combined write and read
// interaction with an HDHomeRun device. For finer control, use a
// pre-configured net.Conn with NewClient.
//
// SetTimeout will override any deadlines configured on a net.Conn used
// with NewClient.
func (c *Client) SetTimeout(d time.Duration) {
	c.mu.Lock()
	defer c.mu.Unlock()

	c.timeout = d
}

// Close closes the Client's underlying connection.
func (c *Client) Close() error {
	c.mu.Lock()
	defer c.mu.Unlock()

	return c.c.Close()
}

// Execute sends a single request to an HDHomeRun device, and reads a single
// reply from the device.
//
// Execute is a low-level method that does no request validation, and should
// be used with great caution.
//
// Most users should use the Query method instead.
func (c *Client) Execute(req *Packet) (*Packet, error) {
	// Serialize all access to the device connection, to prevent any chance of
	// overlapping requests and replies from different goroutines.
	c.mu.Lock()
	defer c.mu.Unlock()

	// When configured, only allow a certain amount of time for a write and
	// a subsequent read.
	if c.timeout != 0 {
		deadline := time.Now().Add(c.timeout)
		if err := c.c.SetDeadline(deadline); err != nil {
			return nil, err
		}
	}

	pb, err := req.MarshalBinary()
	if err != nil {
		return nil, err
	}

	if _, err := c.c.Write(pb); err != nil {
		return nil, err
	}

	n, err := c.c.Read(c.b)
	if err != nil {
		return nil, err
	}

	var rep Packet
	if err := rep.UnmarshalBinary(c.b[:n]); err != nil {
		return nil, err
	}

	return &rep, nil
}

// Query performs a read-only query to retrieve information from an HDHomeRun
// device. A list of possible query values can be found by sending "help"
// as the query parameter.
func (c *Client) Query(query string) ([]byte, error) {
	queryb := strBytes(query)

	req := &Packet{
		Type: libhdhomerun.TypeGetsetReq,
		Tags: []Tag{
			{
				Type: libhdhomerun.TagGetsetName,
				Data: queryb,
			},
		},
	}

	rep, err := c.Execute(req)
	if err != nil {
		return nil, err
	}

	if rep.Type != libhdhomerun.TypeGetsetRpy {
		return nil, fmt.Errorf("expected get/set reply, but got %#x", rep.Type)
	}

	// Expect to find both a name and value tag, and the name should be identical
	// to the query we provided in the request.
	var name, value []byte
	for _, t := range rep.Tags {
		switch t.Type {
		case libhdhomerun.TagGetsetName:
			name = t.Data
		case libhdhomerun.TagGetsetValue:
			value = t.Data
		}
	}

	if name == nil || value == nil {
		return nil, errors.New("missing query name and/or value in query reply")
	}

	if !bytes.Equal(name, queryb) {
		return nil, fmt.Errorf("unexpected query in reply packet: %v", name)
	}

	return value, nil
}

// strBytes returns a null-terminated byte slice containing the contents of s.
func strBytes(s string) []byte {
	return append([]byte(s), 0x00)
}
