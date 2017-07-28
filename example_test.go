package hdhomerun_test

import (
	"context"
	"io"
	"log"
	"time"

	"github.com/mdlayher/hdhomerun"
)

// This example demonstrates the use of a Discoverer to discover devices until
// a timeout elapses.
func ExampleDiscoverer_discover() {
	// Discover devices of any type with any ID.
	d, err := hdhomerun.NewDiscoverer()
	if err != nil {
		log.Fatalf("error starting discovery: %v", err)
	}

	// Discover devices for up to 2 seconds or until canceled.
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	for {
		// Note: Discover blocks until a device is found or its context is
		// canceled.  Always pass a context with a cancel function, deadline,
		// or timeout.
		//
		// io.EOF is returned when the context is canceled.
		device, err := d.Discover(ctx)
		switch err {
		case nil:
			// Found a device.
			// If only one device is expected, invoke cancel here.
			log.Println("device:", device)
		case io.EOF:
			// Context canceled; no more devices to be found.
			return
		default:
			log.Fatalf("error during discovery: %v", err)
		}
	}
}
