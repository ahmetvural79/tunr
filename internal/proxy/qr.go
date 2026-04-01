package proxy

import (
	"bytes"

	qrterminal "github.com/mdp/qrterminal/v3"
)

// GenerateQRCode renders a QR code into a string using half-block characters.
// Pass an empty url to get an empty string back.
func GenerateQRCode(url string) string {
	if url == "" {
		return ""
	}

	var buf bytes.Buffer
	config := qrterminal.Config{
		Level:      qrterminal.L,
		Writer:     &buf,
		HalfBlocks: true,
		BlackChar:  qrterminal.BLACK,
		WhiteChar:  qrterminal.WHITE,
	}
	qrterminal.GenerateWithConfig(url, config)
	return buf.String()
}
