/* ipp-usb - HTTP reverse proxy, backed by IPP-over-USB connection to device
 *
 * Copyright (C) 2020 and up by Alexander Pevzner (pzz@apevzner.com)
 * See LICENSE for license terms and conditions
 *
 * IPP service registration
 */

package main

import (
	"bytes"
	"io/ioutil"
	"net/http"
	"strings"

	"github.com/alexpevzner/goipp"
)

// IppService performs IPP Get-Printer-Attributes query using provided
// http.Client and decodes received information into the form suitable
// for DNS-SD registration
func IppService(c *http.Client) (dnssd_name string, info DnsSdInfo, err error) {
	uri := "http://localhost/ipp/print"

	// Query printer attributes
	msg := goipp.NewRequest(goipp.DefaultVersion, goipp.OpGetPrinterAttributes, 1)
	msg.Operation.Add(goipp.MakeAttribute("attributes-charset",
		goipp.TagCharset, goipp.String("utf-8")))
	msg.Operation.Add(goipp.MakeAttribute("attributes-natural-language",
		goipp.TagLanguage, goipp.String("en-US")))
	msg.Operation.Add(goipp.MakeAttribute("printer-uri",
		goipp.TagURI, goipp.String(uri)))
	msg.Operation.Add(goipp.MakeAttribute("requested-attributes",
		goipp.TagKeyword, goipp.String("all")))

	req, _ := msg.EncodeBytes()
	resp, err := c.Post(uri, goipp.ContentType, bytes.NewBuffer(req))
	if err != nil {
		return
	}

	// Decode IPP response message
	respData, err := ioutil.ReadAll(resp.Body)
	resp.Body.Close()
	if err != nil {
		return
	}

	err = msg.DecodeBytes(respData)
	if err != nil {
		log_debug("! IPP: %s", err)
		log_dump(respData)
		err = nil // FIXME - ignore error for now
		return
	}

	// Decode service info
	attrs := newIppDecoder(msg)
	dnssd_name, info = attrs.Decode()

	return
}

// ippAttrs represents a collection of IPP printer attributes,
// enrolled into a map for convenient access
type ippAttrs map[string]goipp.Values

// Create new ippAttrs
func newIppDecoder(msg *goipp.Message) ippAttrs {
	attrs := make(ippAttrs)

	// Note, we move from the end of list to the beginning, so
	// in a case of duplicated attributes, first occurrence wins
	for i := len(msg.Printer) - 1; i >= 0; i-- {
		attr := msg.Printer[i]
		attrs[attr.Name] = attr.Values
	}

	return attrs
}

// Decode printer attributes
func (attrs ippAttrs) Decode() (dnssd_name string, info DnsSdInfo) {
	info = DnsSdInfo{Type: "_ipp._tcp"}

	// Obtain dnssd_name
	dnssd_name = attrs.getString("printer-dns-sd-name",
		"printer-info", "printer-make-and-model")

	// Obtain and parse IEEE 1284 device ID
	devid := make(map[string]string)
	for _, id := range strings.Split(attrs.getString("printer-device-id"), ";") {
		keyval := strings.SplitN(id, ":", 2)
		if len(keyval) == 2 {
			devid[keyval[0]] = keyval[1]
		}
	}

	info.Txt.Add("note", attrs.getString("printer-location"))
	info.Txt.JoinNotEmpty("pdl", attrs.getStrings("document-format-supported"))
	info.Txt.Add("rp", "ipp/print")
	info.Txt.AddNotEmpty("URF", devid["URF"])
	info.Txt.AddNotEmpty("UUID", strings.TrimPrefix(attrs.getString("printer-uuid"), "urn:uuid:"))
	info.Txt.Add("txtvers", "1")
	info.Txt.AddNotEmpty("ty", attrs.getString("printer-make-and-model"))
	info.Txt.AddNotEmpty("Color", attrs.getBool("color-supported"))

	log_debug("> %q: %s TXT record", dnssd_name, info.Type)
	for _, txt := range info.Txt {
		log_debug("  %s=%s", txt.Key, txt.Value)
	}

	return
}

// Get attribute's string value by attribute name
// Multiple names may be specified, for fallback purposes
func (attrs ippAttrs) getString(names ...string) string {
	strings := attrs.getStrings(names...)
	if strings == nil {
		return ""
	}

	return strings[0]
}

// Get attribute's []string value by attribute name
// Multiple names may be specified, for fallback purposes
func (attrs ippAttrs) getStrings(names ...string) []string {
	vals := attrs.getAttr(goipp.TypeString, names...)
	strings := make([]string, len(vals))
	for i := range vals {
		strings[i] = string(vals[i].(goipp.String))
	}

	return strings
}

// Get boolean attribute. Returns "F" or "T" if attribute is found,
// empty string otherwise.
// Multiple names may be specified, for fallback purposes
func (attrs ippAttrs) getBool(names ...string) string {
	vals := attrs.getAttr(goipp.TypeBoolean, names...)
	if vals == nil {
		return ""
	}
	if vals[0].(goipp.Boolean) {
		return "T"
	}
	return "F"
}

// Get attribute's value by attribute name
// Multiple names may be specified, for fallback purposes
// Value type is checked and enforced
func (attrs ippAttrs) getAttr(t goipp.Type, names ...string) []goipp.Value {

	for _, name := range names {
		v, ok := attrs[name]
		if ok && v[0].V.Type() == t {
			var vals []goipp.Value
			for i := range v {
				vals = append(vals, v[i].V)
			}
			return vals
		}
	}

	return nil
}
