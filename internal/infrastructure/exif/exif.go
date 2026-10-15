// Package exif reads the capture time a camera stores in a JPEG photo.
//
// Only the tags needed to know when a photo was taken are parsed:
// DateTimeOriginal (0x9003) with its OffsetTimeOriginal (0x9011), falling back
// to the IFD0 DateTime (0x0132).
package exif

import (
	"bytes"
	"encoding/binary"
	"strings"
	"time"
)

const (
	tagDateTime           = 0x0132
	tagExifIFDPointer     = 0x8769
	tagDateTimeOriginal   = 0x9003
	tagOffsetTimeOriginal = 0x9011

	typeASCII = 2
	typeLong  = 4

	exifTimeLayout = "2006:01:02 15:04:05"
)

// CaptureTime returns when the photo was taken. A timestamp without an EXIF
// offset is interpreted in loc. ok is false when the data is not a JPEG or
// carries no usable capture time.
func CaptureTime(data []byte, loc *time.Location) (captured time.Time, ok bool) {
	tiff := findTIFF(data)
	if tiff == nil {
		return time.Time{}, false
	}
	order, ifd0, ok := tiffHeader(tiff)
	if !ok {
		return time.Time{}, false
	}
	ifd0Tags := readIFD(tiff, order, ifd0)
	dateTime, offset := "", ""
	if pointer, found := ifd0Tags[tagExifIFDPointer]; found && pointer.typ == typeLong {
		exifTags := readIFD(tiff, order, pointer.value(order))
		dateTime = exifTags[tagDateTimeOriginal].ascii(tiff, order)
		offset = exifTags[tagOffsetTimeOriginal].ascii(tiff, order)
	}
	if dateTime == "" {
		dateTime = ifd0Tags[tagDateTime].ascii(tiff, order)
	}
	if dateTime == "" {
		return time.Time{}, false
	}
	if offset != "" {
		if parsed, err := time.Parse(exifTimeLayout+"-07:00", dateTime+offset); err == nil {
			return parsed, true
		}
	}
	if loc == nil {
		loc = time.UTC
	}
	parsed, err := time.ParseInLocation(exifTimeLayout, dateTime, loc)
	if err != nil {
		return time.Time{}, false
	}
	return parsed, true
}

// findTIFF walks the JPEG markers up to the image data and returns the TIFF
// block inside the APP1 "Exif" segment.
func findTIFF(data []byte) []byte {
	if len(data) < 4 || data[0] != 0xFF || data[1] != 0xD8 {
		return nil
	}
	for pos := 2; pos+4 <= len(data); {
		if data[pos] != 0xFF {
			return nil
		}
		marker := data[pos+1]
		if marker == 0xD9 || marker == 0xDA { // end of image / start of scan
			return nil
		}
		length := int(binary.BigEndian.Uint16(data[pos+2 : pos+4]))
		if length < 2 || pos+2+length > len(data) {
			return nil
		}
		segment := data[pos+4 : pos+2+length]
		if marker == 0xE1 && bytes.HasPrefix(segment, []byte("Exif\x00\x00")) {
			return segment[6:]
		}
		pos += 2 + length
	}
	return nil
}

func tiffHeader(tiff []byte) (binary.ByteOrder, uint32, bool) {
	if len(tiff) < 8 {
		return nil, 0, false
	}
	var order binary.ByteOrder
	switch string(tiff[:2]) {
	case "II":
		order = binary.LittleEndian
	case "MM":
		order = binary.BigEndian
	default:
		return nil, 0, false
	}
	if order.Uint16(tiff[2:4]) != 42 {
		return nil, 0, false
	}
	return order, order.Uint32(tiff[4:8]), true
}

type ifdEntry struct {
	typ   uint16
	count uint32
	raw   []byte // the 4-byte value/offset field
}

func (e ifdEntry) value(order binary.ByteOrder) uint32 {
	if len(e.raw) != 4 {
		return 0
	}
	return order.Uint32(e.raw)
}

// ascii returns the entry's string value; values up to 4 bytes are inline.
func (e ifdEntry) ascii(tiff []byte, order binary.ByteOrder) string {
	if e.typ != typeASCII || e.count == 0 || e.count > 64 {
		return ""
	}
	var raw []byte
	if e.count <= 4 {
		raw = e.raw[:e.count]
	} else {
		offset := e.value(order)
		end := uint64(offset) + uint64(e.count)
		if end > uint64(len(tiff)) {
			return ""
		}
		raw = tiff[offset:end]
	}
	return strings.TrimSpace(strings.TrimRight(string(raw), "\x00"))
}

func readIFD(tiff []byte, order binary.ByteOrder, offset uint32) map[uint16]ifdEntry {
	entries := make(map[uint16]ifdEntry)
	if uint64(offset)+2 > uint64(len(tiff)) {
		return entries
	}
	count := int(order.Uint16(tiff[offset : offset+2]))
	start := int(offset) + 2
	for i := 0; i < count; i++ {
		pos := start + i*12
		if pos+12 > len(tiff) {
			break
		}
		entries[order.Uint16(tiff[pos:pos+2])] = ifdEntry{
			typ:   order.Uint16(tiff[pos+2 : pos+4]),
			count: order.Uint32(tiff[pos+4 : pos+8]),
			raw:   tiff[pos+8 : pos+12],
		}
	}
	return entries
}
