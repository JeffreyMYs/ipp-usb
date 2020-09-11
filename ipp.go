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
	"fmt"
	"io/ioutil"
	"net/http"
	"strings"

	"github.com/OpenPrinting/goipp"
)

// IppPrinterInfo represents additional printer information, which
// is not included into DNS-SD TXT record, but still needed for
// other purposes
type IppPrinterInfo struct {
	DNSSdName   string // DNS-SD device name
	UUID        string // Device UUID
	AdminURL    string // Admin URL
	IconURL     string // Device icon URL
	IppSvcIndex int    // IPP DNSSdSvcInfo index within array of services
}

// IppService performs IPP Get-Printer-Attributes query using provided
// http.Client and decodes received information into the form suitable
// for DNS-SD registration
//
// Discovered services will be added to the services collection
func IppService(log *LogMessage, services *DNSSdServices,
	port int, usbinfo UsbDeviceInfo,
	c *http.Client) (ippinfo *IppPrinterInfo, err error) {

	// Query printer attributes
	uri := fmt.Sprintf("http://localhost:%d/ipp/print", port)
	msg, err := ippGetPrinterAttributes(log, c, uri)
	if err != nil {
		return
	}

	// Decode IPP service info
	attrs := newIppDecoder(msg)
	ippinfo, ippScv := attrs.decode(usbinfo)

	// Probe for fax support
	uri = fmt.Sprintf("http://localhost:%d/ipp/faxout", port)
	if _, err2 := ippGetPrinterAttributes(log, c, uri); err2 == nil {
		log.Debug(' ', "IPP FaxOut service detected")
		ippScv.Txt.Add("Fax", "T")
		ippScv.Txt.Add("rfo", "ipp/faxout")
	} else {
		log.Debug(' ', "IPP FaxOut service not present")
		ippScv.Txt.Add("Fax", "F")
	}

	// Construct LPD info. Per Apple spec, we MUST advertise
	// LPD with zero port, even if we don't support it
	lpdScv := DNSSdSvcInfo{
		Type: "_printer._tcp",
		Port: 0,
		Txt:  nil,
	}

	// Pack it all together
	ippScv.Port = port
	services.Add(lpdScv)

	ippinfo.IppSvcIndex = len(*services)
	services.Add(ippScv)

	return
}

