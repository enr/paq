package install

import (
	"bytes"
	"context"
	"debug/elf"
	"encoding/binary"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/enr/paq/internal/config"
)

// writeELF writes a minimal x86-64 ELF executable to dir/name, with a
// PT_INTERP program header naming interp (none when interp is empty, i.e. a
// static binary).
func writeELF(t *testing.T, dir, name, interp string) string {
	t.Helper()
	const ehsize, phentsize = 64, 56
	hdr := elf.Header64{
		Type:      uint16(elf.ET_EXEC),
		Machine:   uint16(elf.EM_X86_64),
		Version:   uint32(elf.EV_CURRENT),
		Ehsize:    ehsize,
		Phentsize: phentsize,
	}
	copy(hdr.Ident[:], elf.ELFMAG)
	hdr.Ident[elf.EI_CLASS] = byte(elf.ELFCLASS64)
	hdr.Ident[elf.EI_DATA] = byte(elf.ELFDATA2LSB)
	hdr.Ident[elf.EI_VERSION] = byte(elf.EV_CURRENT)

	var buf bytes.Buffer
	if interp != "" {
		hdr.Phoff = ehsize
		hdr.Phnum = 1
		data := append([]byte(interp), 0)
		binary.Write(&buf, binary.LittleEndian, hdr)
		binary.Write(&buf, binary.LittleEndian, elf.Prog64{
			Type:   uint32(elf.PT_INTERP),
			Flags:  uint32(elf.PF_R),
			Off:    ehsize + phentsize,
			Filesz: uint64(len(data)),
			Memsz:  uint64(len(data)),
			Align:  1,
		})
		buf.Write(data)
	} else {
		binary.Write(&buf, binary.LittleEndian, hdr)
	}

	path := filepath.Join(dir, name)
	if err := os.WriteFile(path, buf.Bytes(), 0o755); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestMissingLoader(t *testing.T) {
	dir := t.TempDir()
	present := filepath.Join(dir, "ld-present.so.1")
	if err := os.WriteFile(present, nil, 0o755); err != nil {
		t.Fatal(err)
	}
	absent := filepath.Join(dir, "ld-absent.so.2")
	script := filepath.Join(dir, "script")
	if err := os.WriteFile(script, []byte("#!/bin/sh\n"), 0o755); err != nil {
		t.Fatal(err)
	}

	cases := []struct {
		name, path, want string
	}{
		{"loader absent", writeELF(t, dir, "dynamic-absent", absent), absent},
		{"loader present", writeELF(t, dir, "dynamic-present", present), ""},
		{"static", writeELF(t, dir, "static", ""), ""},
		{"not ELF", script, ""},
		{"missing file", filepath.Join(dir, "nope"), ""},
	}
	for _, tc := range cases {
		if got := missingLoader(tc.path); got != tc.want {
			t.Errorf("%s: missingLoader = %q, want %q", tc.name, got, tc.want)
		}
	}
}

// TestPipelineWarnsOnMissingLoader verifies that installing a binary whose
// dynamic loader is absent from the system succeeds with a warning.
func TestPipelineWarnsOnMissingLoader(t *testing.T) {
	isolateState(t)
	dir := t.TempDir()
	elfData, err := os.ReadFile(writeELF(t, dir, "rg", filepath.Join(dir, "ld-absent.so.2")))
	if err != nil {
		t.Fatal(err)
	}
	tgzData := makeFakeTarGz(elfData)

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write(tgzData)
	}))
	defer srv.Close()

	dest := filepath.Join(t.TempDir(), "rg")
	cfg := &config.Config{
		Specs: map[string]config.Spec{
			"rg": {Backend: "url", Source: srv.URL + "/rg-{{version}}.tar.gz", Archive: "tar.gz", Extract: "rg", Chmod: "0755"},
		},
		Apps: map[string]config.AppEntry{
			"rg": {Use: "rg", Version: "0.1.0", Dest: dest},
		},
	}

	var warnings []string
	hooks := &Hooks{OnWarn: func(msg string) { warnings = append(warnings, msg) }}
	if err := Run(context.Background(), cfg, "rg", nil, hooks); err != nil {
		t.Fatalf("install failed: %v", err)
	}
	for _, w := range warnings {
		if strings.Contains(w, "ld-absent.so.2") && strings.Contains(w, "will not run") {
			return
		}
	}
	t.Errorf("no missing-loader warning, got: %v", warnings)
}
