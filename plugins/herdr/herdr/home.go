package herdr

import (
	"os"
	"os/user"
)

// Home is the user's home directory. A Plugin's environment carries no
// HOME (the Supervisor passes only PATH and its own variables), so it
// falls back to the account database.
func Home() string {
	if h, err := os.UserHomeDir(); err == nil && h != "" {
		return h
	}
	if u, err := user.Current(); err == nil {
		return u.HomeDir
	}
	return ""
}
