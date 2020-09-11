/* ipp-usb - HTTP reverse proxy, backed by IPP-over-USB connection to device
 *
 * Copyright (C) 2020 and up by Alexander Pevzner (pzz@apevzner.com)
 * See LICENSE for license terms and conditions
 *
 * Common errors
 */

package main

import (
	"errors"
)

var (
	ErrLockIsBusy  = errors.New("Lock is busy")
	ErrNoMemory    = errors.New("Not enough memory")
	ErrShutdown    = errors.New("Shutdown requested")
	ErrBlackListed = errors.New("Device is blacklisted")
)
