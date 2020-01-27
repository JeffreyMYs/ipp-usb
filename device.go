/* ipp-usb - HTTP reverse proxy, backed by IPP-over-USB connection to device
 *
 * Copyright (C) 2020 and up by Alexander Pevzner (pzz@apevzner.com)
 * See LICENSE for license terms and conditions
 *
 * Device object brings all parts together
 */

package main

import (
	"net"
	"net/http"
)

// Device object brings all parts together, namely:
//   * HTTP proxy server
//   * USB-backed http.Transport
//   * DNS-SD advertiser
//
// There is one instance of Device object per USB device
type Device struct {
	UsbAddr        UsbAddr         // Device's USB address
	State          *DevState       // Persistent state
	HttpClient     *http.Client    // HTTP client for internal queries
	HttpProxy      *HttpProxy      // HTTP proxy
	UsbTransport   *UsbTransport   // Backing USB transport
	DnsSdPublisher *DnsSdPublisher // DNS-SD publisher
	Log            *Logger         // Device's logger
}

// NewDevice creates new Device object
func NewDevice(addr UsbAddr) (*Device, error) {
	dev := &Device{
		UsbAddr: addr,
	}

	var err error
	var info UsbDeviceInfo
	var listener net.Listener
	var dnssd_name string
	var dnssd_services DnsSdServices
	var log *LogMessage

	// Create USB transport
	dev.UsbTransport, err = NewUsbTransport(addr)
	if err != nil {
		goto ERROR
	}

	// Obtain device's logger
	info = dev.UsbTransport.UsbDeviceInfo()
	dev.Log = dev.UsbTransport.Log()

	// Load persistent state
	dev.State = LoadDevState(info.Ident())

	// Update comment
	dev.State.SetComment(info.Comment())

	// Create HTTP client for local queries
	dev.HttpClient = &http.Client{
		Transport: &HttpLoggingRoundTripper{
			Log:          dev.Log,
			RoundTripper: dev.UsbTransport,
		},
	}

	// Create net.Listener
	listener, err = dev.State.HttpListen()
	if err != nil {
		goto ERROR
	}

	// Create HTTP server
	dev.HttpProxy = NewHttpProxy(dev.Log, listener, dev.UsbTransport)

	// Obtain DNS-SD info for IPP, this is required, we are
	// IPP-USB gate, after all :-)
	log = dev.Log.Begin()
	defer log.Commit()

	dnssd_name, err = IppService(log, &dnssd_services,
		dev.State.HttpPort, info, dev.HttpClient)

	if err != nil {
		goto ERROR
	}

	// Update device state, if name changed
	if dnssd_name != dev.State.DnsSdName {
		dev.State.DnsSdName = dnssd_name
		dev.State.DnsSdOverride = dnssd_name
		dev.State.Save()
	}

	// Obtain DNS-SD info for eSCL, this is optional
	err = EsclService(log, &dnssd_services, dev.State.HttpPort, info, dev.HttpClient)
	if err != nil {
		dev.Log.Error('!', "%s", err)
	}

	// Start DNS-SD publisher
	for _, svc := range dnssd_services {
		dev.Log.Debug('>', "%s: %s TXT record:", dnssd_name, svc.Type)
		for _, txt := range svc.Txt {
			dev.Log.Debug(' ', "  %s=%s", txt.Key, txt.Value)
		}
	}

	dev.DnsSdPublisher = NewDnsSdPublisher(dev.State, dnssd_services)
	err = dev.DnsSdPublisher.Publish()
	if err != nil {
		goto ERROR
	}

	return dev, nil

ERROR:
	if dev.HttpProxy != nil {
		dev.HttpProxy.Close()
	}

	if dev.UsbTransport != nil {
		dev.UsbTransport.Close()
	}

	if listener != nil {
		listener.Close()
	}

	return nil, err
}

// Close the Device
func (dev *Device) Close() {
	dev.DnsSdPublisher.Unpublish()
	dev.HttpProxy.Close()
	dev.UsbTransport.Close()
}
