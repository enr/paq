package version

import (
	"regexp"
	"strconv"
	"strings"
)

var semverRe = regexp.MustCompile(`(\d+\.\d+(?:\.\d+)?)`)

// Clean rimuove il prefisso "v" e qualsiasi suffisso non numerico da una stringa di versione.
// Esempi: "v14.1.1" → "14.1.1", "jdk-21.0.2+13" → "21.0.2", "14.1.1" → "14.1.1"
func Clean(raw string) string {
	// rimuovi prefisso "v" case-insensitive
	s := strings.TrimPrefix(raw, "v")
	s = strings.TrimPrefix(s, "V")

	// estrai la prima sequenza di tipo N.N o N.N.N
	match := semverRe.FindString(s)
	if match != "" {
		return match
	}

	// fallback: ritorna la stringa senza prefisso "v"
	return s
}

// Build estrae il numero di build da un tag/versione, cioè la parte dopo il
// primo "+". Esempi: "jdk-21.0.11+10" → "10", "21.0.2+13" → "13", "14.1.1" → "".
func Build(raw string) string {
	if idx := strings.IndexByte(raw, '+'); idx >= 0 {
		return raw[idx+1:]
	}
	return ""
}

// Compare confronta due versioni già pulite (es. "14.1.1") numericamente
// campo per campo (major, minor, patch). Ritorna <0 se a < b, 0 se uguali,
// >0 se a > b. Campi mancanti o non numerici valgono 0.
func Compare(a, b string) int {
	aMajor, aMinor, aPatch := Parse(a)
	bMajor, bMinor, bPatch := Parse(b)
	if d := compareNumeric(aMajor, bMajor); d != 0 {
		return d
	}
	if d := compareNumeric(aMinor, bMinor); d != 0 {
		return d
	}
	return compareNumeric(aPatch, bPatch)
}

func compareNumeric(a, b string) int {
	ai, _ := strconv.Atoi(a)
	bi, _ := strconv.Atoi(b)
	return ai - bi
}

// Parse estrae major, minor, patch da una versione già pulita (es. "14.1.1").
// Se un campo non è presente, ritorna stringa vuota per quel campo.
func Parse(version string) (major, minor, patch string) {
	parts := strings.SplitN(version, ".", 3)
	if len(parts) >= 1 {
		major = parts[0]
	}
	if len(parts) >= 2 {
		minor = parts[1]
	}
	if len(parts) >= 3 {
		// rimuovi eventuale suffisso dopo la patch (es. "2+13" → "2")
		patch = semverRe.FindString(parts[2])
		if patch == "" {
			patch = parts[2]
		}
		// patch è solo il numero, non contenere punti
		if idx := strings.IndexByte(patch, '.'); idx >= 0 {
			patch = patch[:idx]
		}
	}
	return
}
