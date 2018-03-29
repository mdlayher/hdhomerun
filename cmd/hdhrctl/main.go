// Command hdhrctl provides a basic HDHomeRun device querying utility.
package main

import (
	"context"
	"flag"
	"fmt"
	"io"
	"log"
	"time"

	"github.com/mdlayher/hdhomerun"
)

func main() {
	var (
		idFlag    = flag.String("i", "", "device ID to discover; wildcard by default")
		typeFlag  = flag.String("t", "", "device type to discover; wildcard by default")
		countFlag = flag.Int("n", 0, "number of devices to discover; unlimited by default")
	)

	flag.Parse()

	id := hdhomerun.DeviceIDWildcard
	if *idFlag != "" {
		id = *idFlag
	}

	typ := hdhomerun.DeviceTypeWildcard
	if t := *typeFlag; t != "" {
		switch t {
		case "tuner":
			typ = hdhomerun.DeviceTypeTuner
		case "storage":
			typ = hdhomerun.DeviceTypeStorage
		default:
			log.Fatalf("invalid device type: %q", t)
		}
	}

	devices, err := discover(*countFlag, typ, id)
	if err != nil {
		log.Fatalf("failed to discover devices: %v", err)
	}

	if len(devices) == 0 {
		log.Fatal("no devices found")
	}

	query := flag.Arg(0)
	if query == "" {
		query = "/sys/model"
	}

	for _, d := range devices {
		if err := queryDevice(d, query); err != nil {
			log.Fatal(err)
		}
	}
}

func queryDevice(d *hdhomerun.DiscoveredDevice, query string) error {
	log.Println("device:", d)

	c, err := hdhomerun.Dial(d.Addr)
	if err != nil {
		return err
	}

	b, err := c.Query(query)
	if err != nil {
		return err
	}

	fmt.Println(string(b))

	return nil
}

func discover(n int, typ hdhomerun.DeviceType, id string) ([]*hdhomerun.DiscoveredDevice, error) {
	d, err := hdhomerun.NewDiscoverer(
		hdhomerun.DiscoverDeviceType(typ),
		hdhomerun.DiscoverDeviceID(id),
	)
	if err != nil {
		return nil, fmt.Errorf("error starting discovery: %v", err)
	}

	// Discover devices for up to 2 seconds or until canceled.
	var devices []*hdhomerun.DiscoveredDevice
	ctx, cancel := context.WithTimeout(context.Background(), 1*time.Second)
	defer cancel()

	var count int
outer:
	for {
		device, err := d.Discover(ctx)
		switch err {
		case nil:
			// Found a device.
			// If only one was expected, could invoke cancel here.
			devices = append(devices, device)
			count++

			if count == n {
				return devices, nil
			}
		case io.EOF:
			// No more devices to be found.
			break outer
		default:
			return nil, fmt.Errorf("error during discovery: %v", err)
		}
	}

	return devices, nil
}
