/* ipp-usb - HTTP reverse proxy, backed by IPP-over-USB connection to device
 *
 * Copyright (C) 2020 and up by Alexander Pevzner (pzz@apevzner.com)
 * See LICENSE for license terms and conditions
 *
 * PnP manager
 */

package main

// Start PnP manager
func PnPStart() {
	devices := UsbAddrList{}
	devByAddr := make(map[string]*Device)

	for {
		newdevices := BuildUsbAddrList()
		added, removed := devices.Diff(newdevices)
		devices = newdevices

		for _, addr := range added {
			Log.Debug('+', "PNP %s: added", addr)
			dev, err := NewDevice(addr)
			if err == nil {
				devByAddr[addr.MapKey()] = dev
			} else {
				Log.Error('!', "PNP %s: %s", addr, err)
			}
		}

		for _, addr := range removed {
			Log.Debug('-', "PNP %s: removed", addr)
			dev, ok := devByAddr[addr.MapKey()]
			if ok {
				dev.Close()
				delete(devByAddr, addr.MapKey())
			}
		}

		<-UsbHotPlugChan
	}
}
