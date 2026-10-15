package exif

import (
	"encoding/binary"
	"testing"
	"time"
)

// buildJPEG returns a minimal JPEG whose APP1 segment holds an EXIF block with
// DateTimeOriginal and, when offset is non-empty, OffsetTimeOriginal.
func buildJPEG(order binary.ByteOrder, dateTime, offset string) []byte {
	entries := 1
	if offset != "" {
		entries = 2
	}
	const ifd0 = 8
	exifIFD := ifd0 + 2 + 12 + 4
	dataStart := exifIFD + 2 + entries*12 + 4

	tiff := make([]byte, dataStart)
	if order == binary.LittleEndian {
		copy(tiff, "II")
	} else {
		copy(tiff, "MM")
	}
	order.PutUint16(tiff[2:], 42)
	order.PutUint32(tiff[4:], ifd0)

	putEntry := func(pos int, tag, typ uint16, count, value uint32) {
		order.PutUint16(tiff[pos:], tag)
		order.PutUint16(tiff[pos+2:], typ)
		order.PutUint32(tiff[pos+4:], count)
		order.PutUint32(tiff[pos+8:], value)
	}
	order.PutUint16(tiff[ifd0:], 1)
	putEntry(ifd0+2, tagExifIFDPointer, typeLong, 1, uint32(exifIFD))

	order.PutUint16(tiff[exifIFD:], uint16(entries))
	dateValue := []byte(dateTime + "\x00")
	putEntry(exifIFD+2, tagDateTimeOriginal, typeASCII, uint32(len(dateValue)), uint32(dataStart))
	tiff = append(tiff, dateValue...)
	if offset != "" {
		offsetValue := []byte(offset + "\x00")
		putEntry(exifIFD+14, tagOffsetTimeOriginal, typeASCII, uint32(len(offsetValue)), uint32(len(tiff)))
		tiff = append(tiff, offsetValue...)
	}

	app1 := append([]byte("Exif\x00\x00"), tiff...)
	jpeg := []byte{0xFF, 0xD8, 0xFF, 0xE1, 0, 0}
	binary.BigEndian.PutUint16(jpeg[4:], uint16(len(app1)+2))
	jpeg = append(jpeg, app1...)
	return append(jpeg, 0xFF, 0xDA, 0, 2, 0xFF, 0xD9)
}

func TestCaptureTime(t *testing.T) {
	jakarta := time.FixedZone("WIB", 7*3600)
	tests := []struct {
		name   string
		data   []byte
		want   time.Time
		wantOK bool
	}{
		{
			name:   "little endian without offset uses location",
			data:   buildJPEG(binary.LittleEndian, "2026:10:14 09:40:15", ""),
			want:   time.Date(2026, 10, 14, 9, 40, 15, 0, jakarta),
			wantOK: true,
		},
		{
			name:   "big endian with offset ignores location",
			data:   buildJPEG(binary.BigEndian, "2026:10:14 09:40:15", "+08:00"),
			want:   time.Date(2026, 10, 14, 9, 40, 15, 0, time.FixedZone("", 8*3600)),
			wantOK: true,
		},
		{name: "not a jpeg", data: []byte("\x89PNG\r\n\x1a\n"), wantOK: false},
		{name: "jpeg without exif", data: []byte{0xFF, 0xD8, 0xFF, 0xDA, 0, 2, 0xFF, 0xD9}, wantOK: false},
		{name: "malformed date", data: buildJPEG(binary.LittleEndian, "kemarin pagi", ""), wantOK: false},
		{name: "truncated", data: buildJPEG(binary.LittleEndian, "2026:10:14 09:40:15", "")[:20], wantOK: false},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got, ok := CaptureTime(test.data, jakarta)
			if ok != test.wantOK {
				t.Fatalf("ok = %v, want %v", ok, test.wantOK)
			}
			if ok && !got.Equal(test.want) {
				t.Fatalf("time = %v, want %v", got, test.want)
			}
		})
	}
}
