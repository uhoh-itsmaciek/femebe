package pgproto

import (
	"bytes"
	. "femebe"
	"fmt"
	"io"
	"reflect"
	"regexp"
	"strconv"
)

type ErrBadTypeCode struct {
	error
}

type ErrTooBig struct {
	error
}

type ErrWrongSize struct {
	error
}

func InitReadyForQuery(m *Message, connState ConnStatus) error {
	if connState != RFQ_IDLE &&
		connState != RFQ_INTRANS &&
		connState != RFQ_ERROR {
		return ErrBadTypeCode{
			fmt.Errorf("Invalid message type %v", connState)}
	}

	m.InitFromBytes(MSG_READY_FOR_QUERY_Z, []byte{byte(connState)})
	return nil
}

func NewField(name string, typOid uint32) *FieldDescription {
	typSize := TypSize(typOid)
	return &FieldDescription{name, 0, 0, typOid, typSize, -1, ENC_FMT_TEXT}
}

func InitRowDescription(m *Message, fields []FieldDescription) {
	// use a heuristic estimate for length to avoid having to
	// resize the msgBytes array
	fieldLenEst := (10 + 4 + 2 + 4 + 2 + 4 + 2)
	msgBytes := make([]byte, 0, len(fields)*fieldLenEst)
	buf := bytes.NewBuffer(msgBytes)
	WriteInt16(buf, int16(len(fields)))
	for _, field := range fields {
		WriteCString(buf, field.Name)
		WriteInt32(buf, field.TableOid)
		WriteInt16(buf, field.TableAttNo)
		WriteUint32(buf, field.TypeOid)
		WriteInt16(buf, field.TypLen)
		WriteInt32(buf, field.Atttypmod)
		WriteInt16(buf, int16(field.Format))
	}

	m.InitFromBytes(MSG_ROW_DESCRIPTION_T, buf.Bytes())
}

func InitDataRow(m *Message, encodedData [][]byte) {
	dataSize := 0
	for _, colVal := range encodedData {
		dataSize += len(colVal)
	}
	msgBytes := make([]byte, 0, 2 + dataSize)
	buf := bytes.NewBuffer(msgBytes)
	colCount := int16(len(encodedData))
	WriteInt16(buf, colCount)
	for _, colVal := range encodedData {
		buf.Write(colVal)
	}

	m.InitFromBytes(MSG_DATA_ROW_D, buf.Bytes())
}

func InitCommandComplete(m *Message, cmdTag string) {
	msgBytes := make([]byte, 0, len([]byte(cmdTag)))
	buf := bytes.NewBuffer(msgBytes)
	WriteCString(buf, cmdTag)

	m.InitFromBytes(MSG_COMMAND_COMPLETE_C, buf.Bytes())
}

func InitQuery(m *Message, query string) {
	msgBytes := make([]byte, 0, len([]byte(query))+1)
	buf := bytes.NewBuffer(msgBytes)
	WriteCString(buf, query)
	m.InitFromBytes(MSG_QUERY_Q, buf.Bytes())
}

type Query struct {
	Query string
}

func ReadQuery(msg *Message) (*Query, error) {
	qs, err := ReadCString(msg.Payload())
	if err != nil {
		return nil, err
	}

	return &Query{Query: qs}, err
}

type FieldDescription struct {
	Name       string
	TableOid   int32
	TableAttNo int16
	TypeOid    uint32
	TypLen     int16
	Atttypmod  int32
	Format     EncFmt
}

func encodeValue(buff *bytes.Buffer, val interface{},
	format EncFmt) (err error) {
	if format == ENC_FMT_TEXT {
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
			return fmt.Errorf("Can't encode value: %#q:%#q\n",
				reflect.TypeOf(val), val)
		}
	} else {
		return fmt.Errorf("Can't encode in format %v")
	}
	return nil
}

type RowDescription struct {
	Fields []FieldDescription
}

