package util

// Truncate truncates a string to the first n characters.
// If the string is shorter than n, it returns the original string.
func Truncate(s string, n int) string {
	runes := []rune(s)
	if len(runes) <= n {
		return s
	}
	return string(runes[:n])
}