package httpcache

import (
	"encoding/binary"
	"errors"
	"net/http"
	"slices"
	"time"
)

const (
	// passVersion marks a response that must not be shared with
	// de-duplicated followers (see passMarker).
	passVersion byte = 0
	// entryVersion is the current binary format version of stored entries.
	entryVersion byte = 1
	// fixedEntryOverhead is the upper bound of the fixed-size part of an entry:
	// version byte, status, creation time and header count.
	fixedEntryOverhead = 1 + 3*binary.MaxVarintLen64
	// headerLengthPrefixes is the number of length prefixes per header besides
	// one per value: the name length and the value count.
	headerLengthPrefixes = 2
)

var (
	// ErrCorruptEntry is returned when a cached entry cannot be decoded.
	ErrCorruptEntry = errors.New("httpcache: corrupt cache entry")
	// ErrUnsupportedVersion is returned when a cached entry has an unknown format version.
	ErrUnsupportedVersion = errors.New("httpcache: unsupported cache entry version")
)

// passMarker is returned by the generator instead of an encoded entry when the
// response is private to the leader request. Followers receiving it run the
// handler themselves.
var passMarker = []byte{passVersion}

// isPassMarker reports whether data is the pass marker.
func isPassMarker(data []byte) bool {
	return len(data) == 1 && data[0] == passVersion
}

// entry is a decoded cached HTTP response.
type entry struct {
	createdAt time.Time
	header    http.Header
	body      []byte
	status    int
}

// encodeEntry serializes a response into the versioned binary entry format:
//
//	[0]      version
//	uvarint  status
//	varint   creation time (unix seconds)
//	uvarint  number of headers, then for each header:
//	         uvarint len + name, uvarint number of values, uvarint len + value...
//	rest     body
//
// Headers that must not be stored (see storableHeaders) are dropped.
func encodeEntry(resp *response) []byte {
	header := storableHeaders(resp.header)

	keys := make([]string, 0, len(header))
	size := fixedEntryOverhead + len(resp.body)

	for name, values := range header {
		keys = append(keys, name)
		size += binary.MaxVarintLen64*(headerLengthPrefixes+len(values)) + len(name)

		for _, v := range values {
			size += len(v)
		}
	}

	slices.Sort(keys)

	buf := make([]byte, 0, size)
	buf = append(buf, entryVersion)
	buf = binary.AppendUvarint(buf, uint64(resp.status)) //nolint:gosec // status is validated by the recorder
	buf = binary.AppendVarint(buf, resp.createdAt.Unix())
	buf = binary.AppendUvarint(buf, uint64(len(keys)))

	for _, name := range keys {
		values := header[name]

		buf = appendString(buf, name)
		buf = binary.AppendUvarint(buf, uint64(len(values)))

		for _, v := range values {
			buf = appendString(buf, v)
		}
	}

	return append(buf, resp.body...)
}

// appendString appends a length-prefixed string to buf.
func appendString(buf []byte, s string) []byte {
	buf = binary.AppendUvarint(buf, uint64(len(s)))
	return append(buf, s...)
}

// decodeEntry parses data produced by encodeEntry.
// The returned body aliases data and must not be modified.
// It returns ErrUnsupportedVersion for unknown versions and ErrCorruptEntry for malformed data.
func decodeEntry(data []byte) (*entry, error) {
	if len(data) == 0 {
		return nil, ErrCorruptEntry
	}

	if data[0] != entryVersion {
		return nil, ErrUnsupportedVersion
	}

	d := decoder{data: data[1:]}

	status := d.uvarint()
	createdAt := d.varint()
	numHeaders := d.count()

	header := make(http.Header, numHeaders)

	for range numHeaders {
		name := d.str()
		numValues := d.count()

		values := make([]string, 0, numValues)
		for range numValues {
			values = append(values, d.str())
		}

		header[name] = values
	}

	if d.err != nil {
		return nil, d.err
	}

	if status < minStatusCode || status > maxStatusCode {
		return nil, ErrCorruptEntry
	}

	return &entry{
		status:    int(status),
		createdAt: time.Unix(createdAt, 0),
		header:    header,
		body:      d.data,
	}, nil
}

// decoder reads length-prefixed values from a byte slice and remembers the first error.
type decoder struct {
	err  error
	data []byte
}

func (d *decoder) uvarint() uint64 {
	if d.err != nil {
		return 0
	}

	v, n := binary.Uvarint(d.data)
	if n <= 0 {
		d.err = ErrCorruptEntry
		return 0
	}

	d.data = d.data[n:]

	return v
}

func (d *decoder) varint() int64 {
	if d.err != nil {
		return 0
	}

	v, n := binary.Varint(d.data)
	if n <= 0 {
		d.err = ErrCorruptEntry
		return 0
	}

	d.data = d.data[n:]

	return v
}

// count reads a collection length; it can never exceed the remaining bytes,
// which bounds allocations on corrupt input.
func (d *decoder) count() int {
	n := d.uvarint()
	if n > uint64(len(d.data)) {
		d.err = ErrCorruptEntry
		return 0
	}

	return int(n) //nolint:gosec // bounded by len(d.data) above
}

func (d *decoder) str() string {
	n := d.count()
	if d.err != nil {
		return ""
	}

	s := string(d.data[:n])
	d.data = d.data[n:]

	return s
}