func ReadRowDescription(msg *Message) (
	rd *RowDescription, err error) {
	if msg.MsgType() != MSG_ROW_DESCRIPTION_T {
		return nil, ErrBadTypeCode{
			fmt.Errorf("Invalid message type %v", msg.MsgType())}
	}

	b := msg.Payload()
	fieldCount, err := ReadUint16(b)
	if err != nil {
		return nil, err
	}

	fields := make([]FieldDescription, fieldCount)
	for i, _ := range fields {
		name, err := ReadCString(b)
		if err != nil {
			return nil, err
		}
		tableOid, err := ReadInt32(b)
		if err != nil {
			return nil, err
		}
		tableAttNo, err := ReadInt16(b)
		if err != nil {
			return nil, err
		}
		typeOid, err := ReadUint32(b)
		if err != nil {
			return nil, err
		}
		typLen, err := ReadInt16(b)
		if err != nil {
			return nil, err
		}
		atttypmod, err := ReadInt32(b)
		if err != nil {
			return nil, err
		}
		format, err := ReadInt16(b)
		if err != nil {
			return nil, err
		}

		fields[i] = FieldDescription{name, tableOid, tableAttNo,
			typeOid, typLen, atttypmod, EncFmt(format)}
	}

	return &RowDescription{fields}, nil
}

type DataRow struct {
	Values [][]byte
}

func ReadDataRow(m *Message) (*DataRow, error) {
	if m.MsgType() != MSG_DATA_ROW_D {
		return nil, ErrBadTypeCode{
			fmt.Errorf("Invalid message type %v", m.MsgType())}
	}
	b := m.Payload()
	fieldCount, err := ReadUint16(b)
	if err != nil {
		return nil, err
	}

	values := make([][]byte, fieldCount)

	for i := range values {
		fieldLen, err := ReadInt32(b)
		if err != nil {
			return nil, err
		}
		if fieldLen >= 0 {
			fieldData := make([]byte, fieldLen)
			io.ReadFull(b, fieldData)
			values[i] = fieldData
		} else if fieldLen == -1 {
			values[i] = nil
		} else {
			return nil, ErrWrongSize{
				fmt.Errorf("Invalid length %v for field %v",
					fieldLen, i)}
		}
	}
	return &DataRow{values}, nil
}

type CommandComplete struct {
	Tag string
	AffectedCount uint64
	Oid uint32
}

func ReadCommandComplete(m *Message) (*CommandComplete, error) {
	if m.MsgType() != MSG_COMMAND_COMPLETE_C {
		return nil, ErrBadTypeCode{
			fmt.Errorf("Invalid message type %v", m.MsgType())}
	}

	p := m.Payload()
	fullTag, err := ReadCString(p)
	if err != nil {
		return nil, err
	}

	cmdRe := regexp.MustCompile("(INSERT|DELETE|UPDATE|SELECT|MOVE|FETCH|COPY) (\\d+)(?: (\\d+))?")
	if match := cmdRe.FindStringSubmatch(fullTag); match != nil {
		var rowcountIdx int
		var rowcount uint64
		var oid uint32

		hasOid := len(match) == 4 && match[3] != ""
		tag := match[1]

		if hasOid {
			val, err := strconv.ParseUint(match[2], 10, 32)
			if err != nil {
				panic("Oh snap")
			}
			oid = uint32(val)
			rowcountIdx = 3
		} else {
			rowcountIdx = 2
			oid = 0
		}

		rowcount, err := strconv.ParseUint(match[rowcountIdx], 10, 64)
		if err != nil {
			panic("Oh snap")
		}

		return &CommandComplete{tag, rowcount, oid}, nil
	} else {
		return &CommandComplete{fullTag, 0, 0}, nil
	}

	panic("Oh snap")
}


func InitAuthenticationOk(m *Message) {
	m.InitFromBytes(MSG_AUTHENTICATION_OK_R, []byte{0, 0, 0, 0})
}

type BackendKeyData struct {
	Pid int32
	Key int32
}

func ReadBackendKeyData(msg *Message) (*BackendKeyData, error) {
	if msg.MsgType() != MSG_BACKEND_KEY_DATA_K {
		return nil, ErrBadTypeCode{
			fmt.Errorf("Invalid message type %v", msg.MsgType())}
	}

	const RIGHT_SZ = 12
	if msg.Size() != RIGHT_SZ {
		return nil, ErrWrongSize{
			fmt.Errorf("BackendKeyData is wrong size: "+
				"expected %v, got %v", RIGHT_SZ, msg.Size())}
	}

	r := msg.Payload()
	pid, err := ReadInt32(r)
	if err != nil {
		return nil, err
	}

	key, err := ReadInt32(r)
	if err != nil {
		return nil, err
	}

	return &BackendKeyData{Pid: pid, Key: key}, err
}

