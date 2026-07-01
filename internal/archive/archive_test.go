package archive

import (
	"archive/tar"
	"archive/zip"
	"bytes"
	"compress/gzip"
	"os"
	"path/filepath"
	"testing"
)

// makeTarGz crea un .tar.gz in-memory con le entry fornite.
// entries è una mappa path→contenuto.
func makeTarGz(t *testing.T, entries map[string]string) string {
	t.Helper()
	var buf bytes.Buffer
	gz := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gz)
	for name, content := range entries {
		tw.WriteHeader(&tar.Header{
			Name: name,
			Mode: 0755,
			Size: int64(len(content)),
		})
		tw.Write([]byte(content))
	}
	tw.Close()
	gz.Close()

	tmp, _ := os.CreateTemp(t.TempDir(), "test-*.tar.gz")
	tmp.Write(buf.Bytes())
	tmp.Close()
	return tmp.Name()
}

func makeZip(t *testing.T, entries map[string]string) string {
	t.Helper()
	tmp, _ := os.CreateTemp(t.TempDir(), "test-*.zip")
	zw := zip.NewWriter(tmp)
	for name, content := range entries {
		f, _ := zw.Create(name)
		f.Write([]byte(content))
	}
	zw.Close()
	tmp.Close()
	return tmp.Name()
}

func TestExtractTarGzSingleFile(t *testing.T) {
	tgz := makeTarGz(t, map[string]string{
		"ripgrep-14.1.1-x86_64-unknown-linux-gnu/rg":     "binary-content",
		"ripgrep-14.1.1-x86_64-unknown-linux-gnu/README": "readme",
	})

	dest := t.TempDir()
	err := Extract(tgz, "tar.gz", ExtractOpts{
		Extract: "rg",
		Dest:    dest,
	})
	if err != nil {
		t.Fatal(err)
	}

	data, err := os.ReadFile(filepath.Join(dest, "rg"))
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != "binary-content" {
		t.Errorf("content = %q, want binary-content", string(data))
	}
}

func TestExtractTarGzStripComponents(t *testing.T) {
	tgz := makeTarGz(t, map[string]string{
		"jdk-21.0.2/bin/java":   "java-binary",
		"jdk-21.0.2/lib/rt.jar": "rt-jar",
	})

	dest := t.TempDir()
	err := Extract(tgz, "tar.gz", ExtractOpts{
		StripComponents: 1,
		Dest:            dest,
	})
	if err != nil {
		t.Fatal(err)
	}

	data, err := os.ReadFile(filepath.Join(dest, "bin", "java"))
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != "java-binary" {
		t.Errorf("java content = %q, want java-binary", string(data))
	}
}

func TestExtractTarGzSubdir(t *testing.T) {
	tgz := makeTarGz(t, map[string]string{
		"jdk-21.jdk/Contents/Home/bin/java": "java-binary",
		"jdk-21.jdk/Contents/Home/lib/foo":  "lib-foo",
		"jdk-21.jdk/other/file":             "other",
	})

	dest := t.TempDir()
	err := Extract(tgz, "tar.gz", ExtractOpts{
		Subdir: "*/Contents/Home",
		Dest:   dest,
	})
	if err != nil {
		t.Fatal(err)
	}

	data, err := os.ReadFile(filepath.Join(dest, "bin", "java"))
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != "java-binary" {
		t.Errorf("java content = %q, want java-binary", string(data))
	}

	// "other" non deve essere presente
	if _, err := os.Stat(filepath.Join(dest, "other")); !os.IsNotExist(err) {
		t.Error("'other' should not have been extracted")
	}
}

func TestExtractZipSingleFile(t *testing.T) {
	z := makeZip(t, map[string]string{
		"dir/rg.exe": "exe-content",
		"dir/README": "readme",
	})

	dest := t.TempDir()
	err := Extract(z, "zip", ExtractOpts{
		Extract: "rg.exe",
		Dest:    dest,
	})
	if err != nil {
		t.Fatal(err)
	}

	data, err := os.ReadFile(filepath.Join(dest, "rg.exe"))
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != "exe-content" {
		t.Errorf("content = %q, want exe-content", string(data))
	}
}

func TestExtractMissingFile(t *testing.T) {
	tgz := makeTarGz(t, map[string]string{
		"dir/other": "content",
	})
	dest := t.TempDir()
	err := Extract(tgz, "tar.gz", ExtractOpts{Extract: "rg", Dest: dest})
	if err == nil {
		t.Error("expected error for missing extract file")
	}
}
