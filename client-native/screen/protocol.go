package screen

import "encoding/binary"

// Typy zpráv pro screen sharing DataChannel protokol.
const (
	MsgMetadata byte = 0x01 // [1B type][2B width LE][2B height LE][1B fps]
	MsgH264Data byte = 0x02 // [1B type][N bytes H.264 Annex-B]
)

// IsLegacyJPEG detekuje legacy JPEG frame (bez type prefixu) přes SOI marker.
func IsLegacyJPEG(data []byte) bool {
	return len(data) >= 2 && data[0] == 0xFF && data[1] == 0xD8
}

// EncodeMetadata vytvoří metadata zprávu s rozlišením a FPS.
func EncodeMetadata(w, h, fps int) []byte {
	buf := make([]byte, 6)
	buf[0] = MsgMetadata
	binary.LittleEndian.PutUint16(buf[1:3], uint16(w))
	binary.LittleEndian.PutUint16(buf[3:5], uint16(h))
	buf[5] = byte(fps)
	return buf
}

// DecodeMetadata parsuje metadata zprávu.
func DecodeMetadata(data []byte) (w, h, fps int, ok bool) {
	if len(data) < 5 {
		return 0, 0, 0, false
	}
	w = int(binary.LittleEndian.Uint16(data[0:2]))
	h = int(binary.LittleEndian.Uint16(data[2:4]))
	fps = int(data[4])
	return w, h, fps, true
}

// EncodeH264Chunk obalí H.264 data type prefixem.
func EncodeH264Chunk(h264Data []byte) []byte {
	msg := make([]byte, 1+len(h264Data))
	msg[0] = MsgH264Data
	copy(msg[1:], h264Data)
	return msg
}

// ParseMessage parsuje příchozí DataChannel zprávu.
// Vrací typ zprávy a payload (bez type bytu).
// Pro legacy JPEG vrací typ 0 a celá data jako payload.
func ParseMessage(data []byte) (msgType byte, payload []byte) {
	if len(data) == 0 {
		return 0, nil
	}
	if IsLegacyJPEG(data) {
		return 0, data
	}
	return data[0], data[1:]
}
