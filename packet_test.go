package hdhomerun

import (
	"bytes"
	"io"
	"reflect"
	"testing"

	"github.com/google/go-cmp/cmp"
)

var packetTests = []struct {
	name string
	p    *Packet
	b    []byte
}{
	{
		name: "empty",
		p:    &Packet{},
		b: []byte{
			0x00, 0x00,
			0x00, 0x00,
			0x1c, 0xdf, 0x44, 0x21,
		},
	},
	{
		name: "one tag",
		p: &Packet{
			Type: 1,
			Tags: []Tag{{
				Type: 2,
				Data: []byte{0xff},
			}},
		},
		b: []byte{
			0x00, 0x01,
			0x00, 0x03,
			0x02, 0x01, 0xff,
			0x97, 0xa9, 0x18, 0x73,
		},
	},
	{
		name: "two tags",
		p: &Packet{
			Type: 2,
			Tags: []Tag{
				{
					Type: 3,
					Data: []byte{0xff},
				},
				{
					Type: 4,
					Data: []byte{0xaa, 0xbb, 0xcc},
				},
			},
		},
		b: []byte{
			0x00, 0x02,
			0x00, 0x08,
			0x03, 0x01, 0xff,
			0x04, 0x03, 0xaa, 0xbb, 0xcc,
			0x5d, 0x52, 0x64, 0xf2,
		},
	},
	// TODO(mdlayher): tests with large tag values.
}

func TestPacketMarshalUnmarshalBinary(t *testing.T) {
	for _, tt := range packetTests {
		t.Run(tt.name, func(t *testing.T) {
			pb, err := tt.p.MarshalBinary()
			if err != nil {
				t.Fatalf("unexpected error marshaling packet: %v", err)
			}

			if diff := cmp.Diff(tt.b, pb); diff != "" {
				t.Fatalf("unexpected packet bytes (-want +got):\n%s", diff)
			}

			p := new(Packet)
			if err := p.UnmarshalBinary(pb); err != nil {
				t.Fatalf("unexpected error unmarshaling packet: %v", err)
			}

			if diff := cmp.Diff(tt.p, p); diff != "" {
				t.Fatalf("unexpected packet (-want +got):\n%s", diff)
			}
		})
	}
}

func TestPacketUnmarshalBinaryError(t *testing.T) {
	tests := []struct {
		name string
		b    []byte
		err  error
	}{
		{
			name: "empty",
			err:  io.ErrUnexpectedEOF,
		},
		{
			name: "short",
			b:    bytes.Repeat([]byte{0x00}, 7),
			err:  io.ErrUnexpectedEOF,
		},
		{
			name: "checksum",
			b:    bytes.Repeat([]byte{0x00}, 8),
			err:  errInvalidChecksum,
		},
		{
			name: "fuzz",
			b:    []byte("\x1dx˩\xd5D\xd5D\xf3e;\xbe\x1c\xc3F\xbe"),
			err:  io.ErrUnexpectedEOF,
		},
		{
			name: "fuzz",
			b:    []byte("11\x98\xd3\x14\x06R;Q"),
			err:  io.ErrUnexpectedEOF,
		},
		{
			name: "fuzz",
			b:    []byte("reQl\x00\x00\x01\x00V\x00\x80\a\xaf\xaep\xff\xee"),
			err:  io.ErrUnexpectedEOF,
		},
		{
			name: "fuzz",
			b:    []byte("\xa8\xd9\x00\x00\x10\x00\\f\xbfｿD\x1e\xa2\x8d"),
			err:  io.ErrUnexpectedEOF,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			p := new(Packet)
			err := p.UnmarshalBinary(tt.b)

			if want, got := tt.err, err; !reflect.DeepEqual(want, got) {
				t.Fatalf("unexpected error:\n- want: %v\n-  got: %v", want, got)
			}
		})
	}
}

func BenchmarkPacketMarshalBinary(b *testing.B) {
	for _, bb := range packetTests {
		b.Run(bb.name, func(b *testing.B) {
			b.ReportAllocs()
			for i := 0; i < b.N; i++ {
				if _, err := bb.p.MarshalBinary(); err != nil {
					b.Fatalf("failed to marshal: %v", err)
				}
			}
		})
	}
}

func BenchmarkPacketUnmarshalBinary(b *testing.B) {
	for _, bb := range packetTests {
		b.Run(bb.name, func(b *testing.B) {
			var p Packet
			b.ReportAllocs()
			for i := 0; i < b.N; i++ {
				if err := (&p).UnmarshalBinary(bb.b); err != nil {
					b.Fatalf("failed to unmarshal: %v", err)
				}
			}
		})
	}
}
