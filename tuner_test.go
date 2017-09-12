package hdhomerun

import (
	"testing"

	"github.com/google/go-cmp/cmp"
	"github.com/mdlayher/hdhomerun/internal/libhdhomerun"
)

func TestClientTunerDebug(t *testing.T) {
	tests := []struct {
		name  string
		s     string
		debug *TunerDebug
		ok    bool
	}{
		{
			name: "bad status",
			s:    `tun:`,
		},
		{
			name: "bad key=value",
			s:    `tun: ch`,
		},
		{
			name: "bad integer",
			s:    `tun: ss=foo`,
		},
		{
			name:  "unhandled key",
			s:     `foo: bar=0`,
			debug: &TunerDebug{},
			ok:    true,
		},
		{
			name: "not tuned",
			s: `tun: ch=none lock=none ss=0 snq=0 seq=0 dbg=0
			dev: bps=1 resync=2 overflow=3
			cc:  bps=4 resync=5 overflow=6
			ts:  bps=7 te=8 crc=9
			net: pps=10 err=11 stop=0
			`,
			debug: &TunerDebug{
				Tuner: &TunerStatus{
					Channel: "none",
					Lock:    "none",
					Debug:   "0",
				},
				Device: &DeviceStatus{
					BitsPerSecond: 1,
					Resync:        2,
					Overflow:      3,
				},
				CableCARD: &CableCARDStatus{
					BitsPerSecond: 4,
					Resync:        5,
					Overflow:      6,
				},
				TransportStream: &TransportStreamStatus{
					BitsPerSecond:   7,
					TransportErrors: 8,
					CRCErrors:       9,
				},
				Network: &NetworkStatus{
					PacketsPerSecond: 10,
					Errors:           11,
					Stop:             StopReasonNotStopped,
				},
			},
			ok: true,
		},
		{
			name: "tuned",
			s:    `tun: ch=qam:249000000 lock=qam256:249000000 ss=100 snq=100 seq=100 dbg=-383/-6666`,
			debug: &TunerDebug{
				Tuner: &TunerStatus{
					Channel:              "qam:249000000",
					Lock:                 "qam256:249000000",
					SignalStrength:       100,
					SignalToNoiseQuality: 100,
					SymbolErrorQuality:   100,
					Debug:                "-383/-6666",
				},
			},
			ok: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			const query = "/tuner0/debug"

			reply := &Packet{
				Type: libhdhomerun.TypeGetsetRpy,
				Tags: []Tag{
					{
						Type: libhdhomerun.TagGetsetName,
						Data: strBytes(query),
					},
					{
						Type: libhdhomerun.TagGetsetValue,
						Data: strBytes(tt.s),
					},
				},
			}

			c, done := testClient(t, func(req *Packet) (*Packet, error) {
				return reply, nil
			})
			defer done()

			got, err := c.Tuner(0).Debug()

			if tt.ok && err != nil {
				t.Fatalf("unexpected error during query: %v", err)
			}
			if !tt.ok && err == nil {
				t.Fatal("expected an error, but none occurred")
			}
			if !tt.ok {
				return
			}

			if diff := cmp.Diff(tt.debug, got); diff != "" {
				t.Fatalf("unexpected query reply value (-want +got):\n%s", diff)
			}
		})
	}
}
