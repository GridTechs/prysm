package encoder

import (
	"bytes"
	"fmt"
	"io"

	"github.com/gogo/protobuf/proto"
	"github.com/golang/snappy"
	"github.com/prysmaticlabs/go-ssz"
	"github.com/sirupsen/logrus"
)

var _ = NetworkEncoding(&SszNetworkEncoder{})

// MaxChunkSize allowed for decoding messages.
const MaxChunkSize = uint64(1 << 20) // 1Mb

// SszNetworkEncoder supports p2p networking encoding using SimpleSerialize
// with snappy compression (if enabled).
type SszNetworkEncoder struct {
	UseSnappyCompression bool
}

func (e SszNetworkEncoder) doEncode(msg interface{}) ([]byte, error) {
	return ssz.Marshal(msg)
}

// Encode the proto message to the io.Writer.
func (e SszNetworkEncoder) Encode(w io.Writer, msg interface{}) (int, error) {
	if msg == nil {
		return 0, nil
	}
	b, err := e.doEncode(msg)
	if err != nil {
		return 0, err
	}
	if e.UseSnappyCompression {
		return writeSnappyBuffer(w, b)
	}
	return w.Write(b)
}

// EncodeGossip the proto gossip message to the io.Writer.
func (e SszNetworkEncoder) EncodeGossip(w io.Writer, msg interface{}) (int, error) {
	if msg == nil {
		return 0, nil
	}
	b, err := e.doEncode(msg)
	if err != nil {
		return 0, err
	}
	if e.UseSnappyCompression {
		b = snappy.Encode(nil /*dst*/, b)
	}
	return w.Write(b)
}

// EncodeWithLength the proto message to the io.Writer. This encoding prefixes the byte slice with a protobuf varint
// to indicate the size of the message.
func (e SszNetworkEncoder) EncodeWithLength(w io.Writer, msg interface{}) (int, error) {
	if msg == nil {
		return 0, nil
	}
	b, err := e.doEncode(msg)
	if err != nil {
		return 0, err
	}
	// write varint first
	_, err = w.Write(proto.EncodeVarint(uint64(len(b))))
	if err != nil {
		return 0, err
	}
	if e.UseSnappyCompression {
		return writeSnappyBuffer(w, b)
	}
	return w.Write(b)
}

// EncodeWithMaxLength the proto message to the io.Writer. This encoding prefixes the byte slice with a protobuf varint
// to indicate the size of the message. This checks that the encoded message isn't larger than the provided max limit.
func (e SszNetworkEncoder) EncodeWithMaxLength(w io.Writer, msg interface{}, maxSize uint64) (int, error) {
	if msg == nil {
		return 0, nil
	}
	b, err := e.doEncode(msg)
	if err != nil {
		return 0, err
	}
	if uint64(len(b)) > maxSize {
		return 0, fmt.Errorf("size of encoded message is %d which is larger than the provided max limit of %d", len(b), maxSize)
	}
	// write varint first
	_, err = w.Write(proto.EncodeVarint(uint64(len(b))))
	if err != nil {
		return 0, err
	}
	if e.UseSnappyCompression {
		return writeSnappyBuffer(w, b)
	}
	return w.Write(b)
}

func (e SszNetworkEncoder) doDecode(b []byte, to interface{}) error {
	return ssz.Unmarshal(b, to)
}

// Decode the bytes to the protobuf message provided.
func (e SszNetworkEncoder) Decode(b []byte, to interface{}) error {
	if e.UseSnappyCompression {
		newBuffer := bytes.NewBuffer(b)
		r := snappy.NewReader(newBuffer)
		newObj := make([]byte, len(b))
		numOfBytes, err := r.Read(newObj)
		if err != nil {
			return err
		}
		return e.doDecode(newObj[:numOfBytes], to)
	}
	return e.doDecode(b, to)
}

// DecodeGossip decodes the bytes to the protobuf gossip message provided.
func (e SszNetworkEncoder) DecodeGossip(b []byte, to interface{}) error {
	if e.UseSnappyCompression {
		var err error
		b, err = snappy.Decode(nil /*dst*/, b)
		if err != nil {
			return err
		}
	}
	return e.doDecode(b, to)
}

// DecodeWithLength the bytes from io.Reader to the protobuf message provided.
func (e SszNetworkEncoder) DecodeWithLength(r io.Reader, to interface{}) error {
	return e.DecodeWithMaxLength(r, to, MaxChunkSize)
}

// DecodeWithMaxLength the bytes from io.Reader to the protobuf message provided.
// This checks that the decoded message isn't larger than the provided max limit.
func (e SszNetworkEncoder) DecodeWithMaxLength(r io.Reader, to interface{}, maxSize uint64) error {
	if maxSize > MaxChunkSize {
		return fmt.Errorf("maxSize %d exceeds max chunk size %d", maxSize, MaxChunkSize)
	}
	msgLen, err := readVarint(r)
	if err != nil {
		return err
	}
	if e.UseSnappyCompression {
		r = snappy.NewReader(r)
	}
	if msgLen > maxSize {
		return fmt.Errorf("size of decoded message is %d which is larger than the provided max limit of %d", msgLen, maxSize)
	}
	b := make([]byte, e.MaxLength(int(msgLen)))
	numOfBytes, err := r.Read(b)
	if err != nil {
		return err
	}
	return e.doDecode(b[:numOfBytes], to)
}

// ProtocolSuffix returns the appropriate suffix for protocol IDs.
func (e SszNetworkEncoder) ProtocolSuffix() string {
	if e.UseSnappyCompression {
		return "/ssz_snappy"
	}
	return "/ssz"
}

// MaxLength specifies the maximum possible length of an encoded
// chunk of data.
func (e SszNetworkEncoder) MaxLength(length int) int {
	if e.UseSnappyCompression {
		return snappy.MaxEncodedLen(length)
	}
	return length
}

// Writes a bytes value through a snappy buffered writer.
func writeSnappyBuffer(w io.Writer, b []byte) (int, error) {
	bufWriter := snappy.NewBufferedWriter(w)
	defer func() {
		if err := bufWriter.Close(); err != nil {
			logrus.WithError(err).Error("Failed to close snappy buffered writer")
		}
	}()
	return bufWriter.Write(b)
}
