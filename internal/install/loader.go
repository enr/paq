package install

import (
	"debug/elf"
	"io"
	"os"
	"strings"
)

// missingLoader returns the dynamic loader (ELF PT_INTERP, e.g.
// /lib64/ld-linux-x86-64.so.2) that the executable at path requires when it
// is absent from this system: such a binary cannot run here, typically a
// glibc build on a musl system. It returns "" for a non-ELF file, a static
// binary or a loader that is present (e.g. provided by gcompat).
func missingLoader(path string) string {
	f, err := elf.Open(path)
	if err != nil {
		return ""
	}
	defer f.Close()
	for _, p := range f.Progs {
		if p.Type != elf.PT_INTERP {
			continue
		}
		b, err := io.ReadAll(p.Open())
		if err != nil {
			return ""
		}
		interp := strings.TrimRight(string(b), "\x00")
		if _, err := os.Stat(interp); err != nil {
			return interp
		}
	}
	return ""
}
