package gameserver

import (
	"encoding/binary"
	"fmt"
	"io"
	"net"
	"os/exec"
	"strconv"
	"strings"
	"time"
)

// Source RCON packet types
const (
	rconAuth         int32 = 3 // SERVERDATA_AUTH
	rconExecCommand  int32 = 2 // SERVERDATA_EXECCOMMAND
	rconAuthResponse int32 = 2 // SERVERDATA_AUTH_RESPONSE
)

// rconPacket represents a Source RCON packet
type rconPacket struct {
	ID   int32
	Type int32
	Body string
}

// marshal serializes a packet into binary format:
// 4B size (LE) + 4B id (LE) + 4B type (LE) + body + 2B null terminator
func (p *rconPacket) marshal() []byte {
	bodyBytes := []byte(p.Body)
	// size = id(4) + type(4) + body + null(1) + null(1)
	size := int32(4 + 4 + len(bodyBytes) + 2)

	buf := make([]byte, 4+size)
	binary.LittleEndian.PutUint32(buf[0:4], uint32(size))
	binary.LittleEndian.PutUint32(buf[4:8], uint32(p.ID))
	binary.LittleEndian.PutUint32(buf[8:12], uint32(p.Type))
	copy(buf[12:], bodyBytes)
	buf[12+len(bodyBytes)] = 0
	buf[13+len(bodyBytes)] = 0

	return buf
}

// readRCONPacket reads one packet from a reader
func readRCONPacket(r io.Reader) (*rconPacket, error) {
	var size int32
	if err := binary.Read(r, binary.LittleEndian, &size); err != nil {
		return nil, fmt.Errorf("read size: %w", err)
	}

	if size < 10 || size > 4096+10 {
		return nil, fmt.Errorf("invalid packet size: %d", size)
	}

	payload := make([]byte, size)
	if _, err := io.ReadFull(r, payload); err != nil {
		return nil, fmt.Errorf("read payload: %w", err)
	}

	p := &rconPacket{
		ID:   int32(binary.LittleEndian.Uint32(payload[0:4])),
		Type: int32(binary.LittleEndian.Uint32(payload[4:8])),
	}

	// Body is from offset 8 to end minus 2 null bytes
	bodyEnd := len(payload) - 2
	if bodyEnd < 8 {
		bodyEnd = 8
	}
	p.Body = string(payload[8:bodyEnd])

	return p, nil
}

// RCONExec connects to an RCON server, authenticates and executes a command.
// Returns response text or an error.
func RCONExec(host string, port int, password, command string) (string, error) {
	addr := net.JoinHostPort(host, strconv.Itoa(port))

	// Connect with timeout
	conn, err := net.DialTimeout("tcp", addr, 5*time.Second)
	if err != nil {
		return "", fmt.Errorf("connect to %s: %w", addr, err)
	}
	defer conn.Close()

	// Auth — send type 3 with password
	authPkt := &rconPacket{
		ID:   1,
		Type: rconAuth,
		Body: password,
	}
	if err := sendPacket(conn, authPkt); err != nil {
		return "", fmt.Errorf("send auth: %w", err)
	}

	// Wait for auth response (server may send empty response + auth response)
	conn.SetReadDeadline(time.Now().Add(10 * time.Second))
	for {
		resp, err := readRCONPacket(conn)
		if err != nil {
			return "", fmt.Errorf("read auth response: %w", err)
		}

		// Auth response has type SERVERDATA_AUTH_RESPONSE (2)
		if resp.Type == rconAuthResponse {
			if resp.ID == -1 {
				return "", fmt.Errorf("RCON authentication failed")
			}
			// Successful authentication
			break
		}
		// Some servers send an empty SERVERDATA_RESPONSE_VALUE before auth response
	}

	// Execute command — type 2
	cmdPkt := &rconPacket{
		ID:   2,
		Type: rconExecCommand,
		Body: command,
	}
	if err := sendPacket(conn, cmdPkt); err != nil {
		return "", fmt.Errorf("send command: %w", err)
	}

	// Read response
	conn.SetReadDeadline(time.Now().Add(10 * time.Second))
	resp, err := readRCONPacket(conn)
	if err != nil {
		return "", fmt.Errorf("read command response: %w", err)
	}

	return resp.Body, nil
}

// sendPacket sends an RCON packet over a TCP connection
func sendPacket(conn net.Conn, pkt *rconPacket) error {
	data := pkt.marshal()
	conn.SetWriteDeadline(time.Now().Add(5 * time.Second))
	_, err := conn.Write(data)
	return err
}

// GetContainerIP gets the IP address of a Docker container via docker inspect
func GetContainerIP(containerID string) (string, error) {
	if !isValidContainerID(containerID) {
		return "", fmt.Errorf("invalid container ID")
	}

	cmd := exec.Command("docker", "inspect", "-f", "{{range .NetworkSettings.Networks}}{{.IPAddress}}{{end}}", containerID)
	out, err := cmd.Output()
	if err != nil {
		return "", fmt.Errorf("docker inspect: %w", err)
	}

	ip := strings.TrimSpace(string(out))
	if ip == "" {
		return "", fmt.Errorf("container has no IP address")
	}

	return ip, nil
}
