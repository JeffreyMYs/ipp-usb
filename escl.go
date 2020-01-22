/* ipp-usb - HTTP reverse proxy, backed by IPP-over-USB connection to device
 *
 * Copyright (C) 2020 and up by Alexander Pevzner (pzz@apevzner.com)
 * See LICENSE for license terms and conditions
 *
 * ESCL service registration
 */

package main

import (
	"bytes"
	"encoding/xml"
	"errors"
	"fmt"
	"io"
	"io/ioutil"
	"net/http"
	"sort"
	"strings"
)

// EsclService queries eSCL ScannerCapabilities using provided
// http.Client and decodes received information into the form
// suitable for DNS-SD registration
func EsclService(port int, usbinfo UsbDeviceInfo, c *http.Client) (
	infos []DnsSdInfo, err error) {

	uri := "http://localhost/eSCL/ScannerCapabilities"
	decoder := newEsclCapsDecoder()
	info := DnsSdInfo{
		Type: "_uscan._tcp",
		Port: port,
	}

	var xmlData []byte
	var list []string

	// Query ScannerCapabilities
	resp, err := c.Get(uri)
	if err != nil {
		goto ERROR
	}

	if resp.StatusCode/100 != 2 {
		resp.Body.Close()
		err = fmt.Errorf("HTTP status: %s", resp.Status)
		goto ERROR
	}

	xmlData, err = ioutil.ReadAll(resp.Body)
	resp.Body.Close()
	if err != nil {
		goto ERROR
	}

	// Decode the XML
	err = decoder.decode(bytes.NewBuffer(xmlData))
	if err != nil {
		goto ERROR
	}

	// If we have no data, assume eSCL response was invalud
	if decoder.uuid == "" || decoder.version == "" ||
		len(decoder.cs) == 0 || len(decoder.pdl) == 0 ||
		!(decoder.platen && decoder.adf) {
		err = errors.New("invalid response")
	}

	// Build eSCL DnsSdInfo

	if decoder.duplex {
		info.Txt.Add("duplex", "T")
	} else {
		info.Txt.Add("duplex", "F")
	}

	switch {
	case decoder.platen && !decoder.adf:
		info.Txt.Add("is", "platen")
	case !decoder.platen && decoder.adf:
		info.Txt.Add("is", "adf")
	case decoder.platen && decoder.adf:
		info.Txt.Add("is", "platen,adf")
	}

	list = []string{}
	for c := range decoder.cs {
		list = append(list, c)
	}
	sort.Strings(list)
	info.Txt.IfNotEmpty("cs", strings.Join(list, ","))

	info.Txt.IfNotEmpty("UUID", decoder.uuid)

	list = []string{}
	for p := range decoder.pdl {
		list = append(list, p)
	}
	sort.Strings(list)
	info.Txt.IfNotEmpty("pdl", strings.Join(list, ","))

	info.Txt.Add("ty", usbinfo.Product)
	info.Txt.Add("rs", "eSCL")
	info.Txt.IfNotEmpty("vers", decoder.version)
	info.Txt.IfNotEmpty("txtvers", "1")

	// Pack the reply
	infos = []DnsSdInfo{info}

	return

	// Handle a error
ERROR:
	err = fmt.Errorf("eSCL: %s", err)
	return
}

// esclCapsDecoder represents eSCL ScannerCapabilities decoder
type esclCapsDecoder struct {
	uuid        string
	version     string
	platen, adf bool
	duplex      bool
	pdl, cs     map[string]struct{}
}

// newesclCapsDecoder creates new esclCapsDecoder
func newEsclCapsDecoder() *esclCapsDecoder {
	return &esclCapsDecoder{
		pdl: make(map[string]struct{}),
		cs:  make(map[string]struct{}),
	}
}

// Decode scanner capabilities
func (decoder *esclCapsDecoder) decode(in io.Reader) error {
	xmlDecoder := xml.NewDecoder(in)

	var path bytes.Buffer
	var lenStack []int

	for {
		token, err := xmlDecoder.RawToken()
		if err != nil {
			break
		}

		switch t := token.(type) {
		case xml.StartElement:
			lenStack = append(lenStack, path.Len())
			path.WriteByte('/')
			path.WriteString(t.Name.Space)
			path.WriteByte(':')
			path.WriteString(t.Name.Local)
			decoder.element(path.String())

		case xml.EndElement:
			last := len(lenStack) - 1
			path.Truncate(lenStack[last])
			lenStack = lenStack[:last]

		case xml.CharData:
			data := bytes.TrimSpace(t)
			if len(data) > 0 {
				decoder.data(path.String(), string(data))
			}
		}
	}

	return nil
}

const (
	// Relative to root
	esclPlaten          = "/scan:ScannerCapabilities/scan:Platen"
	esclAdf             = "/scan:ScannerCapabilities/scan:Adf"
	esclPlatenInputCaps = esclPlaten + "/scan:PlatenInputCaps"
	esclAdfSimplexCaps  = esclAdf + "/scan:AdfSimplexInputCaps"
	esclAdfDuplexCaps   = esclAdf + "/scan:AdfDuplexCaps"

	// Relative to esclPlatenInputCaps, esclAdfSimplexCaps or esclAdfDuplexCaps
	esclSettingProfile = "/scan:SettingProfiles/scan:SettingProfile"
	esclColorMode      = esclSettingProfile + "/scan:ColorModes/scan:ColorMode"
	esclDocumentFormat = esclSettingProfile + "/scan:DocumentFormats/pwg:DocumentFormat"
)

// handle beginning of XML element
func (decoder *esclCapsDecoder) element(path string) {
	switch path {
	case esclPlaten:
		decoder.platen = true
	case esclAdf:
		decoder.adf = true
	}
}

// handle XML element data
func (decoder *esclCapsDecoder) data(path, data string) {
	switch path {
	case "/scan:ScannerCapabilities/scan:UUID":
		decoder.uuid = data
	case "/scan:ScannerCapabilities/pwg:Version":
		decoder.version = data

	case esclPlatenInputCaps + esclColorMode,
		esclAdfSimplexCaps + esclColorMode,
		esclAdfDuplexCaps + esclColorMode:

		data = strings.ToLower(data)
		switch {
		case strings.HasPrefix(data, "rgb"):
			decoder.cs["color"] = struct{}{}
		case strings.HasPrefix(data, "grayscale"):
			decoder.cs["grayscale"] = struct{}{}
		case strings.HasPrefix(data, "blackandwhite"):
			decoder.cs["binary"] = struct{}{}
		}

	case esclPlatenInputCaps + esclDocumentFormat,
		esclAdfSimplexCaps + esclDocumentFormat,
		esclAdfDuplexCaps + esclDocumentFormat:

		decoder.pdl[data] = struct{}{}
	}
}
