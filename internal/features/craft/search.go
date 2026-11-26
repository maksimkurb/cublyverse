package craft

import (
	"regexp"
	"slices"
	"strings"
	"unicode/utf8"
)

// maxSearchWords bounds the work one query can ask for.
const maxSearchWords = 10

// accented and plain mirror craft_search_text of the database.
const (
	accented = "áàâäãåąāčćçďéèêëěęēíìîïīĺľłňńñóòôöõőōřŕšśşťţúùûüůűūýÿžźżё"
	plain    = "aaaaaaaacccdeeeeeeeiiiiilllnnnooooooorrsssttuuuuuuuyyzzzе"
)

var foldAccents = func() *strings.Replacer {
	from, to := []rune(accented), []rune(plain)
	pairs := make([]string, 0, 2*len(from))
	for i := range from {
		pairs = append(pairs, string(from[i]), string(to[i]))
	}
	return strings.NewReplacer(pairs...)
}()

// searchFold lowers a text and drops the diacritics search ignores.
func searchFold(value string) string {
	return foldAccents.Replace(strings.ToLower(value))
}

// synonym widens a word of the query. A word that starts with one of the
// triggers, or is a beginning of one at least four letters long, also finds the
// patterns. Patterns are regular expressions over the folded text.
type synonym struct {
	triggers []string
	patterns []string
}

// word matches the beginning of an English word: "red" must not find
// "colored".
func word(prefix string) string { return `\m` + prefix }

// synonyms covers the names catalogues write in Latin letters and the colour
// words of their English descriptions.
var synonyms = []synonym{
	{[]string{"мулине"}, []string{"mouline", "мулине"}},
	{[]string{"mouline"}, []string{"мулине"}},
	{[]string{"спешл", "спешиал", "спешел"}, []string{"special"}},
	{[]string{"дмс", "дмц"}, []string{"dmc"}},
	{[]string{"пресиоз", "прециоз", "преиоз"}, []string{"preciosa"}},
	{[]string{"гамма"}, []string{"gamma"}},
	{[]string{"тохо"}, []string{"toho"}},
	{[]string{"миюки", "мийюки"}, []string{"miyuki"}},
	{[]string{"бисер"}, []string{"rocailles", "seed bead"}},
	{[]string{"стеклярус"}, []string{"bugle"}},
	{[]string{"жемчуг", "жемчуж"}, []string{"жемч", word("pearl")}},
	{[]string{"пайетк", "пайеток"}, []string{"пайет", "sequin"}},

	{[]string{"красн"}, []string{"красн", word("red")}},
	{[]string{"оранж"}, []string{"оранж", word("orange")}},
	{[]string{"желт"}, []string{"желт", word("yellow")}},
	{[]string{"зелен"}, []string{"зелен", word("green")}},
	{[]string{"голуб"}, []string{"голуб", word("blue")}},
	{[]string{"синий", "синяя", "синее", "синие"}, []string{"син", word("blue")}},
	{[]string{"фиолет"}, []string{"фиолет", word("violet"), word("purple")}},
	{[]string{"сирен"}, []string{"сирен", word("lilac")}},
	{[]string{"розов"}, []string{"розов", word("pink"), word("rose")}},
	{[]string{"белый", "белая", "белое", "белые"}, []string{"бел", word("white")}},
	{[]string{"черн"}, []string{"черн", word("black")}},
	{[]string{"серый", "серая", "серое", "серые"}, []string{"сер(ый|ая|ое|ые|о-)", word("gr[ae]y")}},
	{[]string{"коричн"}, []string{"коричн", word("brown")}},
	{[]string{"бирюз"}, []string{"бирюз", word("turquoise")}},
	{[]string{"бежев"}, []string{"беж", word("beige"), word("ecru")}},
	{[]string{"золот"}, []string{"золот", word("gold")}},
	{[]string{"серебр"}, []string{"серебр", word("silver")}},
	{[]string{"прозрач"}, []string{"прозрач", word("transp")}},
	{[]string{"непрозрач"}, []string{"непрозрач", word("opaque")}},
	{[]string{"матов"}, []string{"матов", word("matt")}},
	{[]string{"радужн"}, []string{"радуж", word("rainbow")}},
}

// searchTerms turns a query into one regular expression per word. An entry is
// found when every expression matches it, so the order of words and the field
// each is in do not matter.
func searchTerms(query string) []string {
	words := strings.Fields(searchFold(query))
	if len(words) > maxSearchWords {
		words = words[:maxSearchWords]
	}
	terms := make([]string, 0, len(words))
	for _, typed := range words {
		alternatives := []string{regexp.QuoteMeta(typed)}
		for _, entry := range synonyms {
			if !entry.matches(typed) {
				continue
			}
			for _, pattern := range entry.patterns {
				if !slices.Contains(alternatives, pattern) {
					alternatives = append(alternatives, pattern)
				}
			}
		}
		if len(alternatives) == 1 {
			terms = append(terms, alternatives[0])
			continue
		}
		terms = append(terms, "("+strings.Join(alternatives, "|")+")")
	}
	return terms
}

func (s synonym) matches(typed string) bool {
	return slices.ContainsFunc(s.triggers, func(trigger string) bool {
		return strings.HasPrefix(typed, trigger) ||
			(utf8.RuneCountInString(typed) >= 4 && strings.HasPrefix(trigger, typed))
	})
}
