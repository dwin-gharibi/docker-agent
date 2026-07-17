package image

import (
	"bytes"
	"os"
	"time"

	uv "github.com/charmbracelet/ultraviolet"
	"github.com/mattn/go-isatty"
	"golang.org/x/term"
)

const (
	kittyProbeID      = "4242"
	kittyProbeTimeout = 300 * time.Millisecond
	kittyProbeQuery   = "\x1b_Gi=" + kittyProbeID + ",a=q,t=d,f=24,s=1,v=1;AAAA\x1b\\"
	kittyProbeOK      = "\x1b_Gi=" + kittyProbeID + ";OK\x1b\\"
)

// SupportsKittyGraphics probes a terminal for Kitty graphics support. It must
// run before the TUI takes ownership of the terminal input stream.
func SupportsKittyGraphics(in, out *os.File) bool {
	if in == nil || out == nil || !isatty.IsTerminal(in.Fd()) || !isatty.IsTerminal(out.Fd()) {
		return false
	}

	state, err := term.MakeRaw(int(in.Fd()))
	if err != nil {
		return false
	}
	defer func() { _ = term.Restore(int(in.Fd()), state) }()

	reader, err := uv.NewCancelReader(in)
	if err != nil {
		return false
	}
	defer reader.Close()

	timer := time.AfterFunc(kittyProbeTimeout, func() { reader.Cancel() })
	defer timer.Stop()
	if _, err := out.WriteString(kittyProbeQuery); err != nil {
		return false
	}

	response := make([]byte, 0, 128)
	buf := make([]byte, 128)
	for {
		n, err := reader.Read(buf)
		if n > 0 {
			response = append(response, buf[:n]...)
			if bytes.Contains(response, []byte(kittyProbeOK)) {
				return true
			}
			if len(response) > 1024 {
				response = response[len(response)-512:]
			}
		}
		if err != nil {
			return false
		}
	}
}
