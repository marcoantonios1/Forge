package ui

import "os"

const (
	Reset  = "\033[0m"
	Bold   = "\033[1m"
	Dim    = "\033[2m"
	Red    = "\033[31m"
	Green  = "\033[32m"
	Yellow = "\033[33m"
	Blue   = "\033[34m"
	Cyan   = "\033[36m"
	White  = "\033[37m"
)

// IsTTY returns true if f is an interactive terminal.
// Uses os.ModeCharDevice — never panics.
func IsTTY(f *os.File) bool {
	fi, err := f.Stat()
	if err != nil {
		return false
	}
	return fi.Mode()&os.ModeCharDevice != 0
}

// Colour wraps s in the given ANSI code + Reset, only when colour is true.
func Colour(s, code string, colour bool) string {
	if !colour {
		return s
	}
	return code + s + Reset
}

// DimText wraps s in the Dim ANSI code, only when colour is true.
func DimText(s string, colour bool) string {
	return Colour(s, Dim, colour)
}
