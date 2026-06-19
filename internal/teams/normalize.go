package teams

import "strings"

// diacritics folds accented letters to ASCII (e.g. Türkiye → turkiye, Curaçao → curacao).
var diacritics = strings.NewReplacer(
	"ç", "c", "ć", "c", "č", "c",
	"ü", "u", "ú", "u", "ù", "u", "û", "u",
	"é", "e", "è", "e", "ê", "e", "ë", "e",
	"á", "a", "à", "a", "â", "a", "ä", "a", "ã", "a", "å", "a",
	"í", "i", "ì", "i", "î", "i", "ï", "i",
	"ó", "o", "ò", "o", "ô", "o", "ö", "o", "õ", "o", "ø", "o",
	"ñ", "n", "ß", "ss", "ş", "s", "š", "s", "ž", "z", "ý", "y",
)

// aliases maps a normalized name to a canonical one, reconciling how different
// providers spell the same national team.
var aliases = map[string]string{
	"united states":                "usa",
	"united states of america":     "usa",
	"turkey":                       "turkiye",
	"czech republic":               "czechia",
	"korea republic":               "south korea",
	"republic of korea":            "south korea",
	"korea dpr":                    "north korea",
	"cote d ivoire":                "ivory coast",
	"cote divoire":                 "ivory coast",
	"dr congo":                     "congo dr",
	"democratic republic of congo": "congo dr",
	"bosnia and herzegovina":       "bosnia",
	"bosnia herzegovina":           "bosnia",
	"ir iran":                      "iran",
	"cape verde islands":           "cape verde",
}

// Normalize canonicalizes a team name for cross-provider matching: lowercase,
// accent-free, punctuation-stripped, with known national-team aliases applied.
func Normalize(name string) string {
	s := strings.ToLower(strings.TrimSpace(name))
	s = diacritics.Replace(s)
	s = strings.ReplaceAll(s, "&", " and ")
	s = strings.Map(func(r rune) rune {
		switch r {
		case '.', '\'', '-', '/', ',':
			return ' '
		}
		return r
	}, s)
	s = strings.Join(strings.Fields(s), " ")
	if a, ok := aliases[s]; ok {
		return a
	}
	return s
}

// NameMatches reports whether two team names refer to the same team, after
// normalization (exact, or one fully contained in the other).
func NameMatches(a, b string) bool {
	na, nb := Normalize(a), Normalize(b)
	if na == "" || nb == "" {
		return false
	}
	return na == nb || strings.Contains(na, nb) || strings.Contains(nb, na)
}