// ippGetPrinterAttributes performs GetPrinterAttributes query,
// using the specified http.Client and uri
//
// If this function returns nil error, it means that:
//   1) HTTP transaction performed successfully
//   2) Received reply successfully decoded
//   3) It is not an IPP error response
//
// Otherwise, the appropriate error is generated and returned
func ippGetPrinterAttributes(log *LogMessage, c *http.Client, uri string) (
	msg *goipp.Message, err error) {

	// Query printer attributes
	msg = goipp.NewRequest(goipp.DefaultVersion, goipp.OpGetPrinterAttributes, 1)
	msg.Operation.Add(goipp.MakeAttribute("attributes-charset",
		goipp.TagCharset, goipp.String("utf-8")))
	msg.Operation.Add(goipp.MakeAttribute("attributes-natural-language",
		goipp.TagLanguage, goipp.String("en-US")))
	msg.Operation.Add(goipp.MakeAttribute("printer-uri",
		goipp.TagURI, goipp.String(uri)))
	msg.Operation.Add(goipp.MakeAttribute("requested-attributes",
		goipp.TagKeyword, goipp.String("all")))

	log.Add(LogTraceIPP, '>', "IPP request:").
		IppRequest(LogTraceIPP, '>', msg).
		Nl(LogTraceIPP).
		Flush()

	req, _ := msg.EncodeBytes()
	resp, err := c.Post(uri, goipp.ContentType, bytes.NewBuffer(req))
	if err != nil {
		err = fmt.Errorf("HTTP: %s", err)
		return
	}

	defer resp.Body.Close()

	// Check HTTP status
	if resp.StatusCode/100 != 2 {
		err = fmt.Errorf("HTTP: %s", resp.Status)
		return
	}

	// Decode IPP response message
	respData, err := ioutil.ReadAll(resp.Body)
	if err != nil {
		err = fmt.Errorf("HTTP: %s", err)
		return
	}

	err = msg.DecodeBytes(respData)
	if err != nil {
		log.Debug(' ', "Failed to decode IPP message: %s", err)
		log.HexDump(LogTraceIPP, respData)
		err = fmt.Errorf("IPP decode: %s", err)
		return
	}

	log.Add(LogTraceIPP, '<', "IPP response:").
		IppResponse(LogTraceIPP, '<', msg).
		Nl(LogTraceIPP).
		Flush()

	// Check response status
	if msg.Code >= 100 {
		err = fmt.Errorf("IPP: %s", goipp.Status(msg.Code))
		return
	}

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

// Decode printer attributes and build TXT record for IPP service
//
// This is where information comes from:
//
//   DNS-SD name: "printer-dns-sd-name" with fallback to "printer-info",
//                "printer-make-and-model" and finally to MfgAndProduct
//                from the UsbDeviceInfo
//
//   TXT fields:
//     air:              hardcoded as "none"
//     mopria-certified: "mopria-certified"
//     rp:               hardcoded as "ipp/print"
//     kind:             "printer-kind"
//     PaperMax:         based on decoding "media-size-supported"
//     URF:              "urf-supported" with fallback to
//                       URF extracted from "printer-device-id"
//     UUID:             "printer-uuid", without "urn:uuid:" prefix
//     Color:            "color-supported"
//     Duplex:           search "sides-supported" for strings with
//                       prefix "one" or "two"
//     note:             "printer-location"
//     qtotal:           hardcoded as "1"
//     usb_MDL:          MDL, extracted from "printer-device-id"
//     usb_MFG:          MFG, extracted from "printer-device-id"
//     usb_CMD:          CMD, extracted from "printer-device-id"
//     ty:               "printer-make-and-model"
//     priority:         hardcoded as "50"
//     product:          "printer-make-and-model", in round brackets
//     pdl:              "document-format-supported"
//     txtvers:          hardcoded as "1"
//     adminurl:         "printer-more-info"
//
func (attrs ippAttrs) decode(usbinfo UsbDeviceInfo) (
	ippinfo *IppPrinterInfo, svc DNSSdSvcInfo) {

	svc = DNSSdSvcInfo{
		Type:     "_ipp._tcp",
		SubTypes: []string{"_universal._sub._ipp._tcp"},
	}

	// Obtain IppPrinterInfo
	ippinfo = &IppPrinterInfo{
		AdminURL: attrs.strSingle("printer-more-info"),
		IconURL:  attrs.strSingle("printer-icons"),
	}

	// Obtain DNSSdName
	ippinfo.DNSSdName = attrs.strSingle("printer-dns-sd-name")
	if ippinfo.DNSSdName == "" {
		ippinfo.DNSSdName = attrs.strSingle("printer-info")
	}
	if ippinfo.DNSSdName == "" {
		ippinfo.DNSSdName = attrs.strSingle("printer-make-and-model")
	}
	if ippinfo.DNSSdName == "" {
		ippinfo.DNSSdName = usbinfo.MfgAndProduct
	}

	// Obtain UUID
	ippinfo.UUID = attrs.getUUID()
	if ippinfo.UUID == "" {
		ippinfo.UUID = usbinfo.UUID()
	}

	// Obtain and parse IEEE 1284 device ID
	devid := make(map[string]string)
	for _, id := range strings.Split(attrs.strSingle("printer-device-id"), ";") {
		keyval := strings.SplitN(id, ":", 2)
		if len(keyval) == 2 {
			devid[keyval[0]] = keyval[1]
		}
	}

	svc.Txt.Add("air", "none")
	svc.Txt.IfNotEmpty("mopria-certified", attrs.strSingle("mopria-certified"))
	svc.Txt.Add("rp", "ipp/print")
	svc.Txt.Add("priority", "50")
	svc.Txt.IfNotEmpty("kind", attrs.strJoined("printer-kind"))
	svc.Txt.IfNotEmpty("PaperMax", attrs.getPaperMax())
	if !svc.Txt.IfNotEmpty("URF", attrs.strJoined("urf-supported")) {
		svc.Txt.IfNotEmpty("URF", devid["URF"])
	}
	svc.Txt.IfNotEmpty("UUID", ippinfo.UUID)
	svc.Txt.IfNotEmpty("Color", attrs.getBool("color-supported"))
	svc.Txt.IfNotEmpty("Duplex", attrs.getDuplex())
	svc.Txt.Add("note", attrs.strSingle("printer-location"))
	svc.Txt.Add("qtotal", "1")
	svc.Txt.IfNotEmpty("usb_MDL", devid["MDL"])
	svc.Txt.IfNotEmpty("usb_MFG", devid["MFG"])
	svc.Txt.IfNotEmpty("usb_CMD", devid["CMD"])
	svc.Txt.IfNotEmpty("ty", attrs.strSingle("printer-make-and-model"))
	svc.Txt.IfNotEmpty("product", attrs.strBrackets("printer-make-and-model"))
	svc.Txt.IfNotEmpty("pdl", attrs.strJoined("document-format-supported"))
	svc.Txt.Add("txtvers", "1")
	svc.Txt.URLIfNotEmpty("adminurl", ippinfo.AdminURL)

	return
}

// getUUID returns printer UUID, or "", if UUID not available
func (attrs ippAttrs) getUUID() string {
	uuid := attrs.strSingle("printer-uuid")
	return UUIDNormalize(uuid)
}

// getDuplex returns "T" if printer supports two-sided
// printing, "F" if not and "" if it cant' tell
func (attrs ippAttrs) getDuplex() string {
	vals := attrs.getAttr(goipp.TypeString, "sides-supported")
	one, two := false, false
	for _, v := range vals {
		s := string(v.(goipp.String))
		switch {
		case strings.HasPrefix(s, "one"):
			one = true
		case strings.HasPrefix(s, "two"):
			two = true
		}
	}

	if two {
		return "T"
	}

	if one {
		return "F"
	}

	return ""
}

// getPaperMax returns max paper size, supported by printer
//
// According to Bonjour Printing Specification, Version 1.2.1,
// it can take one of following values:
//   "<legal-A4"
//   "legal-A4"
//   "tabloid-A3"
//   "isoC-A2"
//   ">isoC-A2"
//
// If PaperMax cannot be guessed, it returns empty string
func (attrs ippAttrs) getPaperMax() string {
	// Roll over "media-size-supported", extract
	// max x-dimension and max y-dimension
	vals := attrs.getAttr(goipp.TypeCollection, "media-size-supported")
	if vals == nil {
		return ""
	}

	var x_dim_max, y_dim_max int

	for _, collection := range vals {
		var x_dim_attr, y_dim_attr goipp.Attribute
		attrs := collection.(goipp.Collection)
		for i := len(attrs) - 1; i >= 0; i-- {
			switch attrs[i].Name {
			case "x-dimension":
				x_dim_attr = attrs[i]
			case "y-dimension":
				y_dim_attr = attrs[i]
			}
		}

		if len(x_dim_attr.Values) > 0 {
			switch dim := x_dim_attr.Values[0].V.(type) {
			case goipp.Integer:
				if int(dim) > x_dim_max {
					x_dim_max = int(dim)
				}
			case goipp.Range:
				if int(dim.Upper) > x_dim_max {
					x_dim_max = int(dim.Upper)
				}
			}
		}

		if len(y_dim_attr.Values) > 0 {
			switch dim := y_dim_attr.Values[0].V.(type) {
			case goipp.Integer:
				if int(dim) > y_dim_max {
					y_dim_max = int(dim)
				}
			case goipp.Range:
				if int(dim.Upper) > y_dim_max {
					y_dim_max = int(dim.Upper)
				}
			}
		}
	}

	if x_dim_max == 0 || y_dim_max == 0 {
		return ""
	}

	// Now classify by paper size
	return PaperSize{x_dim_max, y_dim_max}.Classify()
}

// Get a single-string attribute.
func (attrs ippAttrs) strSingle(name string) string {
	strs := attrs.getStrings(name)
	if len(strs) == 0 {
		return ""
	}

	return strs[0]
}

// Get a multi-string attribute, represented as a comma-separated list
func (attrs ippAttrs) strJoined(name string) string {
	strs := attrs.getStrings(name)
	return strings.Join(strs, ",")
}

// Get a single string, and put it into brackets
func (attrs ippAttrs) strBrackets(name string) string {
	s := attrs.strSingle(name)
	if s != "" {
		s = "(" + s + ")"
	}
	return s
}

// Get attribute's []string value by attribute name
func (attrs ippAttrs) getStrings(name string) []string {
	vals := attrs.getAttr(goipp.TypeString, name)
	strs := make([]string, len(vals))
	for i := range vals {
		strs[i] = string(vals[i].(goipp.String))
	}

	return strs
}

// Get boolean attribute. Returns "F" or "T" if attribute is found,
// empty string otherwise.
func (attrs ippAttrs) getBool(name string) string {
	vals := attrs.getAttr(goipp.TypeBoolean, name)
	if vals == nil {
		return ""
	}
	if vals[0].(goipp.Boolean) {
		return "T"
	}
	return "F"
}

// Get attribute's value by attribute name
// Value type is checked and enforced
func (attrs ippAttrs) getAttr(t goipp.Type, name string) []goipp.Value {

	v, ok := attrs[name]
	if ok && v[0].V.Type() == t {
		var vals []goipp.Value
		for i := range v {
			vals = append(vals, v[i].V)
		}
		return vals
	}

	return nil
}
