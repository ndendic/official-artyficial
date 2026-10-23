//go:build ignore

// build.go builds the Plugin package into package/: the provider binary and
// manifest.json with the binary's SHA-256 and its target.
//
//	go run build.go
//
// The target is what "go env GOOS GOARCH" reports, so GOOS and GOARCH in the
// environment pick it; it names manifest.platform.targets and the .exe suffix.
// For a cross build, build this program for this machine first, then run it
// with the target set (go run would build it for the target too):
//
//	go build -o /tmp/herdr-build build.go && GOOS=darwin GOARCH=arm64 /tmp/herdr-build
//
// bin/release does this for you.
//
// The RT Station Go SDK is not public yet. go.mod replaces
// github.com/ndendic/data-start with ../../../data-start, a data-start
// checkout beside this repository. Elsewhere set DATA_START_DIR to a
// data-start checkout and the build uses a copy of go.mod that points there
// (go.mod is untouched). The copy names the checkout by a path relative to
// this folder, so the binary's build info carries no path from this machine.
//
// build.go also writes THIRD_PARTY_NOTICES into package/: this repository's
// licence, then the licence files of every module linked into the binary and
// of the Go standard library. The manifest declares it beside the binary.
package main

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

func main() {
	if err := build(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func build() error {
	raw, err := os.ReadFile("manifest.template.json")
	if err != nil {
		return err
	}
	var manifest map[string]any
	if err = json.Unmarshal(raw, &manifest); err != nil {
		return err
	}
	if err = os.MkdirAll("package", 0o755); err != nil {
		return err
	}
	goos, goarch, err := target()
	if err != nil {
		return err
	}
	entry := "provider"
	if goos == "windows" {
		entry += ".exe"
	}
	args := []string{"build", "-trimpath", "-o", filepath.Join("package", entry), "."}
	if dir := os.Getenv("DATA_START_DIR"); dir != "" {
		modfile, cleanup, err := replacedModfile(dir)
		if err != nil {
			return err
		}
		defer cleanup()
		args = append([]string{"build", "-modfile=" + modfile}, args[1:]...)
	}
	command := exec.Command("go", args...)
	command.Stdout, command.Stderr = os.Stdout, os.Stderr
	if err = command.Run(); err != nil {
		return err
	}
	binary, err := os.ReadFile(filepath.Join("package", entry))
	if err != nil {
		return err
	}
	digest := sha256.Sum256(binary)
	notices, err := thirdPartyNotices(filepath.Join("package", entry))
	if err != nil {
		return fmt.Errorf("third-party notices: %w", err)
	}
	if err = os.WriteFile(filepath.Join("package", noticesName), notices, 0o644); err != nil {
		return err
	}
	noticesDigest := sha256.Sum256(notices)
	manifest["entry"] = entry
	manifest["files"] = map[string]string{
		entry:       hex.EncodeToString(digest[:]),
		noticesName: hex.EncodeToString(noticesDigest[:]),
	}
	manifest["platform"].(map[string]any)["targets"] = []string{goos + "/" + goarch}
	raw, err = json.MarshalIndent(manifest, "", "  ")
	if err != nil {
		return err
	}
	if err = os.WriteFile(filepath.Join("package", "manifest.json"), append(raw, '\n'), 0o644); err != nil {
		return err
	}
	fmt.Println("built package/ for", goos+"/"+goarch)
	return nil
}

// target is the GOOS and GOARCH go build uses: the environment's, or the
// toolchain's defaults.
func target() (string, string, error) {
	out, err := exec.Command("go", "env", "GOOS", "GOARCH").Output()
	if err != nil {
		return "", "", fmt.Errorf("go env GOOS GOARCH: %w", err)
	}
	fields := strings.Fields(string(out))
	if len(fields) != 2 {
		return "", "", fmt.Errorf("go env GOOS GOARCH gave %q", out)
	}
	return fields[0], fields[1], nil
}

// replacedModfile writes go.mod and go.sum to a temporary directory with the
// data-start replace pointing at dir, for go build -modfile.
func replacedModfile(dir string) (string, func(), error) {
	dir, err := filepath.Abs(dir)
	if err != nil {
		return "", nil, err
	}
	if _, err = os.Stat(filepath.Join(dir, "go.mod")); err != nil {
		return "", nil, fmt.Errorf("DATA_START_DIR %s: %w", dir, err)
	}
	// Go resolves a relative replace against the module root (this folder),
	// not against the folder of the -modfile copy.
	cwd, err := os.Getwd()
	if err != nil {
		return "", nil, err
	}
	rel, err := filepath.Rel(cwd, dir)
	if err != nil {
		return "", nil, err
	}
	rel = filepath.ToSlash(rel)
	if !strings.HasPrefix(rel, ".") {
		rel = "./" + rel
	}
	mod, err := os.ReadFile("go.mod")
	if err != nil {
		return "", nil, err
	}
	sum, err := os.ReadFile("go.sum")
	if err != nil {
		return "", nil, err
	}
	var lines []string
	for _, line := range strings.Split(string(mod), "\n") {
		if strings.HasPrefix(line, "replace github.com/ndendic/data-start =>") {
			line = "replace github.com/ndendic/data-start => " + rel
		}
		lines = append(lines, line)
	}
	tmp, err := os.MkdirTemp("", "herdr-console-mod-")
	if err != nil {
		return "", nil, err
	}
	cleanup := func() { os.RemoveAll(tmp) }
	modfile := filepath.Join(tmp, "go.mod")
	if err = os.WriteFile(modfile, []byte(strings.Join(lines, "\n")), 0o644); err == nil {
		err = os.WriteFile(filepath.Join(tmp, "go.sum"), sum, 0o644)
	}
	if err != nil {
		cleanup()
		return "", nil, err
	}
	return modfile, cleanup, nil
}

const noticesName = "THIRD_PARTY_NOTICES"

// sdkNotice stands in for the RT Station Go SDK, which has no public licence yet.
const sdkNotice = `github.com/ndendic/data-start (the RT Station Go SDK)
Copyright (c) 2026 Nikola Dendić. It is not public yet and has no open
source licence. It is included in this binary by its copyright holder, only
so that this Plugin runs; no other rights in it are granted.
`

// thirdPartyNotices lists the licence of this repository, of the Go standard
// library and of every module that "go version -m" reports in the binary,
// with each module's LICENSE, COPYING and NOTICE files from the module cache.
func thirdPartyNotices(binary string) ([]byte, error) {
	var b bytes.Buffer
	b.WriteString("Third-party notices for the herdr console Plugin (artyficial.herdr)\n\n")
	section := func(title string, body []byte) {
		b.WriteString(strings.Repeat("=", 72) + "\n" + title + "\n" + strings.Repeat("=", 72) + "\n\n")
		b.Write(bytes.TrimRight(body, "\n"))
		b.WriteString("\n\n")
	}
	own, err := os.ReadFile(filepath.Join("..", "..", "LICENSE"))
	if err != nil {
		return nil, fmt.Errorf("this repository's LICENSE: %w", err)
	}
	section("herdr console Plugin (this repository)", own)
	goroot, err := exec.Command("go", "env", "GOROOT").Output()
	if err != nil {
		return nil, fmt.Errorf("go env GOROOT: %w", err)
	}
	std, err := os.ReadFile(filepath.Join(strings.TrimSpace(string(goroot)), "LICENSE"))
	if err != nil {
		return nil, fmt.Errorf("the Go licence: %w", err)
	}
	section("The Go standard library and runtime", std)
	cacheOut, err := exec.Command("go", "env", "GOMODCACHE").Output()
	if err != nil {
		return nil, fmt.Errorf("go env GOMODCACHE: %w", err)
	}
	cache := strings.TrimSpace(string(cacheOut))
	info, err := exec.Command("go", "version", "-m", binary).Output()
	if err != nil {
		return nil, fmt.Errorf("go version -m: %w", err)
	}
	for _, line := range strings.Split(string(info), "\n") {
		f := strings.Fields(line)
		if len(f) < 3 || f[0] != "dep" {
			continue
		}
		path, version := f[1], f[2]
		if path == "github.com/ndendic/data-start" {
			section(path, []byte(sdkNotice))
			continue
		}
		dir := filepath.Join(cache, escapePath(path)+"@"+version)
		entries, err := os.ReadDir(dir)
		if err != nil {
			return nil, fmt.Errorf("%s@%s is not in the module cache: %w", path, version, err)
		}
		found := false
		for _, e := range entries {
			up := strings.ToUpper(e.Name())
			if e.IsDir() || !(strings.HasPrefix(up, "LICENSE") || strings.HasPrefix(up, "LICENCE") ||
				strings.HasPrefix(up, "COPYING") || strings.HasPrefix(up, "NOTICE")) {
				continue
			}
			text, err := os.ReadFile(filepath.Join(dir, e.Name()))
			if err != nil {
				return nil, err
			}
			section(path+" "+version+" ("+e.Name()+")", text)
			found = true
		}
		if !found {
			return nil, fmt.Errorf("%s@%s has no LICENSE file", path, version)
		}
	}
	return b.Bytes(), nil
}

// escapePath is the module cache's case encoding: an upper-case letter
// becomes "!" and the lower-case letter.
func escapePath(path string) string {
	var b strings.Builder
	for _, r := range path {
		if r >= 'A' && r <= 'Z' {
			b.WriteByte('!')
			r += 'a' - 'A'
		}
		b.WriteRune(r)
	}
	return b.String()
}
