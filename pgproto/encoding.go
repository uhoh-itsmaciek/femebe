package pgproto

import (
	"bytes"
	"encoding/hex"
	"femebe"
	"fmt"
	"log"
	"strconv"
	"time"
)

func encodeValText(buf *bytes.Buffer,
	val interface{}, format string) {
	result := fmt.Sprintf(format, val)
	femebe.WriteInt32(buf, int32(len([]byte(result))))
	buf.WriteString(result)
}

func encodeValue(buff *bytes.Buffer, val interface{},
	format EncFmt) (err error) {
	if format == EncFmtTxt {
		switch val.(type) {
		case int16:
			TextEncodeInt16(buff, val.(int16))
		case int32:
			TextEncodeInt32(buff, val.(int32))
		case int64:
			TextEncodeInt64(buff, val.(int64))
		case float32:
			TextEncodeFloat32(buff, val.(float32))
		case float64:
			TextEncodeFloat64(buff, val.(float64))
		case string:
			TextEncodeString(buff, val.(string))
		case bool:
			TextEncodeBool(buff, val.(bool))
		default:
			return fmt.Errorf("Can't encode value %#v of type %#T", val, val)
		}
	} else {
		return fmt.Errorf("Can't encode in format %v")
	}
	return nil
}

func BinEncodeInt16(buff *bytes.Buffer, val int16) {
	femebe.WriteInt32(buff, 2)
	femebe.WriteInt16(buff, val)
}

func TextEncodeInt16(buff *bytes.Buffer, val int16) {
	encodeValText(buff, val, "%d")
}

func TextEncodeInt32(buff *bytes.Buffer, val int32) {
	encodeValText(buff, val, "%d")
}

func TextEncodeInt64(buff *bytes.Buffer, val int64) {
	encodeValText(buff, val, "%d")
}

func TextEncodeFloat32(buff *bytes.Buffer, val float32) {
	encodeValText(buff, val, "%e")
}

func TextEncodeFloat64(buff *bytes.Buffer, val float64) {
	encodeValText(buff, val, "%e")
}

func TextEncodeString(buff *bytes.Buffer, val string) {
	encodeValText(buff, val, "%s")
}

func TextEncodeBool(buff *bytes.Buffer, val bool) {
	encodeValText(buff, val, "%t")
}

// Decode Postgres (text) encoding into a reasonably corresponding Go
// type (lifted from pq)
func Decode(s []byte, typ Oid) interface{} {
	switch typ {
	case OidBytea:
		s = s[2:] // trim off "\\x"
		d := make([]byte, hex.DecodedLen(len(s)))
		_, err := hex.Decode(d, s)
		if err != nil {
			log.Fatalf("femebe: %s", err)
		}
		return d
	case OidTimestamp:
		return mustParse("2006-01-02 15:04:05", typ, s)
	case OidTimestamptz:
		return mustParse("2006-01-02 15:04:05-07", typ, s)
	case OidTime:
		return mustParse("15:04:05", typ, s)
	case OidTimetz:
		return mustParse("15:04:05-07", typ, s)
	case OidDate:
		return mustParse("2006-01-02", typ, s)
	case OidBool:
		return s[0] == 't'
	case OidInt8, OidInt4, OidInt2:
		i, err := strconv.ParseInt(string(s), 10, 64)
		if err != nil {
			log.Fatalf("femebe: %s", err)
		}
		return i
	case OidFloat4, OidFloat8:
		var bits int
		if typ == OidFloat4 {
			bits = 32
		} else {
			bits = 64
		}
		f, err := strconv.ParseFloat(string(s), bits)
		if err != nil {
			log.Fatalf("femebe: %s", err)
		}
		return f
	default:
		return s
	}
}

func mustParse(f string, typ Oid, s []byte) time.Time {
	str := string(s)

	// Special case until time.Parse bug is fixed:
	// http://code.google.com/p/go/issues/detail?id=3487
	if str[len(str)-2] == '.' {
		str += "0"
	}

	// check for a 30-minute-offset timezone
	if (typ == OidTimestamptz || typ == OidTimetz) &&
		str[len(str)-3] == ':' {
		f += ":00"
	}
	t, err := time.Parse(f, str)
	if err != nil {
		log.Fatalf("femebe: decode: %s", err)
	}
	return t
}

// Describe which Go type this Postgres OID will map to in the scheme
// above
func DescribeType(typ Oid) string {
	switch typ {
	case OidBytea:
		return "[]byte"
	case OidTimestamp, OidTimestamptz, OidTime, OidTimetz, OidDate:
		return "time.Time"
	case OidBool:
		return "boolean"
	case OidInt8, OidInt4, OidInt2:
		return "int64"
	case OidFloat4, OidFloat8:
		return "float64"
	default:
		return "unknown"
	}
}
