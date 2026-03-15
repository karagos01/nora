package screen

import "encoding/binary"

// Message types for the screen sharing DataChannel protocol.
const (
	MsgMetadata byte = 0x01 // [1B type][2B width LE][2B height LE][1B fps]
	MsgH264Data byte = 0x02 // [1B type][N bytes H.264 Annex-B]
)

// IsLegacyJPEG detects a legacy JPEG frame (without type prefix) via SOI marker.
func IsLegacyJPEG(data []byte) bool {
	return len(data) >= 2 && data[0] == 0xFF && data[1] == 0xD8
}

// EncodeMetadata creates a metadata message with resolution and FPS.
func EncodeMetadata(w, h, fps int) []byte {
	buf := make([]byte, 6)
	buf[0] = MsgMetadata
	binary.LittleEndian.PutUint16(buf[1:3], uint16(w))
	binary.LittleEndian.PutUint16(buf[3:5], uint16(h))
	buf[5] = byte(fps)
	return buf
}

// DecodeMetadata parses a metadata message.
func DecodeMetadata(data []byte) (w, h, fps int, ok bool) {
	if len(data) < 5 {
		return 0, 0, 0, false
	}
	w = int(binary.LittleEndian.Uint16(data[0:2]))
	h = int(binary.LittleEndian.Uint16(data[2:4]))
	fps = int(data[4])
	return w, h, fps, true
}

// EncodeH264Chunk wraps H.264 data with a type prefix.
func EncodeH264Chunk(h264Data []byte) []byte {
	msg := make([]byte, 1+len(h264Data))
	msg[0] = MsgH264Data
	copy(msg[1:], h264Data)
	return msg
}

// ParseMessage parses an incoming DataChannel message.
// Returns the message type and payload (without the type byte).
// For legacy JPEG, returns type 0 and the entire data as payload.
func ParseMessage(data []byte) (msgType byte, payload []byte) {
	if len(data) == 0 {
		return 0, nil
	}
	if IsLegacyJPEG(data) {
		return 0, data
	}
	return data[0], data[1:]
}