// FEBE Message type constants shamelessly stolen from the pq library.
//
// All the constants in this file have a special naming convention:
// "msg(NameInManual)(characterCode)".  This results in long and
// awkward constant names, but also makes it easy to determine what
// the author's intent is quickly in code (consider that both
// msgDescribeD and msgDataRowD appear on the wire as 'D') as well as
// debugging against captured wire protocol traffic (where one will
// only see 'D', but has a sense what state the protocol is in).

type EncFmt int16

const (
	ENC_FMT_TEXT    EncFmt = 0
	ENC_FMT_BINARY         = 1
	ENC_FMT_UNKNOWN        = 0
)

// Special sub-message coding for Close and Describe
const (
	IS_PORTAL = 'P'
	IS_STMT   = 'S'
)

// Sub-message character coding that is part of ReadyForQuery
type ConnStatus byte

const (
	RFQ_IDLE    ConnStatus = 'I'
	RFQ_INTRANS            = 'T'
	RFQ_ERROR              = 'E'
)

// Message tags
const (
	MSG_AUTHENTICATION_OK_R                 byte = 'R'
	MSG_AUTHENTICATION_CLEARTEXT_PASSWORD_R      = 'R'
	MSG_AUTHENTICATION_M_D5_PASSWORD_R           = 'R'
	MSG_AUTHENTICATION_S_C_M_CREDENTIAL_R        = 'R'
	MSG_AUTHENTICATION_G_S_S_R                   = 'R'
	MSG_AUTHENTICATION_S_S_P_I_R                 = 'R'
	MSG_AUTHENTICATION_G_S_S_CONTINUE_R          = 'R'
	MSG_BACKEND_KEY_DATA_K                       = 'K'
	MSG_BIND_B                                   = 'B'
	MSG_BIND_COMPLETE2                           = '2'
	MSG_CLOSE_C                                  = 'C'
	MSG_CLOSE_COMPLETE3                          = '3'
	MSG_COMMAND_COMPLETE_C                       = 'C'
	MSG_COPY_DATAD                               = 'd'
	MSG_COPY_DONEC                               = 'c'
	MSG_COPY_FAILF                               = 'f'
	MSG_COPY_IN_RESPONSE_G                       = 'G'
	MSG_COPY_OUT_RESPONSE_H                      = 'H'
	MSG_COPY_BOTH_RESPONSE_W                     = 'W'
	MSG_DATA_ROW_D                               = 'D'
	MSG_DESCRIBE_D                               = 'D'
	MSG_EMPTY_QUERY_RESPONSE_I                   = 'I'
	MSG_ERROR_RESPONSE_E                         = 'E'
	MSG_EXECUTE_E                                = 'E'
	MSG_FLUSH_H                                  = 'H'
	MSG_FUNCTION_CALL_F                          = 'F'
	MSG_FUNCTION_CALL_RESPONSE_V                 = 'V'
	MSG_NO_DATAN                                 = 'n'
	MSG_NOTICE_RESPONSE_N                        = 'N'
	MSG_NOTIFICATION_RESPONSE_A                  = 'A'
	MSG_PARAMETER_DESCRIPTIONT                   = 't'
	MSG_PARAMETER_STATUS_S                       = 'S'
	MSG_PARSE_P                                  = 'P'
	MSG_PARSE_COMPLETE1                          = '1'
	MSG_PASSWORD_MESSAGEP                        = 'p'
	MSG_PORTAL_SUSPENDEDS                        = 's'
	MSG_QUERY_Q                                  = 'Q'
	MSG_READY_FOR_QUERY_Z                        = 'Z'
	MSG_ROW_DESCRIPTION_T                        = 'T'

	// SSLRequest is not seen here because we treat SSLRequest as
	// a protocol negotiation mechanic rather than a first-class
	// message, so it does not appear here

	MSG_SYNC_S      = 'S'
	MSG_TERMINATE_X = 'X'
)
