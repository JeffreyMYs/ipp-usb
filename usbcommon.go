/* ipp-usb - HTTP reverse proxy, backed by IPP-over-USB connection to device
 *
 * Copyright (C) 2020 and up by Alexander Pevzner (pzz@apevzner.com)
 * See LICENSE for license terms and conditions
 *
 * Common types for USB
 */

package main

import (
	"fmt"
	"sort"
	"strings"
)

// UsbAddr represents an USB device address
type UsbAddr struct {
	Bus     int // The bus on which the device was detected
	Address int // The address of the device on the bus
}

// String returns a human-readable representation of UsbAddr
func (addr UsbAddr) String() string {
	return fmt.Sprintf("Bus %.3d Device %.3d", addr.Bus, addr.Address)
}

// MapKey() returns a string suitable as a map key
// It is not even guaranteed that this string is printable
func (addr UsbAddr) MapKey() string {
	return fmt.Sprintf("%d:%d", addr.Bus, addr.Address)
}

// Compare 2 addresses, for sorting
func (addr UsbAddr) Less(addr2 UsbAddr) bool {
	return addr.Bus < addr2.Bus ||
		(addr.Bus == addr2.Bus && addr.Address < addr2.Address)
}

// UsbAddrList represents a list of USB addresses
//
// For faster lookup and comparable logging, address list
// is always sorted in acceding order. To maintain this
// invariant, never modify list directly, and use the provided
// (*UsbAddrList) Add() function
type UsbAddrList []UsbAddr

// Add UsbAddr to UsbAddrList
func (list *UsbAddrList) Add(addr UsbAddr) {
	// Find the smallest index of address list
	// item which is greater or equal to the
	// newly inserted address
	//
	// Note, of "not found" case sort.Search()
	// returns len(*list)
	i := sort.Search(len(*list), func(n int) bool {
		return !(*list)[n].Less(addr)
	})

	// Check for duplicate
	if i < len(*list) && (*list)[i] == addr {
		return
	}

	// The simple case: all items are less
	// that newly added, so just append new
	// address to the end
	if i == len(*list) {
		*list = append(*list, addr)
		return
	}

	// Insert item in the middle
	*list = append(*list, (*list)[i])
	(*list)[i] = addr
}

// Find address in a list. Returns address index,
// if address is found, -1 otherwise
func (list UsbAddrList) Find(addr UsbAddr) int {
	i := sort.Search(len(list), func(n int) bool {
		return !list[n].Less(addr)
	})

	if i < len(list) && list[i] == addr {
		return i
	}

	return -1
}

// Diff computes a difference between two address lists,
// returning lists of elements to be added and to be removed
// to/from the list1 to convert it to the list2
func (list1 UsbAddrList) Diff(list2 UsbAddrList) (added, removed UsbAddrList) {
	// Note, there is no needs to sort added and removed
	// lists, they are already created sorted

	for _, a := range list2 {
		if list1.Find(a) < 0 {
			added.Add(a)
		}
	}

	for _, a := range list1 {
		if list2.Find(a) < 0 {
			removed.Add(a)
		}
	}

	return
}

// UsbIfAddr represents a full "address" of the USB interface
type UsbIfAddr struct {
	UsbAddr     // Device address
	Num     int // Interface number within Config
	Alt     int // Number of alternate setting
	In, Out int // Input/output endpoint numbers
}

// String returns a human readable short representation of UsbIfAddr
func (ifaddr UsbIfAddr) String() string {
	return fmt.Sprintf("Bus %.3d Device %.3d Interface %d Alt %d",
		ifaddr.Bus,
		ifaddr.Address,
		ifaddr.Num,
		ifaddr.Alt,
	)
}

// UsbIfAddrList represents a list of USB interface addresses
type UsbIfAddrList []UsbIfAddr

// Add UsbIfAddr to UsbIfAddrList
func (list *UsbIfAddrList) Add(addr UsbIfAddr) {
	*list = append(*list, addr)
}

// UsbDeviceDesc represents an IPP-over-USB device descriptor
type UsbDeviceDesc struct {
	UsbAddr               // Device address
	Config  int           // IPP-over-USB configuration
	IfAddrs UsbIfAddrList // IPP-over-USB interfaces
}

// Type UsbDeviceInfo represents USB device information
type UsbDeviceInfo struct {
	Vendor       uint16
	Product      uint16
	SerialNumber string
	Manufacturer string
	ProductName  string
}

// Ident returns device identification string, suitable as
// persistent state identifier
func (info UsbDeviceInfo) Ident() string {
	id := fmt.Sprintf("%4.4x-%s-%s", info.Vendor, info.SerialNumber, info.ProductName)
	id = strings.Map(func(c rune) rune {
		switch {
		case '0' <= c && c <= '9':
		case 'a' <= c && c <= 'z':
		case 'A' <= c && c <= 'Z':
		case c == '-' || c == '_':
		default:
			c = '-'
		}
		return c
	}, id)
	return id
}

// Comment returns a short comment, describing a device
func (info UsbDeviceInfo) Comment() string {
	c := ""

	if !strings.HasPrefix(info.ProductName, info.Manufacturer) {
		c += info.Manufacturer + " " + info.ProductName
	} else {
		c = info.ProductName
	}

	c += " serial=" + info.SerialNumber

	return c
}
